#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
	echo "usage: $0 <gotth-extension-godaddy-dns-artifact>" >&2
	exit 2
fi

artifact=$1
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/gotth-stack-mail-caddy.XXXXXX")
cleanup() { rm -rf "$scratch"; }
trap cleanup EXIT HUP INT TERM

test -f "$artifact"
cd "$root"
go run ./integration/render-mail-identity.go "$artifact" caddy >"$scratch/Caddyfile"
caddy adapt --config "$scratch/Caddyfile" --adapter caddyfile --validate >/dev/null

sudo -n unshare --net --fork sh -eu -c '
config=$1
log=$2
ip link set lo up
caddy run --config "$config" --adapter caddyfile >"$log" 2>&1 &
pid=$!
stop() {
	kill -TERM "$pid" >/dev/null 2>&1 || true
	wait "$pid" >/dev/null 2>&1 || true
}
trap stop EXIT HUP INT TERM

i=0
while :; do
	if ! kill -0 "$pid" >/dev/null 2>&1; then
		cat "$log" >&2
		exit 1
	fi
	tcp_ports=$(ss -H -lnt | awk '\''{value=$4; sub(/^.*:/, "", value); print value}'\'' | sort -u | tr "\n" " ")
	if [ "$tcp_ports" = "80 443 " ] || [ "$tcp_ports" = "443 80 " ]; then
		break
	fi
	i=$((i + 1))
	if [ "$i" -ge 100 ]; then
		cat "$log" >&2
		echo "unexpected Caddy TCP listeners: $tcp_ports" >&2
		exit 1
	fi
	sleep 0.05
done

if ss -H -lnu | awk '\''{value=$4; sub(/^.*:/, "", value); if (value == "443") found=1} END {exit !found}'\''; then
	cat "$log" >&2
	echo "Caddy opened undeclared UDP/443" >&2
	exit 1
fi
echo MAIL_IDENTITY_CADDY_LISTENERS_OK
' sh "$scratch/Caddyfile" "$scratch/caddy.log"
