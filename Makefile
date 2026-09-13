BINARY  := bin/breacloud-tg-bot
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null)
LDFLAGS := -X github.com/lay-g/breacloud-tg-bot/internal/version.Version=$(VERSION) \
           -X github.com/lay-g/breacloud-tg-bot/internal/version.Commit=$(COMMIT)

.PHONY: build test lint vet run sqlc clean

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

clean:
	rm -rf bin
