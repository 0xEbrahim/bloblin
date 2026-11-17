.PHONY: run build fmt fmt-check vet test check

run:
	go run ./cmd/bloblin

build:
	go build -o bin/bloblin ./cmd/bloblin

fmt:
	gofmt -w cmd internal

fmt-check:
	@files=$$(gofmt -l cmd internal) || exit 1; \
	if [ -n "$$files" ]; then printf '%s\n' "$$files"; exit 1; fi

vet:
	go vet ./...

test:
	go test ./...

check: fmt-check vet test build
