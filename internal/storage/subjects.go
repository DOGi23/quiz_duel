package storage

import (
	"crypto/rand"
	"first-max-bot/internal/models"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

const DefaultSubjectMinimum = 3

func subjectMinimum(category models.Category) int {
	if category.MinQuestionCount > 0 {
		return category.MinQuestionCount
	}
	// Older catalogs and built-in subjects keep their former one-question limit.
	return 1
}

func (s *Storage) subjectCatalogLocked(chatID int64) []models.SubjectCatalog {
	counts := make(map[string]int)
	for _, q := range s.questionsLocked(chatID, false) {
		counts[q.Category]++
	}
	catalog := make([]models.SubjectCatalog, 0, len(s.categoriesLocked(chatID)))
	for _, category := range s.categoriesLocked(chatID) {
		category.MinQuestionCount = subjectMinimum(category)
		catalog = append(catalog, models.SubjectCatalog{Category: category, QuestionCount: counts[category.ID], Ready: counts[category.ID] >= category.MinQuestionCount})
	}
	return catalog
}

func (s *Storage) GetSubjectCatalog(chatID int64) []models.SubjectCatalog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.subjectCatalogLocked(chatID)
}

func (s *Storage) categoriesLocked(chatID int64) []models.Category {
	if list, ok := s.chatCategories[chatID]; ok {
		return list
	}
	return s.categories
}

func (s *Storage) categoryExistsLocked(chatID int64, id string) bool {
	return slices.ContainsFunc(s.categoriesLocked(chatID), func(c models.Category) bool { return c.ID == id })
}

// Each group has its own catalog. Existing questions and running game snapshots
// survive removal, but removed subjects cannot be used to create new games.
// An omitted image update keeps the current image; an empty string removes it.
func (s *Storage) SaveCategory(chatID int64, category models.Category, imageUpdate ...string) (models.Category, error) {
	if len(imageUpdate) > 0 {
		category.ImageURL = imageUpdate[0]
	}
	if err := validateSubjectImage(category.ImageURL); err != nil {
		return models.Category{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	category.Name, category.Description = strings.TrimSpace(category.Name), strings.TrimSpace(category.Description)
	if chatID == 0 || category.Name == "" || utf8.RuneCountInString(category.Name) > 80 || utf8.RuneCountInString(category.Description) > 500 {
		return models.Category{}, fmt.Errorf("укажите название до 80 символов и описание до 500 символов")
	}
	list := slices.Clone(s.categoriesLocked(chatID))
	index := slices.IndexFunc(list, func(c models.Category) bool { return c.ID == category.ID })
	if category.ID != "" && index < 0 {
		return models.Category{}, fmt.Errorf("предмет не найден")
	}
	if category.MinQuestionCount == 0 {
		category.MinQuestionCount = DefaultSubjectMinimum
		if index >= 0 {
			category.MinQuestionCount = subjectMinimum(list[index])
		}
	}
	if category.MinQuestionCount < 1 || category.MinQuestionCount > MaxQuestionCount {
		return models.Category{}, fmt.Errorf("минимум вопросов должен быть от 1 до %d", MaxQuestionCount)
	}
	if slices.ContainsFunc(list, func(c models.Category) bool { return c.ID != category.ID && strings.EqualFold(c.Name, category.Name) }) {
		return models.Category{}, fmt.Errorf("предмет с таким названием уже существует")
	}
	if index < 0 {
		if len(list) >= 40 {
			return models.Category{}, fmt.Errorf("в чате может быть не больше 40 предметов")
		}
		category.ID = "subject-" + rand.Text()
		category.Icon = "menu_book"
		list = append(list, category)
	} else {
		// Metadata not edited by the teacher stays stable.
		category.Icon, category.Color = list[index].Icon, list[index].Color
		if len(imageUpdate) == 0 && category.ImageURL == "" {
			category.ImageURL = list[index].ImageURL
		}
		list[index] = category
	}
	s.chatCategories[chatID] = list
	return category, nil
}

func (s *Storage) DeleteCategory(chatID int64, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.categoryExistsLocked(chatID, id) {
		return fmt.Errorf("предмет не найден")
	}
	list := slices.Clone(s.categoriesLocked(chatID))
	s.chatCategories[chatID] = slices.DeleteFunc(list, func(c models.Category) bool { return c.ID == id })
	return nil
}

func (s *Storage) UserChats(userID int64) []models.ChatInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := []models.ChatInfo{}
	for id := range s.chatMembers {
		if s.isMemberLocked(id, userID) {
			info := models.ChatInfo{ID: id, Title: fmt.Sprintf("Чат %d", id)}
			if chat := s.chats[id]; chat != nil {
				info = *chat
			}
			list = append(list, info)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}
