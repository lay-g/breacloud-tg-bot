BINARY  := bin/breacloud-tg-bot
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null)
LDFLAGS := -X github.com/lay-g/breacloud-tg-bot/internal/version.Version=$(VERSION) \
           -X github.com/lay-g/breacloud-tg-bot/internal/version.Commit=$(COMMIT)

.PHONY: build test lint vet run sqlc integration clean

CONFIG := $(HOME)/.config/breacloud-tg-bot/config.yaml

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run

# 用本地配置做一次离线冒烟：不发送 Telegram 消息，只打印通知内容。
run: build
	./$(BINARY) serve --dry-run --run-now

sqlc:
	sqlc generate

# 真实外部依赖的集成测试：只读调用 BreaCloud API，并向不存在的 chat 发送
# MarkdownV2 消息以校验语法。token 从本地配置里取，不落盘、不进日志。
integration:
	BREACLOUD_TEST_TOKEN="$$(sed -n 's/.*api_token: "\(.*\)"/\1/p' $(CONFIG))" \
	TELEGRAM_TEST_TOKEN="$$(sed -n 's/.*bot_token: "\(.*\)"/\1/p' $(CONFIG))" \
	go test -run Integration ./...

clean:
	rm -rf bin
