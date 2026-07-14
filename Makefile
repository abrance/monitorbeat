# Makefile — monitorbeat
# 占位骨架，P0 阶段填充实际 target

GO        ?= go
GOFLAGS   ?=
BIN_DIR   ?= bin
BIN_NAME  ?= monitorbeat
PKG       := ./...

.PHONY: help build test lint docker clean

help: ## 显示帮助
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## 编译二进制到 bin/
	@echo "(P0 待实现) $(GO) build -o $(BIN_DIR)/$(BIN_NAME) ./cmd/monitorbeat"

test: ## 运行单测
	@echo "(P0 待实现) $(GO) test $(GOFLAGS) $(PKG)"

lint: ## 静态检查
	@echo "(P0 待实现) $(GO) vet $(PKG) && golangci-lint run"

docker: ## 构建镜像
	@echo "(P2 待实现) docker build -t monitorbeat:latest ."

clean: ## 清理
	rm -rf $(BIN_DIR)