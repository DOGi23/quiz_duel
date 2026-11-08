package bot

import (
	"context"
	"fmt"
	"github.com/max-messenger/max-bot-api-client-go/v2/model"
	"html"
)

func (b *Bot) chooseDuelChat(ctx context.Context, update model.Update) {
	group, err := b.groupDestination(update)
	if err == nil {
		b.chooseDuelOpponent(ctx, group)
	}
}

func (b *Bot) chooseDuelOpponent(ctx context.Context, update model.Update) {
	id, _, _, _ := getUpdateUser(update)
	if !isGroupUpdate(update) || !b.storage.IsChatMember(update.ChatID, id) {
		b.chooseDuelChat(ctx, update)
		return
	}
	if err := b.SyncChatMembersFromAPI(ctx, update.ChatID); err != nil {
		b.sendReply(ctx, update, "Не удалось обновить участников группы. Попробуйте позже.", nil)
		return
	}
	if !b.storage.IsChatMember(update.ChatID, id) {
		b.sendReply(ctx, update, "Вы больше не состоите в этой группе.", nil)
		return
	}
	members := b.storage.GetChatMembers(update.ChatID)
	keyboard := model.NewKeyboard()
	count := 0
	for _, member := range members {
		if member.ID == id {
			continue
		}
		// Large groups use the full participant picker in the Mini App.
		if count < 20 {
			keyboard.AddRow().AddCallBack(member.FullName(), fmt.Sprintf("cb_challenge_%d", member.ID))
		}
		count++
	}
	if count == 0 {
		b.sendReply(ctx, update, "В группе пока нет доступных соперников.", nil)
		return
	}
	b.addGameButton(keyboard, "Все участники в Арене", fmt.Sprintf("chat-%d", update.ChatID))
	b.sendReply(ctx, update, "Выберите соперника из «"+b.chatTitle(update.ChatID)+"». Приглашение появится в группе, принять его сможет только выбранный игрок.", keyboard)
}

func (b *Bot) challengeOpponent(ctx context.Context, update model.Update, opponentID int64) {
	sender := b.touchFromUpdate(ctx, update)
	response := update
	if err := b.SyncChatMembersFromAPI(ctx, update.ChatID); err != nil {
		b.sendReply(ctx, response, "Не удалось проверить участников группы. Попробуйте позже.", nil)
		return
	}
	duel, err := b.storage.CreateDuel(update.ChatID, sender.ID, opponentID)
	if err != nil {
		b.sendReply(ctx, response, html.EscapeString(err.Error()), nil)
		return
	}
	if err := b.AnnounceDuel(ctx, duel); err != nil {
		_ = b.storage.CancelDuel(duel.ID, sender.ID)
		b.sendReply(ctx, response, "Приглашение не доставлено в группу. Проверьте права бота на отправку сообщений и повторите вызов.", nil)
		return
	}
}
