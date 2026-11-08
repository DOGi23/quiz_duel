package bot

import (
	"context"
	"fmt"
	"github.com/max-messenger/max-bot-api-client-go/v2/model"
)

func (b *Bot) cmdApp(ctx context.Context, update model.Update) {
	var err error
	update, err = b.groupDestination(update)
	if err != nil {
		return
	}
	b.touchFromUpdate(ctx, update)
	if b.botID.Load() == 0 || b.appURL.Load().(string) == "" {
		b.sendReply(ctx, update, "Мини-приложение пока недоступно: не получена ссылка на бота. Попробуйте позже.", nil)
		return
	}
	keyboard := model.NewKeyboard()
	_ = b.SyncChatMembersFromAPI(ctx, update.ChatID)
	b.addGameButton(keyboard, "Открыть Квиз-Дуэль", fmt.Sprintf("chat-%d", update.ChatID))
	b.sendReply(ctx, update, "Открыть мини-приложение для этой группы:", keyboard)
}
