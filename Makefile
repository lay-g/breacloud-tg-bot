BINARY  := bin/breacloud-tg-bot
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null)

# 镜像名从 git 远端推导，仓库迁移或 fork 后不需要手改：
#   https://github.com/lay-g/breacloud-tg-bot.git -> ghcr.io/lay-g/breacloud-tg-bot
#   git@github.com:lay-g/breacloud-tg-bot.git     -> 同上
# 非 GitHub 远端（或不在 git 仓库里）时回退成本地镜像名。
GITHUB_REPO := $(shell git remote get-url origin 2>/dev/null | \
	sed -nE 's#^(git@github\.com:|https?://github\.com/)(.*)$$#\2#p' | \
	sed -E 's#\.git$$##' | grep -E '^[^/]+/[^/]+$$')
IMAGE ?= $(or $(IMAGE_OVERRIDE),$(if $(GITHUB_REPO),ghcr.io/$(GITHUB_REPO),breacloud-tg-bot))
LDFLAGS := -X github.com/lay-g/breacloud-tg-bot/internal/version.Version=$(VERSION) \
           -X github.com/lay-g/breacloud-tg-bot/internal/version.Commit=$(COMMIT)

.PHONY: build test lint vet run sqlc integration check-image docker docker-push docker-run clean

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

# 镜像名在两处出现：Makefile 从 git 远端推导，docker-compose.yml 里写死默认值。
# 这个断言保证两者不会悄悄漂移（仓库改名或 fork 后最容易忘掉一处）。
check-image:
	@derived='$(IMAGE)'; \
	composed=$$(sed -nE 's/.*IMAGE:-([^}]+).*/\1/p' docker-compose.yml | head -1); \
	if [ "$$derived" != "$$composed" ]; then \
		echo "镜像名不一致：Makefile 推导出 $$derived，docker-compose.yml 默认值是 $$composed"; \
		exit 1; \
	fi; \
	echo "镜像名一致：$$derived"

# 构建容器镜像。VERSION 会注入二进制，默认取 git describe。
docker: check-image
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) \
		-t $(IMAGE):$(VERSION) .

# 推送到 GitHub Container Registry。需要先 docker login ghcr.io，
# 凭据用有 write:packages 权限的 Personal Access Token。
# 打 tag 推送时 GitHub Actions 也会自动构建多架构镜像，见 .github/workflows/docker.yml。
docker-push: docker
	docker push $(IMAGE):$(VERSION)
	@echo "已推送 $(IMAGE):$(VERSION)"

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
		$(IMAGE):$(VERSION) serve --dry-run --run-now

clean:
	rm -rf bin
