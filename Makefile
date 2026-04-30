GO ?= /home/gilbertfrost/.local/go/bin/go
DIST_DIR ?= dist
TARGET ?= ubuntu@192.168.0.16
REMOTE_BIN ?= /usr/local/sbin/vpn-router

.PHONY: test build build-amd64 build-arm64 build-all deploy clean

test:
	$(GO) test ./...

build: build-amd64

build-amd64:
	mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -o $(DIST_DIR)/vpn-router-linux-amd64 ./cmd/vpn-router
	sha256sum $(DIST_DIR)/vpn-router-linux-amd64

build-arm64:
	mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -o $(DIST_DIR)/vpn-router-linux-arm64 ./cmd/vpn-router
	sha256sum $(DIST_DIR)/vpn-router-linux-arm64

build-all: build-amd64 build-arm64

deploy: build-amd64
	ssh $(TARGET) 'cat > /tmp/vpn-router' < $(DIST_DIR)/vpn-router-linux-amd64
	ssh $(TARGET) 'chmod +x /tmp/vpn-router && sudo install -m 755 /tmp/vpn-router $(REMOTE_BIN) && sha256sum $(REMOTE_BIN)'

clean:
	rm -rf $(DIST_DIR)
