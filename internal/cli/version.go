package cli

import (
	"fmt"

	"github.com/lay-g/breacloud-tg-bot/internal/config"
	"github.com/lay-g/breacloud-tg-bot/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "显示版本信息",
		RunE: func(*cobra.Command, []string) error {
			info := version.Version
			if version.Commit != "" {
				info += " (" + version.Commit + ")"
			}
			fmt.Printf("%s %s\n", config.AppName, info)
			return nil
		},
	}
}
