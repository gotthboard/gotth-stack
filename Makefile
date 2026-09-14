.PHONY: verify verify-web verify-caddy coverage build frontend-dependencies generate-web

verify: verify-web verify-caddy
	version="$$(go env GOVERSION)"; test "$${version%%-*}" = "go1.26.6"
	test -z "$$(gofmt -l $$(git ls-files --cached --others --exclude-standard -- '*.go'))"
	go vet -mod=readonly ./...
	go test -mod=readonly -race -cover ./...
	go build -mod=readonly ./cmd/...
	go run -mod=readonly ./cmd/gotth-stack validate examples/full-stack.json >/dev/null
	go run -mod=readonly ./cmd/gotth-stack plan examples/full-stack.json >/dev/null

verify-caddy:
	version="$$(go env GOVERSION)"; test "$${version%%-*}" = "go1.26.6"
	test "$$(rg -l --glob '*.go' --glob '!*_test.go' 'os/exec|exec\.Command' internal/adapters/caddy)" = "internal/adapters/caddy/process.go"
	test -z "$$(rg -n --glob '*.go' --glob '!*_test.go' 'systemctl|/etc/caddy|/bin/(sh|bash)|http\.(Post|Put)|fmt\.(Errorf|Sprintf)' internal/adapters/caddy)"
	test -z "$$(rg -n --glob '*.go' --glob '!*_test.go' 'gotth-stack/pkg/journal|func .*Apply|\"apply\"' internal/adapters/caddy cmd/gotth-stack)"
	go vet -mod=readonly ./internal/adapters/caddy
	go test -mod=readonly -race -cover ./internal/adapters/caddy

verify-web: generate-web
	version="$$(go env GOVERSION)"; test "$${version%%-*}" = "go1.26.6"
	before="$$(sha256sum internal/site/view_templ.go internal/site/static/site-4c3b7f235729e101ffa964903e3ec0c23e47ff7b3fc7ba41452d030b900eec52.css internal/site/static/htmx-2.0.10.min.js)"; $(MAKE) generate-web >/dev/null; after="$$(sha256sum internal/site/view_templ.go internal/site/static/site-4c3b7f235729e101ffa964903e3ec0c23e47ff7b3fc7ba41452d030b900eec52.css internal/site/static/htmx-2.0.10.min.js)"; test "$$before" = "$$after"
	test "$$(sha256sum internal/site/static/site-4c3b7f235729e101ffa964903e3ec0c23e47ff7b3fc7ba41452d030b900eec52.css | cut -d' ' -f1)" = "4c3b7f235729e101ffa964903e3ec0c23e47ff7b3fc7ba41452d030b900eec52"
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
