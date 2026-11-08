package api

import "net/http"

func (s *Server) handleDuelReadiness(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DuelID   string `json:"duel_id"`
		RoundNum int    `json:"round_num"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	d := s.storage.GetDuel(req.DuelID)
	if d == nil {
		apiError(w, 404, "Дуэль не найдена")
		return
	}
	if !s.checkChat(w, r, d.ChatID) {
		return
	}
	var err error
	if r.URL.Path == "/api/duel/unready" {
		d, err = s.storage.ResetDuelReadiness(req.DuelID, userID(r), req.RoundNum)
	} else {
		d, err = s.storage.SetDuelReadiness(req.DuelID, userID(r), req.RoundNum, r.URL.Path == "/api/duel/ready")
	}
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, duelView(d))
}
