# systemd 用户服务

## `systemctl --user status` 对未运行的服务返回退出码 3

**现象**：`service status` 在服务未运行时输出 `错误: exit status 3` 并以退出码 1 结束，把 3 这个有意义的语义吞掉了。

**原因**：cobra 的 `RunE` 返回的错误被统一打印。

**解决**：`Execute()` 里对 `*exec.ExitError` 直接沿用退出码、不额外打印——子命令的诊断信息已经写到终端了。

**相关文件**：`internal/cli/root.go`

## 用户服务的单元文件位置不跟随 `XDG_CONFIG_HOME`

**现象**：把 `XDG_CONFIG_HOME` 指到别处后，单元文件按该路径写入，systemd 找不到。

**原因**：systemd 用户实例固定从 `~/.config/systemd/user` 读取单元。

**解决**：`config.UnitPath()` 硬编码 `~/.config/systemd/user/<name>.service`，只有应用自己的配置与数据目录才跟随 XDG。

**相关文件**：`internal/config/paths.go`

## 用户服务注销后会被停掉，需要 linger

**现象**：`enable` 之后服务正常，但用户注销后服务停止，也没有开机自启。

**原因**：systemd 用户实例随登录会话结束而终止。

**解决**：`service install` 结束时用 `loginctl show-user <user> -p Linger` 检查并提示 `sudo loginctl enable-linger $USER`；不自动提权（安装流程无 sudo）。

**相关文件**：`internal/systemd/systemd.go`、`internal/cli/service.go`

## `Restart=always` 要求进程常驻

**现象**：单元刚装好时 `Active: activating (auto-restart)`，`journalctl` 里每 5 秒一次启动日志。

**原因**：`serve` 启动后立即退出，被 `Restart=always` 反复拉起。

**解决**：`serve` 必须阻塞到收到 SIGINT/SIGTERM 才返回；只能在拿到明确退出信号或致命配置错误时结束进程。

**相关文件**：`internal/cli/serve.go`
