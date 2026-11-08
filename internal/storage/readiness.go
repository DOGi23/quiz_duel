package storage

import (
	"first-max-bot/internal/models"
	"fmt"
	"time"
)

const duelCountdown = 3 * time.Second

func (s *Storage) startCountdownLocked(d *models.Duel) {
	if d.Status == "countdown" && !time.Now().Before(d.Rounds[d.CurrentRound-1].StartsAt) {
		d.Status = "in_progress"
	}
}

// Loaded is a separate acknowledgement after the readiness screen was rendered.
// Merely accepting an invitation or polling does not mark the client as loaded.
func (s *Storage) SetDuelReadiness(id string, userID int64, roundNum int, ready bool) (*models.Duel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.duels[id]
	if d == nil || d.Player2 == nil || (d.Player1.ID != userID && d.Player2.ID != userID) || !s.isMemberLocked(d.ChatID, userID) {
		return nil, fmt.Errorf("игрок не участвует в дуэли")
	}
	if time.Since(d.CreatedAt) >= time.Hour {
		return nil, fmt.Errorf("дуэль устарела")
	}
	if roundNum != d.CurrentRound || roundNum < 1 || roundNum > len(d.Rounds) {
		return nil, fmt.Errorf("этот раунд недоступен")
	}
	s.startCountdownLocked(d)
	if d.Status == "countdown" || d.Status == "in_progress" {
		return cloneDuel(d), nil
	}
	if d.Status != "preparing" {
		return nil, fmt.Errorf("дуэль ещё не принята или уже завершена")
	}
	r := &d.Rounds[roundNum-1]
	if ready {
		if d.Player1.ID == userID {
			if !r.Player1Loaded {
				return nil, fmt.Errorf("дождитесь загрузки экрана")
			}
			r.Player1Ready = true
		} else {
			if !r.Player2Loaded {
				return nil, fmt.Errorf("дождитесь загрузки экрана")
			}
			r.Player2Ready = true
		}
	} else if d.Player1.ID == userID {
		r.Player1Loaded = true
	} else {
		r.Player2Loaded = true
	}
	// Only the first round requires an explicit Ready action. Later rounds
	// acknowledge readiness when the client finishes rendering the next screen.
	if roundNum > 1 {
		r.Player1Ready, r.Player2Ready = r.Player1Loaded, r.Player2Loaded
	}
	if r.Player1Loaded && r.Player2Loaded && r.Player1Ready && r.Player2Ready {
		r.StartsAt = time.Now().Add(duelCountdown)
		r.Player1StartedAt, r.Player2StartedAt = r.StartsAt, r.StartsAt
		d.Status = "countdown"
	}
	return cloneDuel(d), nil
}

// Leaving the room before its scheduled start revokes readiness. After the
// start it cannot be used to pause a running clock or gain extra answer time.
func (s *Storage) ResetDuelReadiness(id string, userID int64, roundNum int) (*models.Duel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.duels[id]
	if d == nil || d.Player2 == nil || (d.Player1.ID != userID && d.Player2.ID != userID) || !s.isMemberLocked(d.ChatID, userID) {
		return nil, fmt.Errorf("игрок не участвует в дуэли")
	}
	if roundNum != d.CurrentRound {
		return nil, fmt.Errorf("этот раунд недоступен")
	}
	s.startCountdownLocked(d)
	if d.Status != "preparing" && d.Status != "countdown" {
		return cloneDuel(d), nil
	}
	r := &d.Rounds[roundNum-1]
	if d.Player1.ID == userID {
		r.Player1Loaded, r.Player1Ready = false, false
	} else {
		r.Player2Loaded, r.Player2Ready = false, false
	}
	r.StartsAt, r.Player1StartedAt, r.Player2StartedAt = time.Time{}, time.Time{}, time.Time{}
	d.Status = "preparing"
	return cloneDuel(d), nil
}
