package storage

import (
	cryptorand "crypto/rand"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"

	"first-max-bot/internal/models"
)

const answerLimit = 15 * time.Second

func shuffledQuestion(q models.Question) models.Question {
	q = cloneQuestion(q)
	rand.Shuffle(len(q.Options), func(i, j int) {
		q.Options[i], q.Options[j] = q.Options[j], q.Options[i]
		if q.CorrectIndex == i {
			q.CorrectIndex = j
		} else if q.CorrectIndex == j {
			q.CorrectIndex = i
		}
	})
	return q
}

func (s *Storage) CreateDuel(chatID, player1ID, player2ID int64, category ...string) (*models.Duel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createDuelLocked(chatID, player1ID, player2ID, category...)
}

func (s *Storage) createDuelLocked(chatID, player1ID, player2ID int64, category ...string) (*models.Duel, error) {
	s.expireInvitationsLocked()
	if !s.isMemberLocked(chatID, player1ID) || (player2ID != 0 && !s.isMemberLocked(chatID, player2ID)) {
		return nil, fmt.Errorf("дуэль доступна только участникам этого чата")
	}
	if player1ID == player2ID {
		return nil, fmt.Errorf("нельзя вызвать самого себя")
	}
	cat := ""
	if len(category) > 0 {
		cat = category[0]
	}
	ready := make(map[string]bool)
	for _, subject := range s.subjectCatalogLocked(chatID) {
		ready[subject.ID] = subject.Ready
		if cat == subject.ID && !subject.Ready {
			return nil, fmt.Errorf("предмет «%s» пока не готов: включённых вопросов %d из %d", subject.Name, subject.QuestionCount, subject.MinQuestionCount)
		}
	}
	questions := []models.Question{}
	for _, q := range s.questionsLocked(chatID, false) {
		if ready[q.Category] && (cat == "" || q.Category == cat) {
			questions = append(questions, q)
		}
	}
	if len(questions) == 0 {
		return nil, fmt.Errorf("нет вопросов по этому предмету")
	}
	// Do not let repeated clicks create unlimited pending invitations.
	for _, d := range s.duels {
		if time.Since(d.CreatedAt) < time.Hour && d.ChatID == chatID && d.Status != "finished" && d.Status != "declined" && (d.Player1.ID == player1ID || (d.Player2 != nil && d.Player2.ID == player1ID)) {
			return nil, fmt.Errorf("сначала завершите текущую дуэль")
		}
	}
	rand.Shuffle(len(questions), func(i, j int) { questions[i], questions[j] = questions[j], questions[i] })
	d := &models.Duel{ID: "duel-" + cryptorand.Text(), ChatID: chatID, Player1: s.chatUserLocked(chatID, player1ID), Rounds: make([]models.Round, s.gameSettingsLocked(chatID).DuelQuestionCount), CurrentRound: 1, Status: "waiting", CreatedAt: time.Now(), IsRewarded: true, RewardNotice: "До 3 дуэлей с XP с одним соперником в сутки в этом чате."}
	if player2ID != 0 {
		d.Player2 = s.chatUserLocked(chatID, player2ID)
	}
	for i := range d.Rounds {
		q := shuffledQuestion(questions[i%len(questions)])
		d.Rounds[i] = models.Round{RoundNumber: i + 1, Category: q.Category, CategoryPickerID: player1ID, Question: q, Player1Answer: -1, Player2Answer: -1}
	}
	s.duels[d.ID] = d
	d.ExpiresAt = d.CreatedAt.Add(5 * time.Minute)
	return cloneDuel(d), nil
}

func (s *Storage) AcceptDuel(id string, chatID, userID int64) (*models.Duel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acceptDuelLocked(id, chatID, userID)
}

