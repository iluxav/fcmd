BINARY   := fcmd
PORT     ?= 7891
VERSION  ?= dev
LDFLAGS  := -s -w -X main.version=$(VERSION)
GOFLAGS  := -trimpath -ldflags "$(LDFLAGS)"

.PHONY: build run daemon tui smoketest vet tidy test clean cross install uninstall

build:
	go build $(GOFLAGS) -o $(BINARY) .

run: tui

tui: build
	./$(BINARY)

daemon: build
	./$(BINARY) run -port $(PORT)

smoketest:
	go run ./cmd/smoketest

vet:
	go vet ./...

tidy:
	go mod tidy

test:
	go test ./...

clean:
	rm -f $(BINARY) $(BINARY).exe
	rm -rf dist

# Cross-compile release artifacts into ./dist (mirrors the GitHub Actions matrix).
cross: clean
	@mkdir -p dist
	@set -e; for target in \
	    linux/amd64 linux/arm64 \
	    darwin/amd64 darwin/arm64 \
	    windows/amd64; do \
	  os=$${target%%/*}; arch=$${target##*/}; ext=""; \
	  [ "$$os" = "windows" ] && ext=".exe"; \
	  out=dist/$(BINARY)_$(VERSION)_$${os}_$${arch}$${ext}; \
	  echo "-> $$out"; \
	  GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build $(GOFLAGS) -o $$out .; \
	done

install: build
	install -m 0755 $(BINARY) /usr/local/bin/$(BINARY)

uninstall:
	rm -f /usr/local/bin/$(BINARY)
