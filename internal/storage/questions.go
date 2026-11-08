package storage

import (
	"first-max-bot/internal/models"
	"fmt"
)

type questionChange struct {
	Question models.Question
	Deleted  bool
}

func (s *Storage) questionsLocked(chatID int64, includeDisabled bool) []models.Question {
	result := []models.Question{}
	for _, q := range s.questions {
		if q.ChatID != 0 && q.ChatID != chatID {
			continue
		}
		if change, ok := s.questionChanges[chatID][q.ID]; ok {
			if change.Deleted {
				continue
			}
			q = change.Question
		}
		if !includeDisabled && q.Disabled {
			continue
		}
		result = append(result, cloneQuestion(q))
	}
	return result
}

func (s *Storage) ChangeQuestion(chatID int64, id string, updated *models.Question, disabled *bool, remove bool) (models.Question, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var original models.Question
	for _, q := range s.questionsLocked(chatID, true) {
		if q.ID == id {
			original = q
			break
		}
	}
	if original.ID == "" {
		return models.Question{}, fmt.Errorf("вопрос не найден в этой группе")
	}
	q := original
	if updated != nil {
		q = *updated
		q.ID, q.ChatID, q.Disabled = id, chatID, original.Disabled
		q.IsTeacherQuestion, q.Multiplier, q.Difficulty = original.IsTeacherQuestion, original.Multiplier, original.Difficulty
		var err error
		q, err = s.validateQuestionLocked(q)
		if err != nil {
			return models.Question{}, err
		}
	}
	if disabled != nil {
		q.Disabled = *disabled
	}
	if s.questionChanges[chatID] == nil {
		s.questionChanges[chatID] = make(map[string]questionChange)
	}
	s.questionChanges[chatID][id] = questionChange{Question: cloneQuestion(q), Deleted: remove}
	return cloneQuestion(q), nil
}
