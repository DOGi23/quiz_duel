package bot

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/max-messenger/max-bot-api-client-go/v2/model"
)

func isGroupUpdate(update model.Update) bool {
	if update.Message != nil && update.Message.Recipient.ChatType != "" {
		return strings.EqualFold(string(update.Message.Recipient.ChatType), "chat")
	}
	return update.ChatID < 0
}

func groupContext(update model.Update) int64 {
	if isGroupUpdate(update) {
		return update.ChatID
	}
	return 0
}

// Keep the originating group in menu callbacks. A callback never grants
// membership: handleCallback checks it again before restoring this context.
func contextKeyboard(keyboard *model.Keyboard, chatID int64) *model.Keyboard {
	if keyboard == nil {
		return nil
	}
	result := model.NewKeyboard()
	for _, row := range keyboard.Build().Payload.Buttons {
		target := result.AddRow()
		for _, button := range row {
			switch button.Type {
			case model.ButtonCallback:
				payload := button.Payload
				if !strings.Contains(payload, "_chat_") {
					payload = fmt.Sprintf("%s_chat_%d", payload, chatID)
				}
				target.AddCallBack(button.Text, payload)
			case model.ButtonOpenApp:
				target.AddOpenApp(button.Text, button.ContactID)
				copied := target.Build()[len(target.Build())-1]
				copied.WebApp = button.WebApp
				copied.Payload = button.Payload
			case model.ButtonLink:
				target.AddLink(button.Text, button.URL)
			}
		}
	}
	return result
}

// A dialog can resolve to a group only when the user belongs to exactly one
// known group. Never guess between groups or broadcast a personal command.
func (b *Bot) groupDestination(update model.Update) (model.Update, error) {
	group := isGroupUpdate(update)
	if update.Message == nil && b.storage.GetChat(update.ChatID) != nil {
		group = true
	}
	if !group {
		id, _, _, _ := getUpdateUser(update)
		chats := b.storage.UserChats(id)
		if len(chats) != 1 {
			return update, fmt.Errorf("отправьте команду в нужной группе: общий чат не определён однозначно")
		}
		update.ChatID = chats[0].ID
	}
	if update.ChatID == 0 {
		return update, fmt.Errorf("общий чат не указан")
	}
	message := model.MessageUpdate{}
	if update.Message != nil {
		message = *update.Message
	}
	message.Recipient.ChatType = model.ChatTypeChat
	update.Message = &message
	return update, nil
}

func (b *Bot) sendReply(ctx context.Context, update model.Update, text string, keyboard *model.Keyboard) {
	update, err := b.groupDestination(update)
	if err != nil {
		log.Printf("Ответ бота пропущен: %v", err)
		return
	}
	_ = b.sendMessage(ctx, update, text, contextKeyboard(keyboard, update.ChatID))
}
