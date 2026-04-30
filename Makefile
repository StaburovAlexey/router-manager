GO ?= /home/gilbertfrost/.local/go/bin/go
DIST_DIR ?= dist
TARGET ?= ubuntu@192.168.0.16
REMOTE_BIN ?= /usr/local/sbin/router-manager
VERSION ?= dev
LDFLAGS ?= -s -w -X main.version=$(VERSION)

.PHONY: test build build-amd64 build-arm64 build-all deploy clean

test:
	$(GO) test ./...

build: build-amd64

build-amd64:
	mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/router-manager-linux-amd64 ./cmd/router-manager
	sha256sum $(DIST_DIR)/router-manager-linux-amd64

build-arm64:
	mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/router-manager-linux-arm64 ./cmd/router-manager
	sha256sum $(DIST_DIR)/router-manager-linux-arm64

build-all: build-amd64 build-arm64
	(cd $(DIST_DIR) && sha256sum router-manager-linux-amd64 router-manager-linux-arm64 > checksums.txt && cat checksums.txt)

deploy: build-amd64
	ssh $(TARGET) 'cat > /tmp/router-manager' < $(DIST_DIR)/router-manager-linux-amd64
	ssh $(TARGET) 'chmod +x /tmp/router-manager && sudo install -m 755 /tmp/router-manager $(REMOTE_BIN) && sha256sum $(REMOTE_BIN)'

clean:
	rm -rf $(DIST_DIR)
