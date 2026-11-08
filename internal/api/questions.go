package api

import (
	"first-max-bot/internal/models"
	"net/http"
)

func (s *Server) handleQuestionChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChatID      int64    `json:"chat_id"`
		ID          string   `json:"id"`
		Category    string   `json:"category"`
		Topic       string   `json:"topic"`
		Text        string   `json:"text"`
		Options     []string `json:"options"`
		Correct     *int     `json:"correct_idx"`
		Explanation string   `json:"explanation"`
		Disabled    *bool    `json:"disabled"`
	}
	if !decodeBody(w, r, &req) || !s.checkChat(w, r, req.ChatID) || !s.checkTeacher(w, r, req.ChatID) {
		return
	}
	remove := r.Method == http.MethodDelete
	var q *models.Question
	if !remove && req.Disabled == nil {
		if req.Correct == nil {
			apiError(w, 400, "Укажите правильный ответ")
			return
		}
		q = &models.Question{Category: req.Category, Topic: req.Topic, Text: req.Text, Options: req.Options, CorrectIndex: *req.Correct, Explanation: req.Explanation, TeacherName: s.storage.GetUser(userID(r)).FullName()}
	}
	changed, err := s.storage.ChangeQuestion(req.ChatID, req.ID, q, req.Disabled, remove)
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, changed)
}
