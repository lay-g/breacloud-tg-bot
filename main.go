package main

import (
	// 内嵌时区数据库：容器镜像里通常没有 /usr/share/zoneinfo，
	// 而本程序的报告时间与「今天/昨天」都依赖本地时区，缺了它就会静默退化成 UTC。
	_ "time/tzdata"

	"github.com/lay-g/breacloud-tg-bot/internal/cli"
)

func main() {
	cli.Execute()
}
