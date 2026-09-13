# systemd — 用户级服务

## 职责

渲染 systemd 用户单元、调用 `systemctl --user`、检查 linger 状态。只处理「怎么成为一个服务」，不关心服务里跑什么。

## 为什么用用户级单元

需求明确不需要 sudo。用户级单元满足这一点，代价是要处理 linger：用户服务默认随登录会话终止，注销后会被停掉，也不会在开机时（无人登录的情况下）启动。

因此 `install` 结束时会检查 linger 并给出提示，但**不自动提权**——`sudo loginctl enable-linger $USER` 交给用户自己执行。

## 单元模板

    [Unit]
    Description=BreaCloud Telegram Bot
    After=network-online.target

    [Service]
    Type=simple
    ExecStart=<二进制绝对路径> serve --config <配置文件绝对路径>
    Restart=always
    RestartSec=5
    StandardOutput=journal
    StandardError=journal

    [Install]
    WantedBy=default.target

要点：

- `ExecStart` 用绝对路径。`install` 会先 `os.Executable()` 再 `EvalSymlinks`，并把 `go run` 临时目录的情况挡在前面（见 `docs/design/cli.md`）。
- 路径含空格或引号时按 systemd 规则加引号并转义，避免单元被解析成两个参数。
- `Restart=always` 要求 `serve` 常驻；如果 `serve` 启动后立刻退出，会变成 5 秒一次的重启循环。
- 日志走 journal，用 `journalctl --user -u breacloud-tg-bot -f` 查看。

## 安装流程

1. 检查 `systemctl` 在 PATH 中，否则报错退出（例如在容器里）。
2. 单元文件已存在且未指定 `--force` 时拒绝覆盖，避免无声地改掉别人的配置。
3. 创建 `~/.config/systemd/user`，写单元文件（0644）。
4. `systemctl --user daemon-reload` → `systemctl --user enable --now breacloud-tg-bot.service`。

任一步失败都会把具体命令和错误一起返回，便于定位是权限、会话还是单元语法问题。

## 卸载流程

`disable --now` 的失败**不视为错误**：服务本来就可能没在运行，这里只打印提示然后继续。真正必须成功的是删除单元文件与 `daemon-reload`。

默认保留配置与数据库；`--purge` 才会一并删除，且在删除前打印完整路径清单并要求输入 `yes`。

## 退出码转发

`service start/stop/restart/status` 直接把 `systemctl` 的退出码透传给调用者。特别是 `status`：服务未运行时 systemd 返回 3，这个语义对脚本有用，因此不能吞掉也不该包装成「错误: exit status 3」。

## 可测试性

包内保留 `runCommand` 与 `lookPath` 两个可替换变量。测试里替换掉它们即可断言：

- 单元内容包含 `ExecStart`、`Restart=always`、`WantedBy=default.target`；
- 含空格的路径被正确加引号；
- 安装写出的文件权限是 0644，且调用序列是 `daemon-reload` → `enable --now`；
- 已存在单元且未加 `--force` 时安装被拒绝；
- 卸载会删除单元文件，未加 `--purge` 时保留配置文件。

因此测试不需要真的安装一个服务，也不会污染开发机的 systemd。
