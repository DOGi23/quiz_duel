package api

import (
	"first-max-bot/internal/models"
	"first-max-bot/internal/storage"
	"net/http"
)

func (s *Server) handleSubjects(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChatID           int64   `json:"chat_id"`
		ID               string  `json:"id"`
		Name             string  `json:"name"`
		Description      string  `json:"description"`
		ImageURL         *string `json:"image_url"`
		MinQuestionCount *int    `json:"min_question_count"`
	}
	if !decodeBody(w, r, &req) || !s.checkChat(w, r, req.ChatID) || !s.checkTeacher(w, r, req.ChatID) {
		return
	}
	if r.Method == http.MethodDelete {
		if err := s.storage.DeleteCategory(req.ChatID, req.ID); err != nil {
			apiError(w, 404, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]bool{"deleted": true})
		return
	}
	if r.Method == http.MethodPost {
		req.ID = ""
	} else if req.ID == "" {
		apiError(w, 400, "Укажите предмет")
		return
	}
	var imageUpdate []string
	if req.ImageURL != nil {
		imageUpdate = append(imageUpdate, *req.ImageURL)
	}
	minimum := 0
	if req.MinQuestionCount != nil {
		minimum = *req.MinQuestionCount
		if minimum < 1 || minimum > storage.MaxQuestionCount {
			apiError(w, 400, "Минимум вопросов должен быть целым числом от 1 до 20")
			return
		}
	}
	category, err := s.storage.SaveCategory(req.ChatID, models.Category{ID: req.ID, Name: req.Name, Description: req.Description, MinQuestionCount: minimum}, imageUpdate...)
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, category)
}
