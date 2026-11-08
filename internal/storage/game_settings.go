package storage

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"first-max-bot/internal/models"
)

const MaxQuestionCount = 20

func (s *Storage) gameSettingsLocked(chatID int64) models.GameSettings {
	if settings, ok := s.gameSettings[chatID]; ok {
		return settings
	}
	return models.GameSettings{DuelQuestionCount: 3, QuizQuestionCount: 3}
}

func (s *Storage) GetGameSettings(chatID int64) models.GameSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gameSettingsLocked(chatID)
}

func (s *Storage) SetGameSettings(chatID int64, settings models.GameSettings) error {
	if chatID == 0 || settings.DuelQuestionCount < 1 || settings.DuelQuestionCount > MaxQuestionCount || settings.QuizQuestionCount < 1 || settings.QuizQuestionCount > MaxQuestionCount {
		return fmt.Errorf("количество вопросов должно быть целым числом от 1 до %d", MaxQuestionCount)
	}
	settings.TopReward = strings.TrimSpace(settings.TopReward)
	if !utf8.ValidString(settings.TopReward) || utf8.RuneCountInString(settings.TopReward) > 500 {
		return fmt.Errorf("описание награды должно содержать не больше 500 символов")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gameSettings[chatID] = settings
	return nil
}
