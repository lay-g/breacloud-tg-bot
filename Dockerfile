# 多阶段构建：静态编译的纯 Go 二进制，运行在 distroless 上。
#
#   docker build -t breacloud-tg-bot .
#   docker build --build-arg VERSION=v1.0.0 --build-arg COMMIT=$(git rev-parse --short HEAD) -t breacloud-tg-bot .
#
# 用 modernc.org/sqlite 而不是 mattn/go-sqlite3，所以 CGO 关掉也能跑，
# 镜像里不需要 libc。
#
# 不设 GOOS/GOARCH：镜像按本机架构构建。多架构镜像由 CI 在各自架构的 runner
# 上分别构建，见 .github/workflows/docker.yml。
#
# 版本号默认取自根目录 VERSION 文件（形如 v0.1.0）；--build-arg VERSION=v0.2.0 可覆盖。

FROM golang:1.27-bookworm AS build

WORKDIR /src

# 依赖单独一层：只要 go.mod/go.sum 没变，改代码不会重新下载模块
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=
ARG COMMIT=

RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w \
        -X github.com/lay-g/breacloud-tg-bot/internal/version.Version=${VERSION:-$(cat VERSION)} \
        -X github.com/lay-g/breacloud-tg-bot/internal/version.Commit=${COMMIT}" \
      -o /out/breacloud-tg-bot .

# distroless 没有 shell，因此数据目录必须在构建阶段建好并带上正确的属主，
# 否则命名卷由 Docker 初始化时会变成 root 所有，非 root 进程写不进去。
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/breacloud-tg-bot /usr/local/bin/breacloud-tg-bot
COPY --chown=65532:65532 --from=build /out/data /data

# 配置目录由使用者挂载；数据库固定落在 /data。
VOLUME ["/data"]

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/breacloud-tg-bot"]
CMD ["serve", "--config", "/config/config.yaml"]