func (s *Storage) acceptDuelLocked(id string, chatID, userID int64) (*models.Duel, error) {
	s.expireInvitationsLocked()
	d := s.duels[id]
	if d == nil || d.ChatID != chatID || !s.isMemberLocked(chatID, userID) {
		return nil, fmt.Errorf("вызов не найден в этом чате")
	}
	if (d.Status == "preparing" || d.Status == "countdown" || d.Status == "in_progress") && time.Since(d.CreatedAt) < time.Hour && d.Player2 != nil && d.Player2.ID == userID {
		return cloneDuel(d), nil
	}
	if d.Status != "waiting" || time.Since(d.CreatedAt) >= time.Hour {
		return nil, fmt.Errorf("вызов уже принят или устарел")
	}
	if d.Player1.ID == userID || (d.Player2 != nil && d.Player2.ID != userID) {
		return nil, fmt.Errorf("этот вызов адресован другому участнику")
	}
	for _, other := range s.duels {
		if other.ID == id || other.ChatID != chatID || time.Since(other.CreatedAt) >= time.Hour {
			continue
		}
		active := other.Status == "preparing" || other.Status == "countdown" || other.Status == "in_progress"
		if active && (other.Player1.ID == userID || other.Player2 != nil && other.Player2.ID == userID) || other.Status == "waiting" && other.Player1.ID == userID {
			return nil, fmt.Errorf("сначала завершите текущую дуэль")
		}
	}
	d.Player2 = s.chatUserLocked(chatID, userID)
	d.Status = "preparing"
	return cloneDuel(d), nil
}

func (s *Storage) CancelDuel(id string, userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.duels[id]
	if d == nil || (d.Player1.ID != userID && (d.Player2 == nil || d.Player2.ID != userID)) {
		return fmt.Errorf("дуэль не найдена")
	}
	if d.Status == "finished" {
		return fmt.Errorf("дуэль уже завершена")
	}
	if d.Status != "declined" {
		closeDuel(d, "cancelled")
	}
	return nil
}

func (s *Storage) GetDuel(id string) *models.Duel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneDuel(s.duels[id])
}

