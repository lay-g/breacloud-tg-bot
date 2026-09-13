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

## 镜像名有两处来源，容易漂移

**现象**：仓库改名或 fork 之后，Makefile 从 git 远端推导出的镜像名与 `docker-compose.yml` 里写死的默认值不再一致；`make docker` 推的镜像和 compose 拉的不是同一个。

**原因**：Makefile 能读 git 远端，compose 不能——它只能靠 `${IMAGE:-...}` 的静态默认值。

**解决**：保留两处，但加 `make check-image` 断言它们相等，并挂到 `docker` / `docker-push` 前面。仓库改名时这一步会直接失败并指出两个值。

**相关文件**：`Makefile`、`docker-compose.yml`

## distroless 里没有 shell，排错要用别的镜像

**现象**：`docker exec -it breacloud-tg-bot sh` 报 `exec: "sh": executable file not found`。

**原因**：distroless 只包含运行所需的东西，没有 shell 与 coreutils，这是它体积小、攻击面小的原因。

**解决**：需要看卷里的文件时，把同一个卷挂进一个有 shell 的镜像：

    docker run --rm -v bot-data:/data golang:1.27-bookworm ls -ln /data

**相关文件**：`Dockerfile`

## Portainer stack 里 `build:` 与 `env_file: .env` 都不可用

**现象**：把根目录 `docker-compose.yml` 原样粘进 Portainer 的 Web editor，部署直接失败：`env file .env not found`（compose 在 Portainer 的临时目录里执行），或者镜像拉不下来（`build: context: .` 在 Portainer 里没有构建上下文）。

**原因**：Portainer 只保存 compose 文本，没有本仓库的工作目录，因此相对路径的 `env_file`、`build` 全部失效；镜像只能从 registry 拉。

**解决**：Portainer 用单独的 `docker-compose.portainer.yml`——去掉 `build:` 与 `env_file:`，凭据改在 Stack 的 Environment variables 面板填写、由 `${VAR}` 引用；命名卷显式写 `name:`，这样 stack 改名重建也还是同一个卷。ghcr 上的包是私有的话要先在 Portainer 的 Registries 里加凭据。

**相关文件**：`docker-compose.portainer.yml`、`README.md`
