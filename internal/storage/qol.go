package storage

import (
	cryptorand "crypto/rand"
	"first-max-bot/internal/models"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

func closeDuel(d *models.Duel, reason string) {
	d.Status = "declined"
	d.CloseReason = reason
	d.ClosedAt = time.Now()
}
func dismissed(d *models.Duel, id int64) bool { return slices.Contains(d.DismissedBy, id) }
func (s *Storage) expireInvitationsLocked() {
	for _, d := range s.duels {
		if d.Status == "waiting" && time.Since(d.CreatedAt) >= 5*time.Minute {
			closeDuel(d, "expired")
		}
	}
}
func (s *Storage) ExpireInvitations() { s.mu.Lock(); defer s.mu.Unlock(); s.expireInvitationsLocked() }
func (s *Storage) PendingClosures() []*models.Duel {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireInvitationsLocked()
	out := []*models.Duel{}
	for _, d := range s.duels {
		if d.Status == "declined" && d.CloseReason != "" && !d.CloseNotified {
			out = append(out, cloneDuel(d))
		}
	}
	return out
}
func (s *Storage) MarkClosureNotified(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d := s.duels[id]; d != nil {
		d.CloseNotified = true
	}
}
func (s *Storage) DeclineDuel(id string, chatID, uid int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireInvitationsLocked()
	d := s.duels[id]
	if d == nil || d.ChatID != chatID || !s.isMemberLocked(chatID, uid) || d.Player1.ID == uid || d.Player2 != nil && d.Player2.ID != uid {
		return fmt.Errorf("вызов недоступен")
	}
	if d.Status == "declined" && d.CloseReason == "rejected" {
		return nil
	}
	if d.Status != "waiting" {
		return fmt.Errorf("вызов уже принят или устарел")
	}
	if d.Player2 == nil {
		if !dismissed(d, uid) {
			d.DismissedBy = append(d.DismissedBy, uid)
		}
	} else {
		closeDuel(d, "rejected")
	}
	return nil
}
func (s *Storage) DuelHistory(chatID, uid int64, offset int) []*models.Duel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*models.Duel{}
	if !s.isMemberLocked(chatID, uid) {
		return out
	}
	for _, d := range s.duels {
		if d.ChatID == chatID && (d.Player1.ID == uid || d.Player2 != nil && d.Player2.ID == uid) && (d.Status == "finished" || d.Status == "declined") {
			cp := cloneDuel(d)
			cp.Rounds = nil
			out = append(out, cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	offset = max(0, min(offset, len(out)))
	return out[offset:min(offset+20, len(out))]
}

type QuestionReport struct {
	ID        string          `json:"id"`
	ChatID    int64           `json:"chat_id"`
	UserID    int64           `json:"user_id"`
	UserName  string          `json:"user_name"`
	Question  models.Question `json:"question"`
	Reason    string          `json:"reason"`
	CreatedAt time.Time       `json:"created_at"`
	Resolved  bool            `json:"resolved"`
}

// Extract the actual played snapshot on the server. Never trust client supplied
// question contents or expose its answer in the reporting response.
func (s *Storage) ReportQuestion(uid int64, duelID, quizID string, index int, reason string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 1000 {
		return "", fmt.Errorf("опишите ошибку: до 1000 символов")
	}
	var q models.Question
	var chatID int64
	if duelID != "" && quizID == "" {
		d := s.duels[duelID]
		if d == nil || index < 0 || index >= len(d.Rounds) || d.Player1.ID != uid && (d.Player2 == nil || d.Player2.ID != uid) {
			return "", fmt.Errorf("вопрос недоступен")
		}
		r := d.Rounds[index]
		answered := r.Player1Answered
		if d.Player1.ID != uid {
			answered = r.Player2Answered
		}
		if !answered {
			return "", fmt.Errorf("сначала ответьте на вопрос")
		}
		q = r.Question
		chatID = d.ChatID
	} else if quizID != "" && duelID == "" {
		quiz := s.expressQuiz[quizID]
		if quiz == nil || index < 0 || index >= len(quiz.Questions) || quiz.Participants[uid] == nil || quiz.Participants[uid].TotalAnswers <= index {
			return "", fmt.Errorf("вопрос недоступен")
		}
		q = quiz.Questions[index]
		chatID = quiz.ChatID
	} else {
		return "", fmt.Errorf("укажите игру")
	}
	if !s.isMemberLocked(chatID, uid) {
		return "", fmt.Errorf("вы не состоите в группе")
	}
	for _, r := range s.reports {
		if r.ChatID == chatID && r.UserID == uid && r.Question.ID == q.ID && !r.Resolved {
			return r.ID, nil
		}
	}
	id := "report-" + cryptorand.Text()
	s.reports[id] = QuestionReport{ID: id, ChatID: chatID, UserID: uid, UserName: s.getUserLocked(uid).FullName(), Question: cloneQuestion(q), Reason: reason, CreatedAt: time.Now()}
	return id, nil
}
func (s *Storage) Reports(chatID int64) []QuestionReport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []QuestionReport{}
	for _, r := range s.reports {
		if r.ChatID == chatID {
			r.Question = cloneQuestion(r.Question)
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Resolved != out[j].Resolved {
			return !out[i].Resolved
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}
func (s *Storage) ResolveReport(chatID int64, id string, resolved bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.reports[id]
	if !ok || r.ChatID != chatID {
		return fmt.Errorf("замечание не найдено")
	}
	r.Resolved = resolved
	s.reports[id] = r
	return nil
}
