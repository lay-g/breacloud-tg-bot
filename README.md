# breacloud-tg-bot

用 Telegram 管理 [BreaCloud](https://brea.cloud) 账号下的 VPS。单账号自托管，Go 编写，零 CGO。

- 按区域查看 VPS，查看状态、配置、主 IP 与近 7 日用量
- 电源操作：开机、重启、关机、冷重启、冷关机（全部二次确认）
- 每天定时推送昨日流量报告：全网总量、环比、用量前五
- 流量接近配额时主动预警
- 周期机快到期时提醒

## 快速开始

需要 Go 1.27 或更高版本。

    make build
    ./bin/breacloud-tg-bot service install

`install` 会依次询问：

1. **Telegram Bot Token** — 从 [@BotFather](https://t.me/BotFather) 创建机器人后获得
2. **BreaCloud API Token** — 在 BreaCloud 后台创建的 `bll_` 开头令牌
3. **首个白名单 chat id** — 可以留空

Token 需要勾选这些接口权限（scope）：

- `client.svc.read.list`、`client.svc.read.get`
- `client.svc.read.traffic`、`client.svc.read.traffic_history`
- `client.svc.read.tasks`
- `client.svc.power.action`

安装完成后向机器人发送 `/start`。如果第一步留空了 chat id，此时会收到一条包含你自己 chat id 的提示，把它交给管理员，在已授权的会话里执行 `/allow <chat_id>` 即可。

### 开机自启

服务装在 systemd **用户实例**下，不需要 sudo。但用户服务默认随注销结束，要开机自启需要开启 linger：

    sudo loginctl enable-linger $USER

`install` 结束时会自动检查并提示。

查看日志：

    journalctl --user -u breacloud-tg-bot -f

## Docker 部署

    cp .env.example .env && chmod 600 .env && ${EDITOR:-vi} .env   # 填两个 token
    docker compose up -d
    docker compose logs -f

镜像默认从 GitHub Container Registry 拉取，名字就是仓库本身：`ghcr.io/lay-g/breacloud-tg-bot`。本地没有该镜像时 compose 会用本仓库源码构建一份，并打同一个名字，所以第一次 `docker compose up` 不需要先登录任何 registry。

镜像基于 distroless、以非 root 运行、不含 shell，体积约 15 MB。数据落在命名卷 `bot-data` 里，容器重建不丢白名单与设置。

**为什么用 `.env` 而不是挂配置文件**：容器以 uid 65532 运行，挂载宿主机上 0600 的配置文件它读不到，放宽到 644 又等于把 token 交给本机所有用户。环境变量绕开了这个矛盾，所以 Docker 这条路完全不需要配置文件。

只跑一次、不常驻（用于验证凭据与网络）：

    docker compose run --rm bot serve --dry-run --run-now

### 发布镜像

打 `v*` 标签推送后，`.github/workflows/docker.yml` 会用 `GITHUB_TOKEN` 自动构建
`linux/amd64` 与 `linux/arm64` 两份并推送到 ghcr，标签取自语义化版本。也可以手动触发。

手工推送：

    docker login ghcr.io -u <你的用户名>     # 密码用有 write:packages 权限的 PAT
    make docker-push VERSION=v1.0.0

首次推送后包默认是**私有**的；要在别的机器上拉取需要 `docker login ghcr.io`，或到仓库的 Packages 设置里改成 public。

**同一个 Bot token 不能同时被两个进程拉取更新。** 如果本机还跑着 systemd 服务，用 Docker 之前先 `breacloud-tg-bot service stop`。

时区必须显式设置（compose 文件里默认 `Asia/Shanghai`）：容器默认 UTC，不设的话你定的 09:00 会在北京时间 17:00 才推送。

## 命令行

    breacloud-tg-bot serve [--dry-run] [--run-now] [--traffic-interval 1h]
    breacloud-tg-bot service install|uninstall|start|stop|restart|status
    breacloud-tg-bot version

`serve --dry-run` 不创建 Telegram 客户端，把本该发送的通知打到标准输出；配合 `--run-now` 可以立刻跑一次日报。两者都不需要真的连上 Telegram，便于排错：

    ./bin/breacloud-tg-bot serve --dry-run --run-now

`service uninstall` 默认保留配置与数据库，加 `--purge` 一并删除。

## 机器人命令

    /menu       主菜单
    /vps        按区域查看 VPS
    /report     立即查看昨日流量报告
    /settings   设置
    /help       帮助

管理命令（只在 `/help` 里列出，不进菜单）：

    /allowlist           查看白名单
    /allow <chat_id>     添加白名单
    /deny  <chat_id>     移除白名单

机器人只响应**私聊**，拉进群不会工作。

## 配置

配置文件默认在 `~/.config/breacloud-tg-bot/config.yaml`，权限 0600，含密钥不要提交。模板见 `config.example.yaml`。

环境变量可以覆盖任何一项，前缀 `BREACLOUD_TG_BOT_`（例如 `BREACLOUD_TG_BOT_TELEGRAM_BOT_TOKEN`）。**配置文件可以完全不存在**——只用环境变量也能跑，Docker 部署走的就是这条路。

报告开关、通知时间、预警阈值这类可在机器人里改的设置存在 SQLite 的 `settings` 表（默认 `~/.local/share/breacloud-tg-bot/bot.db`），不进配置文件。

## 已知限制

- **区域是城市级**：列表接口只提供 `region_name`（例如 `Los Angeles`），线路级（例如 `LAX-GIA`）需要逐台调详情再经 `/meta/locations` 映射，而那个映射因主键重复不保证唯一。
- **业务日期按 UTC+8**：日用量口径由 BreaCloud 后端决定，不受主机时区影响；只有“通知时间”按主机本地时区。
- **日用量只用 `range=week`**：`range=day` 的“昨天”桶会被服务端 24 小时窗口截断，系统性少算约 37%。
- **流量单位是 GiB**：接口的 `quota_gb` / `used_gb` 是 1024 进制却写作 GB，界面上与面板保持一致。
- 大账号（数百台）首次全量取数需要几十秒，预警检查间隔不宜低于 1 小时。
- 消息使用 MarkdownV2，所有动态文本都会转义；万一模板出错，发送侧会降级为纯文本并把错误记进日志。

## 开发

    make build       # 编译到 bin/
    make test        # go test ./...（离线，几秒内跑完）
    make lint        # golangci-lint
    make sqlc        # 重新生成 sqlc 代码
    make integration # 打真实 BreaCloud / Telegram API 的测试
    make docker      # 构建容器镜像（名字从 git 远端推导）
    make docker-push # 推送到 ghcr.io
    make docker-run  # 在容器里离线跑一次日报
    make check-image # 断言 Makefile 与 compose 的镜像名一致

改动 schema 或查询后必须重跑 `make sqlc` 并提交生成结果。

集成测试（真实 BreaCloud API + 真实 Telegram API）：

    make integration

它会从本地配置里读出 token 传给测试：BreaCloud 侧只做只读调用；Telegram 侧只向
不存在的 chat 发消息，用来校验渲染出来的 MarkdownV2 语法合法。

也可以手工指定：

    BREACLOUD_TEST_TOKEN=bll_xxx TELEGRAM_TEST_TOKEN=123:abc go test -run Integration ./...

两个环境变量都没设置时这些测试自动跳过，因此 `go test ./...` 默认不联网。

文档：

- `docs/design/` — 按模块的设计与取舍理由
- `docs/references/` — 外部接口参考
- `docs/rules/` — 踩过的坑与解决办法
- `AGENTS.md` — 本仓库的开发约定
