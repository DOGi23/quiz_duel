package storage

import (
	cryptorand "crypto/rand"
	"fmt"
	"maps"
	"math"
	"math/rand"
	"slices"
	"sort"
	"strings"

	"first-max-bot/internal/models"
)

func cloneUser(u *models.User) *models.User {
	if u == nil {
		return nil
	}
	cp := *u
	cp.DailyDuelsWith = maps.Clone(u.DailyDuelsWith)
	cp.Achievements = slices.Clone(u.Achievements)
	return &cp
}

func cloneQuestion(q models.Question) models.Question {
	q.Options = slices.Clone(q.Options)
	return q
}

func cloneDuel(d *models.Duel) *models.Duel {
	if d == nil {
		return nil
	}
	cp := *d
	cp.QuestionCount = len(d.Rounds)
	cp.DismissedBy = slices.Clone(d.DismissedBy)
	cp.Player1, cp.Player2 = cloneUser(d.Player1), cloneUser(d.Player2)
	cp.Rounds = slices.Clone(d.Rounds)
	for i := range cp.Rounds {
		cp.Rounds[i].Question = cloneQuestion(d.Rounds[i].Question)
	}
	return &cp
}

func cloneQuiz(q *models.ExpressQuiz) *models.ExpressQuiz {
	if q == nil {
		return nil
	}
	cp := *q
	cp.Questions = slices.Clone(q.Questions)
	for i := range cp.Questions {
		cp.Questions[i] = cloneQuestion(q.Questions[i])
	}
	cp.Participants = make(map[int64]*models.QuizParticipant, len(q.Participants))
	for id, p := range q.Participants {
		part := *p
		cp.Participants[id] = &part
	}
	return &cp
}

func (s *Storage) GetUser(id int64) *models.User {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneUser(s.getUserLocked(id))
}

func (s *Storage) getUserLocked(id int64) *models.User {
	u := s.users[id]
	if u == nil {
		u = &models.User{ID: id, Role: "student", Level: 1, Achievements: []models.Achievement{}}
		s.users[id] = u
	}
	return u
}

// SaveUser updates profile fields only; stale snapshots cannot overwrite earned XP.
func (s *Storage) SaveUser(u *models.User) {
	if u == nil || u.ID <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dst := s.getUserLocked(u.ID)
	dst.FirstName, dst.LastName, dst.Username = u.FirstName, u.LastName, u.Username
	dst.PhotoURL, dst.School, dst.ClassGroup = u.PhotoURL, u.School, u.ClassGroup
	dst.Role = u.Role
}

func (s *Storage) UpsertChat(info models.ChatInfo) {
	if info.ID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chats[info.ID] = &info
}

func (s *Storage) GetChat(id int64) *models.ChatInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if c := s.chats[id]; c != nil {
		cp := *c
		return &cp
	}
	return nil
}

func (s *Storage) RegisterChatMember(chatID int64, user *models.User) {
	if chatID == 0 || user == nil || user.ID <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.getUserLocked(user.ID)
	u.FirstName, u.LastName, u.Username, u.PhotoURL = user.FirstName, user.LastName, user.Username, user.PhotoURL
	if s.chatMembers[chatID] == nil {
		s.chatMembers[chatID] = make(map[int64]struct{})
	}
	s.chatMembers[chatID][u.ID] = struct{}{}
}

func (s *Storage) SyncChatMembers(chatID int64, title string, members []models.User) {
	if chatID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	set := make(map[int64]struct{}, len(members))
	for _, m := range members {
		if m.ID <= 0 {
			continue
		}
		u := s.getUserLocked(m.ID)
		u.FirstName, u.LastName, u.Username, u.PhotoURL = m.FirstName, m.LastName, m.Username, m.PhotoURL
		set[m.ID] = struct{}{}
	}
	s.chatMembers[chatID] = set
	s.chats[chatID] = &models.ChatInfo{ID: chatID, Title: title, ParticipantsCount: len(set)}
}

func (s *Storage) isMemberLocked(chatID, userID int64) bool {
	if userID <= 0 {
		return false
	}
	if chatID == 0 {
		return false
	}
	_, ok := s.chatMembers[chatID][userID]
	return ok
}

func (s *Storage) IsChatMember(chatID, userID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isMemberLocked(chatID, userID)
}

func (s *Storage) RemoveChatMember(chatID, userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.chatMembers[chatID], userID)
}

func (s *Storage) RemoveChat(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.chatMembers, chatID)
	delete(s.chats, chatID)
	delete(s.gameSettings, chatID)
}

func (s *Storage) GetChatMembers(chatID int64) []*models.User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []*models.User{}
	for id := range s.users {
		if s.isMemberLocked(chatID, id) {
			result = append(result, s.chatUserLocked(chatID, id))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].XP == result[j].XP {
			return result[i].ID < result[j].ID
		}
		return result[i].XP > result[j].XP
	})
	return result
}

