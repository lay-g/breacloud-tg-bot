# Docker 与容器部署

## 非 root 容器读不到宿主机上 0600 的配置文件

**现象**：把 `~/.config/breacloud-tg-bot/config.yaml`（0600）挂进容器后启动失败：

    ​错误: 读取配置文件 /config/config.yaml: open /config/config.yaml: permission denied

**原因**：镜像以 uid 65532（distroless 的 nonroot）运行，而配置文件属主是宿主机的 1000，权限 0600 意味着其他人一律不可读。

**解决**：容器部署改用环境变量传凭据（`BREACLOUD_TG_BOT_TELEGRAM_BOT_TOKEN` 等），由 `env_file: .env` 注入，容器内不落地任何密钥文件。

不要为此把配置文件放宽到 644：那等于把两个 token 交给本机所有用户，是拿安全换方便。

**相关文件**：`docker-compose.yml`、`.env.example`、`internal/config/config.go`

## 命名卷初始化后的属主由镜像决定

**现象**：容器把自己挂载的命名卷当成空目录初始化时，目录属主会继承镜像里同名路径的属主；如果镜像里压根没有这个路径，卷会归 root 所有，非 root 进程写不进去。

**原因**：Docker 用镜像中该路径的权限与属主来初始化新卷。

**解决**：在构建阶段建好 `/out/data`，再用 `COPY --chown=65532:65532` 拷进镜像；这样 `docker run` 首次创建卷时目录就是可写的。

**相关文件**：`Dockerfile`

## distroless 镜像没有时区数据库

**现象**：容器里日志时间是 UTC，报告推送时间比预期晚 8 小时。

**原因**：精简镜像不带 `/usr/share/zoneinfo`，`time.Local` 退化成 UTC。

**解决**：两道保险——

1. `main.go` 里 `import _ "time/tzdata"`，把时区数据库编进二进制（约 450 KB），任何基础镜像都不再依赖；
2. compose 文件显式设置 `TZ`。

二进制内嵌之后仍然需要设 `TZ`：内嵌的是数据，不是默认值。

**相关文件**：`main.go`、`docker-compose.yml`

## 同一个 Bot token 不能被两个进程同时拉取更新

**现象**：systemd 服务与容器同时运行，其中一个的日志开始刷 `Conflict: terminated by other getUpdates request`。

**原因**：Telegram 的 `getUpdates` 是单消费者模型，一个 token 只允许一个进程长轮询。

**解决**：两种部署方式只能选一种。从 systemd 迁到 Docker 时先 `breacloud-tg-bot service stop`；反过来也是。

**相关文件**：`README.md`

## distroless 里没有 shell，排错要用别的镜像

**现象**：`docker exec -it breacloud-tg-bot sh` 报 `exec: "sh": executable file not found`。

**原因**：distroless 只包含运行所需的东西，没有 shell 与 coreutils，这是它体积小、攻击面小的原因。

**解决**：需要看卷里的文件时，把同一个卷挂进一个有 shell 的镜像：

    docker run --rm -v bot-data:/data golang:1.27-bookworm ls -ln /data

**相关文件**：`Dockerfile`
