# config — 配置

## 职责

加载 `config.yaml`、叠加环境变量、填充默认值、解析 XDG 路径，并在启动前把「缺什么」一次说清楚。

## 配置来源优先级

环境变量 > 配置文件 > 内置默认值。

环境变量前缀 `BREACLOUD_TG_BOT_`，层级用下划线连接：`BREACLOUD_TG_BOT_TELEGRAM_BOT_TOKEN` 对应 `telegram.bot_token`。这条通道主要为容器/临时调试准备，systemd 部署走配置文件。

## 字段

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `telegram.bot_token` | 无 | BotFather 提供的 token，必填 |
| `telegram.owner_chat_id` | 0 | 首个白名单 chat id，可留空 |
| `breacloud.base_url` | `https://brea.cloud/api/v1` | 末尾斜杠会被去掉 |
| `breacloud.api_token` | 无 | `bll_` 前缀，必填 |
| `breacloud.concurrency` | 5 | 并发请求上限，小于 1 时钳到 1 |
| `breacloud.timeout` | `20s` | 单请求超时，非正数时回落到 20s |
| `database.path` | `$XDG_DATA_HOME/breacloud-tg-bot/bot.db` | SQLite 文件 |
| `log.level` | `info` | debug / info / warn / error |

## 路径

- 配置目录：`os.UserConfigDir()/breacloud-tg-bot`，即 `$XDG_CONFIG_HOME` 或 `~/.config`。
- 数据目录：`$XDG_DATA_HOME/breacloud-tg-bot`，回退 `~/.local/share`。
- 数据库：数据目录下的 `bot.db`（WAL 模式会同时产生 `-wal` / `-shm`）。
- 单元文件：**固定** `~/.config/systemd/user/breacloud-tg-bot.service`，不跟随 `XDG_CONFIG_HOME`，因为 systemd 用户实例只认这个位置。

## 校验

`Validate()` 一次性收集缺失项再报错，避免用户改一次跑一次。检查：两个 token 非空、`database.path` 非空、API token 以 `bll_` 开头。并发与超时这类可自愈的值在加载阶段就钳到合法范围，不让它们变成启动失败。

## 为什么密钥在文件、设置却在数据库

配置文件只放**密钥与基础设施参数**，所有能在 Bot 里改的东西（报告开关与时间、流量预警开关与阈值、到期预警开关与提前天数）放在 SQLite 的 `settings` 表。

原因：程序回写 YAML 会破坏注释与格式，还要处理并发写；而密钥不该进数据库，因为数据库会被随手备份、复制、拷进容器。两条边界都清晰。

## owner_chat_id 的作用

`chats` 表为空时 Bot 无法响应任何人（白名单是唯一入口），会形成死锁。解决办法不是「第一条消息自动授权」（那等于没有白名单），而是：

1. `service install` 把首个 chat id 写进 `telegram.owner_chat_id`；
2. 程序启动时把它幂等播种到 `chats` 表。

好处是数据库丢失后仍能从配置文件恢复，且安装流程不需要依赖持久层（M1 阶段持久层还不存在，这个设计让 M1 能独立完成）。

留空也没关系：未授权 chat 发 `/start` 会收到一条提示，其中包含它自己的 chat id，把该 id 用 `--chat-id` 重跑一次 install，或在已授权的 chat 里执行 `/allow <id>` 即可。

## 相关文档

- 路径与环境变量：本文件「路径」一节
- 服务单元：`docs/design/systemd.md`
