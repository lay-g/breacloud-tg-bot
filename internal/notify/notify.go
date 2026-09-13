// Package notify 定义通知接口，把「任务算出来的内容」与「怎么发出去」解耦。
//
// 两个实现：bot.Bot（真实发送给 Telegram）与 DryRun（打到 stdout）。
// 定时任务因此可以在没有 Bot token 的环境里完整跑一遍。
package notify

import (
	"context"
	"fmt"
	"io"
)

// Notifier 是任务层唯一需要的发送能力。
type Notifier interface {
	// Send 发给单个 chat。
	Send(ctx context.Context, chatID int64, text string) error
	// Broadcast 发给白名单内全部 chat。
	Broadcast(ctx context.Context, text string) error
}

// DryRun 把消息打到标准输出而不真的发送。
type DryRun struct {
	// Chats 用于 Broadcast，返回白名单内的 chat id。
	Chats func(ctx context.Context) ([]int64, error)
	Out   io.Writer
}

// Send 打印一条消息。
func (d *DryRun) Send(_ context.Context, chatID int64, text string) error {
	_, err := fmt.Fprintf(d.Out, "\n----- dry-run 通知 chat=%d -----\n%s\n", chatID, text)
	return err
}

// Broadcast 打印每条消息，前缀带上目标 chat，方便确认广播范围。
func (d *DryRun) Broadcast(ctx context.Context, text string) error {
	if d.Chats == nil {
		return d.Send(ctx, 0, text)
	}
	chats, err := d.Chats(ctx)
	if err != nil {
		return err
	}
	if len(chats) == 0 {
		_, err := fmt.Fprintf(d.Out, "\n----- dry-run 广播（白名单为空，未投递）-----\n%s\n", text)
		return err
	}
	for _, chatID := range chats {
		if err := d.Send(ctx, chatID, text); err != nil {
			return err
		}
	}
	return nil
}
