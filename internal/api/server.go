package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"first-max-bot/internal/bot"
	"first-max-bot/internal/config"
	"first-max-bot/internal/models"
	"first-max-bot/internal/storage"
	"first-max-bot/web"
)

type Server struct {
	cfg     *config.Config
	storage *storage.Storage
	bot     *bot.Bot
}

func NewServer(cfg *config.Config, store *storage.Storage) *Server {
	return &Server{cfg: cfg, storage: store}
}
func (s *Server) SetBot(b *bot.Bot) { s.bot = b }

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/validate", s.durable(s.handleAuthValidate))
	routes := map[string]http.HandlerFunc{
		"GET /api/game-settings":     s.handleGameSettings,
		"PUT /api/game-settings":     s.handleGameSettings,
		"POST /api/duel/decline":     s.handleDuelDecline,
		"GET /api/duel/history":      s.handleHistory,
		"POST /api/question-reports": s.handleReport,
		"GET /api/teacher/reports":   s.handleReports,
		"PATCH /api/teacher/reports": s.handleReports,
		"GET /api/me":                s.handleMe, "GET /api/categories": s.handleCategories,
		"GET /api/chat": s.handleChat, "GET /api/chat/members": s.handleChatMembers,
		"POST /api/duel/create": s.handleDuelCreate, "GET /api/duel/get": s.handleDuelGet,
		"GET /api/duel/list": s.handleDuelList, "POST /api/duel/accept": s.handleDuelAccept,
		"POST /api/duel/cancel": s.handleDuelCancel, "POST /api/duel/answer": s.handleDuelAnswer,
		"POST /api/duel/rematch": s.handleDuelRematch,
		"POST /api/duel/loaded":  s.handleDuelReadiness,
		"POST /api/duel/ready":   s.handleDuelReadiness,
		"POST /api/duel/unready": s.handleDuelReadiness,
		"GET /api/leaderboard":   s.handleLeaderboard, "GET /api/teacher/questions": s.handleTeacherQuestions,
		"POST /api/teacher/questions": s.handleTeacherQuestions, "GET /api/teacher/express-quiz": s.handleExpressQuiz,
		"PATCH /api/teacher/questions":   s.handleQuestionChange,
		"DELETE /api/teacher/questions":  s.handleQuestionChange,
		"POST /api/teacher/express-quiz": s.handleExpressQuiz, "POST /api/teacher/express-quiz/answer": s.handleExpressQuizAnswer,
		"GET /api/teacher/analytics":   s.handleTeacherAnalytics,
		"POST /api/teacher/subjects":   s.handleSubjects,
		"PATCH /api/teacher/subjects":  s.handleSubjects,
		"DELETE /api/teacher/subjects": s.handleSubjects,
	}
	for path, handler := range routes {
		mux.HandleFunc(path, s.authenticated(s.durable(handler)))
	}
	mux.HandleFunc("GET /api/about", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]string{"product": "Квиз-Дуэль", "team_name": "Алгоритм успеха"})
	})
	mux.Handle("GET /", http.FileServer(http.FS(web.Files)))
}

func jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("X-Server-Time", strconv.FormatInt(time.Now().UnixMilli(), 10))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func apiError(w http.ResponseWriter, status int, msg string) {
	jsonResponse(w, status, map[string]string{"error": msg})
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		apiError(w, 400, "Некорректный JSON")
		return false
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		apiError(w, 400, "Ожидается один JSON объект")
		return false
	}
	return true
}

func (s *Server) queryChat(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.URL.Query().Get("chat_id")
	if raw == "" {
		raw = "0"
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		apiError(w, 400, "Некорректный chat_id")
		return 0, false
	}
	return id, s.checkChat(w, r, id)
}