func (s *Storage) ListDuels(chatID, userID int64) []*models.Duel {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireInvitationsLocked()
	result := []*models.Duel{}
	for _, d := range s.duels {
		if d.Status == "waiting" && dismissed(d, userID) {
			continue
		}
		if d.ChatID != chatID || !s.isMemberLocked(chatID, userID) || time.Since(d.CreatedAt) >= time.Hour {
			continue
		}
		if d.Status == "waiting" && (d.Player2 == nil || d.Player2.ID == userID || d.Player1.ID == userID) || (d.Status == "preparing" || d.Status == "countdown" || d.Status == "in_progress") && (d.Player1.ID == userID || d.Player2.ID == userID) {
			cp := cloneDuel(d)
			cp.Rounds = []models.Round{}
			result = append(result, cp)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result
}

// Reading a room never starts a personal clock. Both clocks use the common
// server timestamp chosen only after both clients have loaded and confirmed.
func (s *Storage) GetDuelForUser(id string, userID int64) (*models.Duel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireInvitationsLocked()
	d := s.duels[id]
	if d == nil || (d.Player1.ID != userID && (d.Player2 == nil || d.Player2.ID != userID)) {
		return nil, fmt.Errorf("дуэль не найдена")
	}
	if time.Since(d.CreatedAt) >= time.Hour && d.Status != "finished" && d.Status != "declined" {
		closeDuel(d, "timeout")
	}
	s.startCountdownLocked(d)
	if d.Status == "in_progress" {
		s.expireRoundLocked(d)
	}
	return cloneDuel(d), nil
}

func (s *Storage) expireRoundLocked(d *models.Duel) {
	r := &d.Rounds[d.CurrentRound-1]
	if !r.Player1Answered && !r.Player1StartedAt.IsZero() && time.Since(r.Player1StartedAt) >= answerLimit {
		s.setAnswerLocked(d, r, true, -1, 15)
	}
	if !r.Player2Answered && !r.Player2StartedAt.IsZero() && time.Since(r.Player2StartedAt) >= answerLimit {
		s.setAnswerLocked(d, r, false, -1, 15)
	}
	s.advanceLocked(d)
}

func validAnswer(q models.Question, opt int, sec float64) bool {
	return opt >= -1 && opt < len(q.Options) && !math.IsNaN(sec) && !math.IsInf(sec, 0) && sec >= 0 && sec <= 15
}

func answerScore(q models.Question, opt int, sec float64) int {
	if opt != q.CorrectIndex || sec >= 15 {
		return 0
	}
	mult := q.Multiplier
	if mult <= 0 {
		mult = 1
	}
	return int(float64(50+int(math.Max(0, (15-sec)*3))) * mult)
}

func (s *Storage) setAnswerLocked(d *models.Duel, r *models.Round, first bool, opt int, sec float64) {
	points := answerScore(r.Question, opt, sec)
	if first {
		r.Player1Answered, r.Player1Answer, r.Player1TimeSec, r.Player1Score = true, opt, sec, points
	} else {
		r.Player2Answered, r.Player2Answer, r.Player2TimeSec, r.Player2Score = true, opt, sec, points
	}
	s.recordAnswerLocked(d.ChatID, r.Question, opt == r.Question.CorrectIndex && sec < 15)
}

func (s *Storage) SubmitDuelAnswer(id string, userID int64, roundNum, opt int, clientSec float64) (*models.Duel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.duels[id]
	if d == nil || d.Player2 == nil || (d.Player1.ID != userID && d.Player2.ID != userID) {
		return nil, fmt.Errorf("игрок не участвует в дуэли")
	}
	if roundNum < 1 || roundNum > len(d.Rounds) {
		return nil, fmt.Errorf("неверный раунд")
	}
	r := &d.Rounds[roundNum-1]
	if !validAnswer(r.Question, opt, clientSec) {
		return nil, fmt.Errorf("неверный ответ или время")
	}
	first := userID == d.Player1.ID
	s.startCountdownLocked(d)
	// Safe retry after a lost response: do not award or count an answer twice.
	if first && r.Player1Answered || !first && r.Player2Answered {
		return cloneDuel(d), nil
	}
	if d.Status != "in_progress" || roundNum != d.CurrentRound || time.Since(d.CreatedAt) >= time.Hour {
		return nil, fmt.Errorf("этот раунд недоступен")
	}
	started := r.Player1StartedAt
	if !first {
		started = r.Player2StartedAt
	}
	if started.IsZero() || time.Now().Before(started) {
		return nil, fmt.Errorf("дождитесь готовности обоих игроков и окончания отсчёта")
	}
	sec := math.Max(0, time.Since(started).Seconds())
	if sec >= 15 {
		opt, sec = -1, 15
	}
	s.setAnswerLocked(d, r, first, opt, sec)
	s.advanceLocked(d)
	return cloneDuel(d), nil
}

func (s *Storage) advanceLocked(d *models.Duel) {
	r := &d.Rounds[d.CurrentRound-1]
	if !r.Player1Answered || !r.Player2Answered {
		return
	}
	if d.CurrentRound < len(d.Rounds) {
		d.CurrentRound++
		d.Status = "preparing"
		return
	}
	s.finishDuel(d)
}

func (s *Storage) finishDuel(d *models.Duel) {
	if d.Status == "finished" {
		return
	}
	d.Status, d.FinishedAt = "finished", time.Now()
	p1, p2 := s.progressLocked(d.ChatID, d.Player1.ID), s.progressLocked(d.ChatID, d.Player2.ID)
	d.IsRewarded = p1.DailyDuelsWith[d.Player2.ID] < 3 && p2.DailyDuelsWith[d.Player1.ID] < 3
	p1.DailyDuelsWith[d.Player2.ID]++
	p2.DailyDuelsWith[d.Player1.ID]++
	a, b := 0, 0
	for _, r := range d.Rounds {
		a += r.Player1Score
		b += r.Player2Score
	}
	bonus := int(math.Max(-15, math.Min(35, float64(p2.Level-p1.Level)*5)))
	reverseBonus := int(math.Max(-15, math.Min(35, float64(p1.Level-p2.Level)*5)))
	x1, x2 := 25, 25
	if a > b {
		d.WinnerID = d.Player1.ID
		p1.Wins++
		p2.Losses++
		x1, x2 = 50+bonus, 10
	} else if b > a {
		d.WinnerID = d.Player2.ID
		p2.Wins++
		p1.Losses++
		x1, x2 = 10, 50+reverseBonus
	} else {
		p1.Draws++
		p2.Draws++
	}
	if d.IsRewarded {
		d.Player1XPDelta, d.Player2XPDelta = x1, x2
		p1.XP += x1
		p2.XP += x2
	} else {
		d.RewardNotice = "Лимит 3 дуэли с XP в сутки с этим соперником в этом чате исчерпан."
	}
	p1.Level, p2.Level = p1.XP/200+1, p2.XP/200+1
	d.Player1, d.Player2 = s.chatUserLocked(d.ChatID, d.Player1.ID), s.chatUserLocked(d.ChatID, d.Player2.ID)
}

func (s *Storage) StartExpressQuiz(chatID, teacherID int64, teacherName, subject, topic string) (*models.ExpressQuiz, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isMemberLocked(chatID, teacherID) {
		return nil, fmt.Errorf("учитель не состоит в чате")
	}
	for _, category := range s.subjectCatalogLocked(chatID) {
		if category.ID == subject && !category.Ready {
			return nil, fmt.Errorf("предмет «%s» пока не готов: включённых вопросов %d из %d", category.Name, category.QuestionCount, category.MinQuestionCount)
		}
	}
	questions := []models.Question{}
	for _, q := range s.questionsLocked(chatID, false) {
		if s.categoryExistsLocked(chatID, q.Category) && q.Category == subject {
			questions = append(questions, q)
		}
	}
	if len(questions) == 0 {
		return nil, fmt.Errorf("нет вопросов по этому предмету")
	}
	rand.Shuffle(len(questions), func(i, j int) { questions[i], questions[j] = questions[j], questions[i] })
	questions = questions[:min(s.gameSettingsLocked(chatID).QuizQuestionCount, len(questions))]
	for i := range questions {
		questions[i] = shuffledQuestion(questions[i])
		questions[i].Multiplier = 2
	}
	for _, q := range s.expressQuiz {
		if q.ChatID == chatID {
			q.IsActive = false
		}
	}
	q := &models.ExpressQuiz{ID: "quiz-" + cryptorand.Text(), ChatID: chatID, TeacherID: teacherID, TeacherName: teacherName, Subject: subject, Topic: topic, Questions: questions, TimeLimitSec: 15, IsActive: true, Participants: make(map[int64]*models.QuizParticipant), CreatedAt: time.Now()}
	s.expressQuiz[q.ID] = q
	return cloneQuiz(q), nil
}

func (s *Storage) GetExpressQuiz(id string) *models.ExpressQuiz {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneQuiz(s.expressQuiz[id])
}

func (s *Storage) ActiveQuiz(chatID int64) *models.ExpressQuiz {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, q := range s.expressQuiz {
		if q.ChatID == chatID && q.IsActive && time.Since(q.CreatedAt) < time.Hour {
			return cloneQuiz(q)
		}
	}
	return nil
}

func (s *Storage) GetQuizForUser(id string, userID int64) (*models.ExpressQuiz, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := s.expressQuiz[id]
	if q == nil || !q.IsActive || time.Since(q.CreatedAt) >= time.Hour || !s.isMemberLocked(q.ChatID, userID) {
		return nil, fmt.Errorf("активный квиз не найден в этом чате")
	}
	p := q.Participants[userID]
	if p == nil {
		p = &models.QuizParticipant{UserID: userID, Name: s.getUserLocked(userID).FullName()}
		q.Participants[userID] = p
	}
	if p.TotalAnswers < len(q.Questions) && p.QuestionStartedAt.IsZero() {
		p.QuestionStartedAt = time.Now()
	}
	return cloneQuiz(q), nil
}

func (s *Storage) SubmitExpressQuizAnswer(id string, userID int64, name string, qIndex, opt int, clientSec float64) (*models.QuizParticipant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := s.expressQuiz[id]
	if q == nil || !q.IsActive || time.Since(q.CreatedAt) >= time.Hour || !s.isMemberLocked(q.ChatID, userID) {
		return nil, fmt.Errorf("активный квиз не найден")
	}
	if qIndex < 0 || qIndex >= len(q.Questions) || !validAnswer(q.Questions[qIndex], opt, clientSec) {
		return nil, fmt.Errorf("неверный вопрос, ответ или время")
	}
	p := q.Participants[userID]
	if p == nil {
		return nil, fmt.Errorf("сначала откройте квиз")
	}
	if qIndex < p.TotalAnswers {
		cp := *p
		return &cp, nil
	}
	if qIndex != p.TotalAnswers || p.QuestionStartedAt.IsZero() {
		return nil, fmt.Errorf("сначала откройте текущий вопрос")
	}
	sec := math.Max(0, time.Since(p.QuestionStartedAt).Seconds())
	if sec >= 15 {
		sec, opt = 15, -1
	}
	question := q.Questions[qIndex]
	correct := opt == question.CorrectIndex && sec < 15
	s.recordAnswerLocked(q.ChatID, question, correct)
	p.TotalAnswers++
	p.TotalTimeSec += sec
	p.QuestionStartedAt = time.Time{}
	if correct {
		p.CorrectCount++
		score := answerScore(question, opt, sec)
		p.Score += score
		u := s.progressLocked(q.ChatID, userID)
		u.XP += score
		u.Level = u.XP/200 + 1
	}
	cp := *p
	return &cp, nil
}
