.PHONY: verify coverage build

verify:
	version="$$(go env GOVERSION)"; test "$${version%%-*}" = "go1.26.6"
	test -z "$$(gofmt -l $$(git ls-files --cached --others --exclude-standard -- '*.go'))"
	go vet -mod=readonly ./...
	go test -mod=readonly -race -cover ./...
	go build -mod=readonly ./cmd/gotth-stack
	go run -mod=readonly ./cmd/gotth-stack validate examples/full-stack.json >/dev/null
	go run -mod=readonly ./cmd/gotth-stack plan examples/full-stack.json >/dev/null

coverage:
	go test -mod=readonly -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

build:
	go build -mod=readonly ./cmd/gotth-stack
