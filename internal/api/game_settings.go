package api

import (
	"net/http"

	"first-max-bot/internal/models"
)

func (s *Server) handleGameSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if chatID, ok := s.queryChat(w, r); ok {
			jsonResponse(w, http.StatusOK, s.storage.GetGameSettings(chatID))
		}
		return
	}
	var req struct {
		ChatID int64 `json:"chat_id"`
		models.GameSettings
	}
	if !decodeBody(w, r, &req) || !s.checkChat(w, r, req.ChatID) || !s.checkTeacher(w, r, req.ChatID) {
		return
	}
	if err := s.storage.SetGameSettings(req.ChatID, req.GameSettings); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, s.storage.GetGameSettings(req.ChatID))
}
