#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
artifacts=${WEB_EVIDENCE_DIR:-/tmp/gotthstack-web-evidence}
address=${VERIFY_WEB_ADDR:-127.0.0.1:18089}
url="http://$address"
tmp=$(mktemp -d)
pid=

cleanup() {
	if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
		kill -TERM "$pid"
		wait "$pid"
	fi
	rm -rf "$tmp"
}
trap cleanup EXIT INT TERM

mkdir -p "$artifacts"
if curl --fail --silent --max-time 1 "$url/healthz" >/dev/null 2>&1; then
	echo "verification address already in use: $address" >&2
	exit 1
fi

(
	cd "$repo"
	go build -mod=readonly -o "$tmp/gotthstack-web" ./cmd/gotthstack-web
)
GOTTHSTACK_WEB_ADDR="$address" "$tmp/gotthstack-web" >"$artifacts/server.stdout" 2>"$artifacts/server.stderr" &
pid=$!

ready=false
attempt=0
while [ "$attempt" -lt 50 ]; do
	if curl --fail --silent --max-time 1 "$url/healthz" >"$artifacts/health.txt"; then
		ready=true
		break
	fi
	attempt=$((attempt + 1))
	sleep 0.1
done
if [ "$ready" != true ]; then
	echo "server did not become ready" >&2
	exit 1
fi

curl --fail --silent --show-error "$url/" >"$artifacts/home.html"
curl --fail --silent --show-error -H 'HX-Request: true' "$url/principles?topic=recovery" >"$artifacts/principles-recovery.html"
chromium --headless --disable-gpu --no-sandbox --hide-scrollbars --run-all-compositor-stages-before-draw --virtual-time-budget=1200 --window-size=1440,1100 --screenshot="$artifacts/wide.png" "$url/" >/dev/null 2>"$artifacts/chromium-wide.stderr"
chromium --headless --disable-gpu --no-sandbox --hide-scrollbars --run-all-compositor-stages-before-draw --virtual-time-budget=1200 --window-size=390,844 --screenshot="$artifacts/narrow.png" "$url/" >/dev/null 2>"$artifacts/chromium-narrow.stderr"
chromium --headless --disable-gpu --no-sandbox --disable-javascript --dump-dom "$url/principles?topic=trust" >"$artifacts/no-js.html" 2>"$artifacts/chromium-no-js.stderr"

grep -q '<main id="main-content">' "$artifacts/home.html"
grep -q 'Skip to content' "$artifacts/home.html"
grep -q 'There is deliberately no apply command yet' "$artifacts/home.html"
grep -q '^<article data-topic="recovery"' "$artifacts/principles-recovery.html"
grep -q 'Unknown is a state, not a success color.' "$artifacts/principles-recovery.html"
grep -q 'Secrets do not become configuration confetti.' "$artifacts/no-js.html"

kill -TERM "$pid"
wait "$pid"
pid=

printf 'browser verification passed; artifacts: %s\n' "$artifacts"
