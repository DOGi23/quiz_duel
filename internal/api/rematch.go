package api

import (
	"context"
	"first-max-bot/internal/models"
	"net/http"
	"time"
)

func (s *Server) announceDuel(r *http.Request, d *models.Duel) string {
	if s.bot == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	if err := s.bot.AnnounceDuel(ctx, d); err != nil {
		return "Дуэль создана, но приглашение не доставлено в группу. Проверьте права бота на отправку сообщений. Вызов доступен в Арене."
	}
	return ""
}

func (s *Server) handleDuelRematch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DuelID string `json:"duel_id"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	parent := s.storage.GetDuel(req.DuelID)
	if parent == nil {
		apiError(w, 404, "Дуэль не найдена")
		return
	}
	if !s.checkChat(w, r, parent.ChatID) {
		return
	}
	d, created, err := s.storage.Rematch(req.DuelID, userID(r))
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	warning := ""
	if created {
		warning = s.announceDuel(r, d)
	}
	d, _ = s.storage.GetDuelForUser(d.ID, userID(r))
	jsonResponse(w, 200, struct {
		*models.Duel
		Warning string `json:"notification_warning,omitempty"`
	}{duelView(d), warning})
}
