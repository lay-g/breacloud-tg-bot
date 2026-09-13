# 项目约定

BreaCloud 账号的 Telegram 管理机器人（Go）。

- 设计文档：`docs/design/`（按模块，含取舍理由）
- 接口参考：`docs/references/`（外部 API 的 URL、字段与实测行为）
- 问题记录：`docs/rules/`（见下）

## 开发

常用命令：`make build` / `make test` / `make lint` / `make run`。

任何改动落地前至少通过：

    go build ./... && go vet ./... && go test ./... && golangci-lint run

## 问题记录（强制）

遇到值得记录的问题时，把**问题与解决方案**写进 `docs/rules/`，按问题领域分文件保存：

- 每个领域一个 Markdown 文件，例如 `docs/rules/telegram.md`、`docs/rules/sqlite.md`；文件不存在就新建。
- 每条记录包含四段：现象（可观察到的失败）、原因、解决方案、相关文件。
- 只记会再次踩到的东西：平台或依赖的反直觉行为、环境陷阱、必须绕过的限制。一次性笔误、纯代码 bug 不记。
- 新建领域文件要登记到 `docs/rules/README.md` 的索引。
- 记录与对应的实现改动一起提交，不要攒到最后补。

## 不可违反的约定

完整的跨模块不变量见 `docs/design/README.md`，以下几条尤其容易违反：

- 面向 Telegram 的消息一律用 MarkdownV2：任何来自接口或用户的动态文本都必须先过 `md.Escape`（或包成 `md.Code`），否则整条消息会被 Telegram 拒收。
- 任何 MarkdownV2 实体都必须在**一行内**闭合，因为超长消息按行截断。
- 业务日期一律按 UTC+8 自然日理解；日用量只用 `traffic-history?range=week`，不用 `range=day`。
- 流量配额单位是 GiB（比较时 `quota_gb << 30`）。
- `POST /services/:id/actions` 非幂等，禁止自动重试。
- 密钥只存在于 `~/.config/breacloud-tg-bot/config.yaml`（systemd 部署）或 `.env`（Docker 部署，见 `.env.example`）：不进仓库、不进日志、不进数据库、不进容器镜像。
