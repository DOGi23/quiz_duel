package bot

import (
	"context"
	"github.com/max-messenger/max-bot-api-client-go/v2/model"
)

func (b *Bot) registerCommands(ctx context.Context) error {
	_, err := b.API().Bots.PatchCommands(ctx, model.BotPatchCommands{Commands: []model.BotCommand{
		{Name: "start", Description: "Главное меню"},
		{Name: "app", Description: "Открыть мини-приложение Квиз-Дуэль"},
		{Name: "duel", Description: "Выбрать группу и вызвать соперника"},
		{Name: "top", Description: "Рейтинг группы"},
		{Name: "members", Description: "Участники группы"},
		{Name: "stats", Description: "Моя статистика"},
		{Name: "teacher", Description: "Кабинет учителя"},
		{Name: "quiz", Description: "Запустить квиз (для учителя)"},
		{Name: "help", Description: "Правила игры"},
	}})
	return err
}
