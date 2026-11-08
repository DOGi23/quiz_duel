package storage

import (
	"maps"
	"slices"
	"time"

	"first-max-bot/internal/models"
)

func (s *Storage) progressLocked(chatID, userID int64) *models.PlayerProgress {
	if s.chatProgress[chatID] == nil {
		s.chatProgress[chatID] = make(map[int64]*models.PlayerProgress)
	}
	p := s.chatProgress[chatID][userID]
	if p == nil {
		p = &models.PlayerProgress{Level: 1, Achievements: []models.Achievement{}}
		s.chatProgress[chatID][userID] = p
	}
	today := time.Now().Format("2006-01-02")
	if p.LastResetDate != today || p.DailyDuelsWith == nil {
		p.LastResetDate, p.DailyDuelsWith = today, make(map[int64]int)
	}
	return p
}

// chatUserLocked only reads stored state, so it also works under an RLock.
func (s *Storage) chatUserLocked(chatID, userID int64) *models.User {
	u := cloneUser(s.users[userID])
	if u == nil {
		u = &models.User{ID: userID}
	}
	u.Role = "student"
	u.XP, u.Level, u.Wins, u.Losses, u.Draws = 0, 1, 0, 0, 0
	u.LastResetDate = time.Now().Format("2006-01-02")
	u.DailyDuelsWith = make(map[int64]int)
	u.Achievements = []models.Achievement{}
	if p := s.chatProgress[chatID][userID]; p != nil {
		u.XP, u.Level, u.Wins, u.Losses, u.Draws = p.XP, p.Level, p.Wins, p.Losses, p.Draws
		u.Achievements = slices.Clone(p.Achievements)
		if p.LastResetDate == u.LastResetDate {
			u.DailyDuelsWith = maps.Clone(p.DailyDuelsWith)
		}
	}
	return u
}

func (s *Storage) GetChatUser(chatID, userID int64) *models.User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.chatUserLocked(chatID, userID)
}

// Version 1 stored one global score per user. Replay the recorded awards rather
// than copying that global score into every group. Keep the original values in
// an archive: any progress without a recorded game cannot be attributed safely.
func (s *Storage) migrateChatProgressLocked() {
	for id, u := range s.users {
		s.legacyProgress[id] = &models.PlayerProgress{XP: u.XP, Level: u.Level, Wins: u.Wins, Losses: u.Losses, Draws: u.Draws, DailyDuelsWith: maps.Clone(u.DailyDuelsWith), LastResetDate: u.LastResetDate, Achievements: slices.Clone(u.Achievements)}
		u.XP, u.Level, u.Wins, u.Losses, u.Draws = 0, 1, 0, 0, 0
		u.DailyDuelsWith, u.LastResetDate, u.Achievements = nil, "", []models.Achievement{}
	}
	today := time.Now().Format("2006-01-02")
	for _, d := range s.duels {
		if d.Status != "finished" || d.Player2 == nil {
			continue
		}
		p1, p2 := s.progressLocked(d.ChatID, d.Player1.ID), s.progressLocked(d.ChatID, d.Player2.ID)
		p1.XP += d.Player1XPDelta
		p2.XP += d.Player2XPDelta
		switch d.WinnerID {
		case d.Player1.ID:
			p1.Wins++
			p2.Losses++
		case d.Player2.ID:
			p2.Wins++
			p1.Losses++
		default:
			p1.Draws++
			p2.Draws++
		}
		if d.FinishedAt.In(time.Local).Format("2006-01-02") == today {
			p1.DailyDuelsWith[d.Player2.ID]++
			p2.DailyDuelsWith[d.Player1.ID]++
		}
	}
	for _, q := range s.expressQuiz {
		for uid, participant := range q.Participants {
			if participant != nil {
				s.progressLocked(q.ChatID, uid).XP += participant.Score
			}
		}
	}
	for _, players := range s.chatProgress {
		for _, p := range players {
			p.Level = p.XP/200 + 1
		}
	}
	for _, d := range s.duels {
		d.Player1 = s.chatUserLocked(d.ChatID, d.Player1.ID)
		if d.Player2 != nil {
			d.Player2 = s.chatUserLocked(d.ChatID, d.Player2.ID)
		}
	}
}
