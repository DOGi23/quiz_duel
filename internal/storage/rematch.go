package storage

import (
	"first-max-bot/internal/models"
	"fmt"
	"time"
)

// Both players' concurrent requests resolve to the same room. The first asks;
// the second player's request accepts, while retries never create another room.
func (s *Storage) Rematch(id string, userID int64) (*models.Duel, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireInvitationsLocked()
	parent := s.duels[id]
	if parent == nil || parent.Player2 == nil || (parent.Player1.ID != userID && parent.Player2.ID != userID) {
		return nil, false, fmt.Errorf("дуэль не найдена")
	}
	if parent.Status != "finished" {
		return nil, false, fmt.Errorf("сначала завершите дуэль")
	}
	if !s.isMemberLocked(parent.ChatID, parent.Player1.ID) || !s.isMemberLocked(parent.ChatID, parent.Player2.ID) {
		return nil, false, fmt.Errorf("оба игрока должны состоять в чате")
	}
	for _, d := range s.duels {
		if d.RematchOf != id || d.Status == "declined" || time.Since(d.CreatedAt) >= time.Hour {
			continue
		}
		if d.Status == "waiting" && d.Player2.ID == userID {
			accepted, err := s.acceptDuelLocked(d.ID, d.ChatID, userID)
			return accepted, false, err
		}
		return cloneDuel(d), false, nil
	}
	opponent := parent.Player1.ID
	if opponent == userID {
		opponent = parent.Player2.ID
	}
	// A rematch must not steal either participant from another active room.
	for _, d := range s.duels {
		if d.ChatID != parent.ChatID || time.Since(d.CreatedAt) >= time.Hour || d.Status == "finished" || d.Status == "declined" {
			continue
		}
		if d.Player1.ID == userID || d.Player1.ID == opponent || d.Player2 != nil && (d.Player2.ID == userID || d.Player2.ID == opponent) {
			return nil, false, fmt.Errorf("один из игроков уже участвует в другой дуэли")
		}
	}
	d, err := s.createDuelLocked(parent.ChatID, userID, opponent)
	if err != nil {
		return nil, false, err
	}
	s.duels[d.ID].RematchOf = id
	d.RematchOf = id
	return d, true, nil
}
