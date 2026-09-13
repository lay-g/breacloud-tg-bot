BINARY  := bin/breacloud-tg-bot
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null)
LDFLAGS := -X github.com/lay-g/breacloud-tg-bot/internal/version.Version=$(VERSION) \
           -X github.com/lay-g/breacloud-tg-bot/internal/version.Commit=$(COMMIT)

.PHONY: build test lint vet run sqlc integration docker docker-run clean

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

# 构建容器镜像。VERSION 会注入二进制，默认取 git describe。
docker:
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) \
		-t breacloud-tg-bot:$(VERSION) .

# 在容器里离线跑一次日报（不连 Telegram），用于验证镜像与配置是否可用。
#
# 凭据走环境变量而不是挂载配置文件：容器以非 root(65532) 运行，
# 宿主机的 0600 配置文件它读不到。
docker-run: docker
	docker run --rm \
		-e TZ=$$(timedatectl show -p Timezone --value 2>/dev/null || echo UTC) \
		-e BREACLOUD_TG_BOT_DATABASE_PATH=/data/bot.db \
		-e BREACLOUD_TG_BOT_TELEGRAM_BOT_TOKEN="$$(sed -n 's/.*bot_token: "\(.*\)"/\1/p' $(CONFIG))" \
		-e BREACLOUD_TG_BOT_BREACLOUD_API_TOKEN="$$(sed -n 's/.*api_token: "\(.*\)"/\1/p' $(CONFIG))" \
		breacloud-tg-bot:$(VERSION) serve --dry-run --run-now

clean:
	rm -rf bin
