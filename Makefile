.PHONY: verify verify-web coverage build frontend-dependencies generate-web

verify: verify-web
	version="$$(go env GOVERSION)"; test "$${version%%-*}" = "go1.26.6"
	test -z "$$(gofmt -l $$(git ls-files --cached --others --exclude-standard -- '*.go'))"
	go vet -mod=readonly ./...
	go test -mod=readonly -race -cover ./...
	go build -mod=readonly ./cmd/...
	go run -mod=readonly ./cmd/gotth-stack validate examples/full-stack.json >/dev/null
	go run -mod=readonly ./cmd/gotth-stack plan examples/full-stack.json >/dev/null

verify-web: generate-web
	version="$$(go env GOVERSION)"; test "$${version%%-*}" = "go1.26.6"
	before="$$(sha256sum internal/site/view_templ.go internal/site/static/site-ab3aa9255fd5fa082e8fc2f5c6739fa76ea155924477bd238ea44996b8b5e7ed.css internal/site/static/htmx-2.0.10.min.js)"; $(MAKE) generate-web >/dev/null; after="$$(sha256sum internal/site/view_templ.go internal/site/static/site-ab3aa9255fd5fa082e8fc2f5c6739fa76ea155924477bd238ea44996b8b5e7ed.css internal/site/static/htmx-2.0.10.min.js)"; test "$$before" = "$$after"
	test "$$(sha256sum internal/site/static/site-ab3aa9255fd5fa082e8fc2f5c6739fa76ea155924477bd238ea44996b8b5e7ed.css | cut -d' ' -f1)" = "ab3aa9255fd5fa082e8fc2f5c6739fa76ea155924477bd238ea44996b8b5e7ed"
	test -z "$$(gofmt -l $$(git ls-files --cached --others --exclude-standard -- '*.go'))"
	test -z "$$(go list -deps ./cmd/gotthstack-web | /usr/bin/rg '^github.com/gotthboard/gotth-stack/(pkg/(stack|journal)|internal/adapters)(/|$$)')"
	test -z "$$(/usr/bin/rg -n --glob '*.go' --glob '!*_test.go' --glob '!*_templ.go' 'os/exec|SetCookie|http\.(Client|Get|Post)|gotth-stack/pkg/(stack|journal)|gotth-stack/internal/adapters' internal/site cmd/gotthstack-web)"
	go vet -mod=readonly ./internal/site ./cmd/gotthstack-web
	go test -mod=readonly -race -cover ./internal/site ./cmd/gotthstack-web
	go build -mod=readonly ./cmd/gotthstack-web

frontend-dependencies:
	test "$$(node --version)" = "v26.7.0"
	test "$$(npm --version)" = "12.0.2"
	npm ci --ignore-scripts --allow-remote=all

generate-web: frontend-dependencies
	go tool templ generate
	npm run generate:css
	install -m 0644 node_modules/htmx.org/dist/htmx.min.js internal/site/static/htmx-2.0.10.min.js

coverage:
	go test -mod=readonly -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

build:
	go build -mod=readonly ./cmd/...