func (s *Server) handleAuthValidate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InitData string `json:"initData"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	var user *models.User
	var chatID int64
	var startParam string
	{
		data, err := validateInitData(req.InitData, s.cfg.BotToken, time.Now())
		if err != nil {
			apiError(w, 401, "Не удалось подтвердить вход. Откройте приложение из MAX.")
			return
		}
		user = s.storage.GetUser(data.User.ID)
		user.FirstName, user.LastName, user.PhotoURL = data.User.FirstName, data.User.LastName, data.User.PhotoURL
		startParam = data.StartParam
		if string(data.Chat.Type) == "CHAT" || string(data.Chat.Type) == "chat" {
			chatID = data.Chat.Id
		}
		// Some MAX clients open apps in a dialog. A signed button payload
		// selects the originating group, but never grants membership in it.
		var selected int64
		if strings.HasPrefix(startParam, "chat-") {
			selected, _ = strconv.ParseInt(strings.TrimPrefix(startParam, "chat-"), 10, 64)
		} else if strings.HasPrefix(startParam, "duel-") {
			if d := s.storage.GetDuel(startParam); d != nil {
				selected = d.ChatID
			}
		} else if strings.HasPrefix(startParam, "quiz-") {
			if q := s.storage.GetExpressQuiz(startParam); q != nil {
				selected = q.ChatID
			}
		}
		if selected != 0 {
			if !s.storage.IsChatMember(selected, user.ID) {
				apiError(w, 403, "Вы не состоите в чате этой игры")
				return
			}
			chatID = selected
		}
	}
	user.Role = "student"
	if s.cfg.IsTeacher(user.ID) {
		user.Role = "teacher"
	}
	s.storage.SaveUser(user)
	if chatID != 0 {
		s.storage.RegisterChatMember(chatID, user)
		if s.bot != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			_ = s.bot.SyncChatMembersFromAPI(ctx, chatID)
			cancel()
		}
	}
	user = s.storage.GetChatUser(chatID, user.ID)
	if s.cfg.IsTeacher(user.ID) || s.bot != nil && s.bot.IsTeacher(r.Context(), chatID, user.ID) {
		user.Role = "teacher"
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, map[string]any{"valid": true, "user": user, "chat_id": chatID, "chat": s.storage.GetChat(chatID), "start_param": startParam})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if chatID, ok := s.queryChat(w, r); ok {
		u := s.storage.GetChatUser(chatID, userID(r))
		if s.isTeacher(r, chatID) {
			u.Role = "teacher"
		}
		jsonResponse(w, 200, u)
	}
}
func (s *Server) handleCategories(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.queryChat(w, r); ok {
		jsonResponse(w, 200, s.storage.GetSubjectCatalog(id))
	}
}
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.queryChat(w, r); ok {
		jsonResponse(w, 200, s.storage.GetChat(id))
	}
}
func (s *Server) handleChatMembers(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.queryChat(w, r); ok {
		members := s.storage.GetChatMembers(id)
		jsonResponse(w, 200, map[string]any{"chat_id": id, "chat": s.storage.GetChat(id), "members": members, "count": len(members)})
	}
}
func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.queryChat(w, r); ok {
		jsonResponse(w, 200, map[string]any{"chat_id": id, "chat": s.storage.GetChat(id), "leaders": s.storage.GetLeaderboard(id), "top_reward": s.storage.GetGameSettings(id).TopReward})
	}
}

// Never publish answers or future questions before both players finish the round.
func duelView(d *models.Duel) *models.Duel {
	for i := range d.Rounds {
		r := &d.Rounds[i]
		if d.Status == "waiting" || r.RoundNumber > d.CurrentRound || r.RoundNumber == d.CurrentRound && (d.Status != "in_progress" && d.Status != "finished" || r.StartsAt.IsZero() || time.Now().Before(r.StartsAt)) {
			r.Question = models.Question{CorrectIndex: -1}
		} else if !r.Player1Answered || !r.Player2Answered {
			r.Question.CorrectIndex = -1
			r.Question.Explanation = ""
			r.Player1Answer = -1
			r.Player2Answer = -1
		}
	}
	return d
}

func (s *Server) handleDuelCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChatID    int64  `json:"chat_id"`
		Player2ID int64  `json:"player2_id"`
		Category  string `json:"category"`
	}
	if !decodeBody(w, r, &req) || !s.checkChat(w, r, req.ChatID) {
		return
	}
	d, err := s.storage.CreateDuel(req.ChatID, userID(r), req.Player2ID, req.Category)
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	warning := s.announceDuel(r, d)
	d, _ = s.storage.GetDuelForUser(d.ID, userID(r))
	jsonResponse(w, 200, struct {
		*models.Duel
		Warning string `json:"notification_warning,omitempty"`
	}{duelView(d), warning})
}

func (s *Server) handleDuelList(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.queryChat(w, r); ok {
		jsonResponse(w, 200, s.storage.ListDuels(id, userID(r)))
	}
}

func (s *Server) handleDuelGet(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	d := s.storage.GetDuel(id)
	if d == nil {
		apiError(w, 404, "Дуэль не найдена")
		return
	}
	if !s.checkChat(w, r, d.ChatID) {
		return
	}
	d, err := s.storage.GetDuelForUser(id, userID(r))
	if err != nil {
		apiError(w, 403, err.Error())
		return
	}
	jsonResponse(w, 200, duelView(d))
}

func (s *Server) handleDuelAccept(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DuelID string `json:"duel_id"`
		ChatID int64  `json:"chat_id"`
	}
	if !decodeBody(w, r, &req) || !s.checkChat(w, r, req.ChatID) {
		return
	}
	d, err := s.storage.AcceptDuel(req.DuelID, req.ChatID, userID(r))
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	d, _ = s.storage.GetDuelForUser(d.ID, userID(r))
	jsonResponse(w, 200, duelView(d))
}

func (s *Server) handleDuelCancel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DuelID string `json:"duel_id"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if err := s.storage.CancelDuel(req.DuelID, userID(r)); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleDuelAnswer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DuelID    string  `json:"duel_id"`
		RoundNum  int     `json:"round_num"`
		OptionIdx *int    `json:"option_idx"`
		TimeSec   float64 `json:"time_sec"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	d := s.storage.GetDuel(req.DuelID)
	if d == nil || req.OptionIdx == nil {
		apiError(w, 400, "Укажите дуэль и ответ")
		return
	}
	if !s.checkChat(w, r, d.ChatID) {
		return
	}
	d, err := s.storage.SubmitDuelAnswer(req.DuelID, userID(r), req.RoundNum, *req.OptionIdx, req.TimeSec)
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, duelView(d))
}

func (s *Server) handleTeacherQuestions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if id, ok := s.queryChat(w, r); ok && s.checkTeacher(w, r, id) {
			jsonResponse(w, 200, s.storage.GetQuestionsByCategory(id, r.URL.Query().Get("category")))
		}
		return
	}
	var req struct {
		ChatID      int64    `json:"chat_id"`
		Category    string   `json:"category"`
		Topic       string   `json:"topic"`
		Text        string   `json:"text"`
		Options     []string `json:"options"`
		CorrectIdx  *int     `json:"correct_idx"`
		Explanation string   `json:"explanation"`
	}
	if !decodeBody(w, r, &req) || !s.checkChat(w, r, req.ChatID) {
		return
	}
	if !s.checkTeacher(w, r, req.ChatID) {
		return
	}
	if req.CorrectIdx == nil {
		apiError(w, 400, "Укажите верный ответ")
		return
	}
	q, err := s.storage.AddQuestion(models.Question{ChatID: req.ChatID, Category: req.Category, Topic: req.Topic, Text: req.Text, Options: req.Options, CorrectIndex: *req.CorrectIdx, Explanation: req.Explanation, TeacherName: s.storage.GetUser(userID(r)).FullName(), Difficulty: "medium"})
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 201, map[string]any{"status": "created", "question": q})
}

func quizView(q *models.ExpressQuiz, uid int64, teacher bool) *models.ExpressQuiz {
	p := q.Participants[uid]
	current := -1
	if p != nil {
		current = p.TotalAnswers
	}
	for i := range q.Questions {
		if i < current { // Already answered by this user; safe to review/report.
			continue
		} else if i != current {
			q.Questions[i] = models.Question{CorrectIndex: -1}
		} else {
			q.Questions[i].CorrectIndex = -1
			q.Questions[i].Explanation = ""
		}
	}
	if !teacher {
		q.Participants = map[int64]*models.QuizParticipant{}
		if p != nil {
			q.Participants[uid] = p
		}
	}
	return q
}

func (s *Server) handleExpressQuiz(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			ChatID  int64  `json:"chat_id"`
			Subject string `json:"subject"`
			Topic   string `json:"topic"`
		}
		if !decodeBody(w, r, &req) || !s.checkChat(w, r, req.ChatID) {
			return
		}
		if !s.checkTeacher(w, r, req.ChatID) {
			return
		}
		q, err := s.storage.StartExpressQuiz(req.ChatID, userID(r), s.storage.GetUser(userID(r)).FullName(), req.Subject, req.Topic)
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		jsonResponse(w, 201, quizView(q, userID(r), true))
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		chatID, ok := s.queryChat(w, r)
		if !ok {
			return
		}
		q := s.storage.ActiveQuiz(chatID)
		if q == nil {
			apiError(w, 404, "В чате пока нет активного квиза")
			return
		}
		id = q.ID
	}
	q, err := s.storage.GetQuizForUser(id, userID(r))
	if err != nil {
		apiError(w, 404, err.Error())
		return
	}
	jsonResponse(w, 200, quizView(q, userID(r), s.isTeacher(r, q.ChatID)))
}

func (s *Server) handleExpressQuizAnswer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QuizID   string  `json:"quiz_id"`
		QIndex   *int    `json:"q_index"`
		OptIndex *int    `json:"opt_index"`
		TimeSec  float64 `json:"time_sec"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.QIndex == nil || req.OptIndex == nil {
		apiError(w, 400, "Укажите вопрос и ответ")
		return
	}
	p, err := s.storage.SubmitExpressQuizAnswer(req.QuizID, userID(r), "", *req.QIndex, *req.OptIndex, req.TimeSec)
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, p)
}

func (s *Server) handleTeacherAnalytics(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.queryChat(w, r); ok && s.checkTeacher(w, r, id) {
		jsonResponse(w, 200, s.storage.GetTopicAnalytics(id))
	}
}
