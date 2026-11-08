package bot

import (
	"context"
	"log"
	"time"
)

// IsTeacher checks the current MAX permissions, never a client-supplied role.
// An administrator of one group receives no privileges in another group.
func (b *Bot) IsTeacher(ctx context.Context, chatID, userID int64) bool {
	if userID <= 0 {
		return false
	}
	if b.cfg.IsTeacher(userID) {
		return true
	}
	if chatID == 0 || b.API() == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	admins, err := b.API().Chats.GetAdmins(ctx, chatID)
	if err != nil {
		log.Printf("Проверка прав учителя в чате %d: %v", chatID, err)
		return false
	}
	for _, member := range admins.Members {
		if member.UserID == userID && !member.IsBot && (member.IsAdmin || member.IsOwner) {
			return true
		}
	}
	return false
}