func (s *Storage) PickRandomOpponent(chatID, exclude int64) *models.User {
	list := s.GetChatMembers(chatID)
	list = slices.DeleteFunc(list, func(u *models.User) bool { return u.ID == exclude })
	if len(list) == 0 {
		return nil
	}
	return list[rand.Intn(len(list))]
}

func (s *Storage) GetCategories(chatID ...int64) []models.Category {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(chatID) > 0 {
		return slices.Clone(s.categoriesLocked(chatID[0]))
	}
	return slices.Clone(s.categories)
}

func (s *Storage) GetQuestionsByCategory(chatID int64, cat string) []models.Question {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []models.Question{}
	for _, q := range s.questionsLocked(chatID, true) {
		if cat == "" || q.Category == cat {
			result = append(result, cloneQuestion(q))
		}
	}
	return result
}

func (s *Storage) AddQuestion(q models.Question) (models.Question, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	q, err = s.validateQuestionLocked(q)
	if err != nil {
		return models.Question{}, err
	}
	q.ID = "custom-" + cryptorand.Text()
	q.IsTeacherQuestion, q.Multiplier = true, 2
	s.questions = append(s.questions, q)
	return cloneQuestion(q), nil
}

func (s *Storage) validateQuestionLocked(q models.Question) (models.Question, error) {
	validCat := s.categoryExistsLocked(q.ChatID, q.Category)
	q.Topic, q.Text = strings.TrimSpace(q.Topic), strings.TrimSpace(q.Text)
	if !validCat || q.Topic == "" || q.Text == "" || len(q.Text) > 4000 || len(q.Topic) > 200 || len(q.Explanation) > 4000 || len(q.Options) != 4 || q.CorrectIndex < 0 || q.CorrectIndex >= len(q.Options) {
		return models.Question{}, fmt.Errorf("укажите предмет, тему, вопрос, четыре варианта и верный ответ")
	}
	q = cloneQuestion(q)
	seen := map[string]bool{}
	for i, opt := range q.Options {
		opt = strings.TrimSpace(opt)
		if opt == "" || len(opt) > 1000 || seen[strings.ToLower(opt)] {
			return models.Question{}, fmt.Errorf("варианты должны быть непустыми и различаться")
		}
		seen[strings.ToLower(opt)] = true
		q.Options[i] = opt
	}
	return cloneQuestion(q), nil
}

func (s *Storage) recordAnswerLocked(chatID int64, q models.Question, correct bool) {
	key := fmt.Sprintf("%d:%s:%s", chatID, q.Category, q.Topic)
	c := s.topicStats[key]
	if c == nil {
		c = &topicCounter{ChatID: chatID, Subject: q.Category, Topic: q.Topic}
		s.topicStats[key] = c
	}
	c.TotalAttempts++
	if !correct {
		c.ErrorsCount++
	}
}

func (s *Storage) GetTopicAnalytics(chatID int64) []models.TopicAnalytics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []models.TopicAnalytics{}
	for _, c := range s.topicStats {
		if c.ChatID != chatID {
			continue
		}
		rate := math.Round(float64(c.ErrorsCount)/float64(c.TotalAttempts)*1000) / 10
		status, rec := "Отлично", "Материал усвоен на высоком уровне."
		if rate > 40 {
			status, rec = "Критично", "Рекомендуется разобрать тему повторно."
		} else if rate > 20 {
			status, rec = "Требует внимания", "Дать задачи на закрепление."
		}
		result = append(result, models.TopicAnalytics{Subject: c.Subject, Topic: c.Topic, TotalAttempts: c.TotalAttempts, ErrorsCount: c.ErrorsCount, ErrorRate: rate, Status: status, Recommendation: rec})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ErrorRate == result[j].ErrorRate {
			return result[i].Subject+result[i].Topic < result[j].Subject+result[j].Topic
		}
		return result[i].ErrorRate > result[j].ErrorRate
	})
	return result
}

func (s *Storage) GetLeaderboard(chatID int64) []models.LeaderboardEntry {
	members := s.GetChatMembers(chatID)
	chat := s.GetChat(chatID)
	result := []models.LeaderboardEntry{}
	for i, u := range members {
		group := u.ClassGroup
		if chat != nil {
			group = chat.Title
		}
		result = append(result, models.LeaderboardEntry{Rank: i + 1, UserID: u.ID, Name: u.FullName(), PhotoURL: u.PhotoURL, ClassGroup: group, School: u.School, XP: u.XP, Level: u.Level, Wins: u.Wins, DuelsCount: u.Wins + u.Losses + u.Draws, IsLeader: i == 0})
	}
	return result
}
