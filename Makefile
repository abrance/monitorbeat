# Makefile — monitorbeat

GO        ?= go
GOFLAGS   ?=
BIN_DIR   ?= bin
BIN_NAME  ?= monitorbeat
PKG       := ./...
VERSION   ?= dev
LDFLAGS   := -s -w -X main.version=$(VERSION)

# Web 服务 (monitorweb) 目标
WEB_DIR     ?= web/ui
WEB_BIN     ?= monitorweb
VM_UI_BIN   ?= $(BIN_DIR)/$(WEB_BIN)

.PHONY: help build test test-verbose vet lint fmt docker clean
.PHONY: web-ui web-ui-install monitorweb web web-clean

help:
	@grep -E '^[a-zA-Z_-]+:.*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ": "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BIN_NAME) ./cmd/monitorbeat

test:
	$(GO) test $(GOFLAGS) $(PKG)

test-verbose:
	$(GO) test $(GOFLAGS) -v -count=1 $(PKG)

vet:
	$(GO) vet $(PKG)

lint: vet
	@gofmt -l . | grep -q . && echo "ERROR: unformatted files:" && gofmt -l . && exit 1 || true
	@echo "gofmt: clean"

fmt:
	gofmt -w .

docker:
	docker build -t monitorbeat:$(VERSION) -t monitorbeat:latest .

clean:
	rm -rf $(BIN_DIR)

# ---------- Web 服务 (P3) ----------

web-ui-install:
	cd $(WEB_DIR) && npm install

# 构建前端（产出 web/ui/dist，由 monitorweb 托管）
web-ui: web-ui-install
	cd $(WEB_DIR) && npm run build

# 构建 monitorweb Go 二进制
monitorweb:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(VM_UI_BIN) ./cmd/monitorweb

# 一次性构建前后端
web: web-ui monitorweb

# 仅前端安装依赖（开发用）
web-dev:
	cd $(WEB_DIR) && npm run dev

web-clean:
	rm -rf $(WEB_DIR)/dist $(WEB_DIR)/node_modules $(VM_UI_BIN)
