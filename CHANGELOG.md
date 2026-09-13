# 变更日志

本项目所有值得关注的变更都记录在这里。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本号遵循[语义化版本](https://semver.org/lang/zh-CN/)。

## [v0.1.0] - 2026-09-13

首个正式版本。

### 新增

- 按区域浏览账号下的 VPS：状态、配置、主 IP 与近 7 日用量。
- 电源操作：开机、重启、关机、冷重启、冷关机，全部二次确认后才执行。
- 每日流量报告：在设定的时间推送昨天的全网总量、环比与用量前五；`/report` 可随时手动查看。
- 流量预警：用量接近配额时主动推送，阈值在 70% / 80% / 90% / 100% 中任选。
- 到期提醒：周期机临近到期日时提醒，文案区分自动续费、需手动续费与到期释放。
- 访问控制：白名单机制（`/allow`、`/deny`、`/allowlist`），机器人只响应私聊。
- 设置面板 `/settings`：报告开关与时间、预警开关与阈值、提前天数都能在机器人里改。
- 两套部署方式：systemd 用户服务（`service install`）与 Docker Compose / Portainer stack；
  镜像基于 distroless、以非 root 运行，多架构发布到 GitHub Container Registry。
- 离线排错：`serve --dry-run --run-now` 不连 Telegram 就能跑一次日报。

[v0.1.0]: https://github.com/lay-g/breacloud-tg-bot/releases/tag/v0.1.0
