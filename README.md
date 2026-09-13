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

配置文件默认在 `~/.config/breacloud-tg-bot/config.yaml`，权限 0600，含密钥不要提交。环境变量可以覆盖任何一项，前缀 `BREACLOUD_TG_BOT_`（例如 `BREACLOUD_TG_BOT_TELEGRAM_BOT_TOKEN`）。

报告开关、通知时间、预警阈值这类可在机器人里改的设置存在 SQLite 的 `settings` 表（默认 `~/.local/share/breacloud-tg-bot/bot.db`），不进配置文件。

## 已知限制

- **区域是城市级**：列表接口只提供 `region_name`（例如 `Los Angeles`），线路级（例如 `LAX-GIA`）需要逐台调详情再经 `/meta/locations` 映射，而那个映射因主键重复不保证唯一。
- **业务日期按 UTC+8**：日用量口径由 BreaCloud 后端决定，不受主机时区影响；只有“通知时间”按主机本地时区。
- **日用量只用 `range=week`**：`range=day` 的“昨天”桶会被服务端 24 小时窗口截断，系统性少算约 37%。
- **流量单位是 GiB**：接口的 `quota_gb` / `used_gb` 是 1024 进制却写作 GB，界面上与面板保持一致。
- 大账号（数百台）首次全量取数需要几十秒，预警检查间隔不宜低于 1 小时。

## 开发

    make build    # 编译到 bin/
    make test     # go test ./...
    make lint     # golangci-lint
    make sqlc     # 重新生成 sqlc 代码

改动 schema 或查询后必须重跑 `make sqlc` 并提交生成结果。

真实 API 的只读集成测试：

    BREACLOUD_TEST_TOKEN=bll_xxx go test -run Integration ./internal/breacloud/

未设置该环境变量时会回退读取默认配置文件里的 token，两者都没有则跳过。

文档：

- `docs/design/` — 按模块的设计与取舍理由
- `docs/references/` — 外部接口参考
- `docs/rules/` — 踩过的坑与解决办法
- `AGENTS.md` — 本仓库的开发约定
