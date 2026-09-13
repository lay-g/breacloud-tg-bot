# Go 工具链与环境

## golangci-lint 的构建 Go 版本必须不低于项目 Go 版本

**现象**：`golangci-lint run` 直接失败：`can't load config: the Go language version (go1.26) used to build golangci-lint is lower than the targeted Go version (1.27.1)`。

**原因**：golangci-lint 是用 Go 1.26 编译的二进制，无法分析声明 go1.27 的模块。

**解决**：用本机工具链重建：

    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

**相关文件**：`AGENTS.md`、`Makefile`

## `go run` 的二进制不能注册为系统服务

**现象**：`go run . service install` 会把单元文件的 `ExecStart` 指向 `~/.../go-buildXXXX/b001/exe/...`，构建缓存清理后服务就起不来。

**原因**：`go run` 先编译到临时目录再执行。

**解决**：`service install` 检查 `os.Executable()`，路径含 `go-build` 或以系统临时目录开头时直接拒绝并提示先 `go build -o bin/<name> .`。

**相关文件**：`internal/cli/service.go`

## 错误字符串的首字母大写与结尾标点会被 staticcheck 拦下

**现象**：`golangci-lint` 报 `ST1005: error strings should not be capitalized` / `should not end with punctuation`。

**原因**：Go 的错误信息约定小写开头、无结尾标点。

**解决**：错误信息写成 `bot token 格式不正确，应形如 123456:ABC-DEFG` 这种形式；专有名词放到冒号之后或用小写表述。

**相关文件**：`internal/cli/service.go`
