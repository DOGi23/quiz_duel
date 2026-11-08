package bot

import (
	"context"
	"first-max-bot/internal/models"
	"fmt"
	"github.com/max-messenger/max-bot-api-client-go/v2/model"
	"html"
)

// API-created searches must be visible to people who have not opened the Arena.
func (b *Bot) AnnounceDuel(ctx context.Context, d *models.Duel) error {
	keyboard := model.NewKeyboard()
	b.addGameButton(keyboard, "Открыть дуэль", d.ID)
	keyboard.AddRow().AddCallBack("Принять вызов", fmt.Sprintf("cb_accept_%s_chat_%d", d.ID, d.ChatID))
	destination := model.Update{ChatID: d.ChatID}
	text := fmt.Sprintf("<b>%s ищет соперника!</b>\nВопросов: %d, по 15 секунд. Кто готов сыграть?", html.EscapeString(d.Player1.FullName()), len(d.Rounds))
	if d.Player2 != nil {
		keyboard.AddRow().AddCallBack("Отклонить", fmt.Sprintf("cb_decline_%s_chat_%d", d.ID, d.ChatID))
		text = fmt.Sprintf("<b>%s вызывает %s на дуэль!</b>\nПринять или отклонить вызов может только %s.\nВопросов: %d, по 15 секунд. Приглашение действует 5 минут.", html.EscapeString(d.Player1.FullName()), html.EscapeString(d.Player2.FullName()), html.EscapeString(d.Player2.FullName()), len(d.Rounds))
	}
	if d.RematchOf != "" && d.Player2 != nil {
		text = fmt.Sprintf("<b>%s предлагает реванш игроку %s!</b>\nПринять или отклонить вызов может только %s.\nКнопка «Реванш» на экране результата ведёт в эту же игру.", html.EscapeString(d.Player1.FullName()), html.EscapeString(d.Player2.FullName()), html.EscapeString(d.Player2.FullName()))
	}
	return b.sendMessage(ctx, destination, text, keyboard)
}

// Closure notices go to the originating group and are retried on delivery failure. MAX has no
// idempotency key, so a crash after delivery can repeat a notification.
func (b *Bot) NotifyClosures(ctx context.Context) {
	if b.API() == nil {
		return
	}
	for _, d := range b.storage.PendingClosures() {
		reason := "Дуэль отменена."
		if d.CloseReason == "rejected" {
			reason = "Соперник отклонил приглашение в дуэль."
		}
		if d.CloseReason == "expired" {
			reason = "Время приглашения в дуэль истекло (5 минут). Можно создать новый вызов."
		}
		if d.CloseReason == "timeout" {
			reason = "Дуэль закрыта из-за длительного бездействия."
		}
		reason += "\nВызов: " + html.EscapeString(d.Player1.FullName())
		if d.Player2 != nil {
			reason += " — " + html.EscapeString(d.Player2.FullName())
		}
		if err := b.sendMessage(ctx, model.Update{ChatID: d.ChatID}, reason, nil); err == nil {
			b.storage.MarkClosureNotified(d.ID)
		}
		if ctx.Err() != nil {
			return
		}
	}
}
