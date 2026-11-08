package api

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"strconv"
	"time"
)

type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *bufferedResponse) Header() http.Header { return w.header }
func (w *bufferedResponse) WriteHeader(n int) {
	if w.status == 0 {
		w.status = n
	}
}
func (w *bufferedResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.body.Write(p)
}
func (s *Server) durable(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b := &bufferedResponse{header: make(http.Header)}
		next(b, r)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := s.storage.Save(ctx); err != nil {
			log.Printf("Сохранение базы: %v", err)
			w.Header().Set("Cache-Control", "no-store")
			apiError(w, 503, "Не удалось сохранить данные. Проверьте состояние игры перед повтором действия.")
			return
		}
		for k, v := range b.header {
			w.Header()[k] = v
		}
		if b.status == 0 {
			b.status = 200
		}
		w.WriteHeader(b.status)
		_, _ = w.Write(b.body.Bytes())
	}
}
func (s *Server) handleDuelDecline(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DuelID string `json:"duel_id"`
		ChatID int64  `json:"chat_id"`
	}
	if !decodeBody(w, r, &req) || !s.checkChat(w, r, req.ChatID) {
		return
	}
	if err := s.storage.DeclineDuel(req.DuelID, req.ChatID, userID(r)); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.queryChat(w, r); ok {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		jsonResponse(w, 200, s.storage.DuelHistory(id, userID(r), offset))
	}
}
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DuelID string `json:"duel_id"`
		QuizID string `json:"quiz_id"`
		Index  int    `json:"index"`
		Reason string `json:"reason"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	id, err := s.storage.ReportQuestion(userID(r), req.DuelID, req.QuizID, req.Index, req.Reason)
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, map[string]string{"id": id})
}
func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if id, ok := s.queryChat(w, r); ok && s.checkTeacher(w, r, id) {
			jsonResponse(w, 200, s.storage.Reports(id))
		}
		return
	}
	var req struct {
		ChatID   int64  `json:"chat_id"`
		ID       string `json:"id"`
		Resolved bool   `json:"resolved"`
	}
	if !decodeBody(w, r, &req) || !s.checkChat(w, r, req.ChatID) || !s.checkTeacher(w, r, req.ChatID) {
		return
	}
	if err := s.storage.ResolveReport(req.ChatID, req.ID, req.Resolved); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
