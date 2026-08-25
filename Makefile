BINARY  := refigure
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.Version=$(VERSION)

TARGETS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: build test fmt vet check build-all clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/refigure

# -count=1 defeats the test cache. The command tests build the binary inside
# TestMain, so the cache cannot tell when their subject has changed.
test:
	go test -race -count=1 ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

check: vet test
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "needs gofmt:"; echo "$$unformatted"; exit 1; fi

# Cross-compiles every release target. CGO stays off so each one is a single
# static binary.
build-all:
	@mkdir -p dist
	@for target in $(TARGETS); do \
		os=$${target%/*}; arch=$${target#*/}; \
		out=dist/$(BINARY)_$${os}_$${arch}; \
		[ "$$os" = "windows" ] && out=$$out.exe; \
		echo "  $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath -ldflags "$(LDFLAGS)" -o $$out ./cmd/refigure || exit 1; \
	done
	@ls -lh dist/

clean:
	rm -rf dist $(BINARY)
