package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"first-max-bot/internal/models"
	_ "modernc.org/sqlite"
)

type persistentState struct {
	db       *sql.DB
	mu       sync.Mutex
	revision int64
	hash     [32]byte
}
type snapshot struct {
	Version        int
	Users          map[int64]*models.User
	Chats          map[int64]*models.ChatInfo
	Members        map[int64]map[int64]struct{}
	Duels          map[string]*models.Duel
	Categories     []models.Category
	ChatCategories map[int64][]models.Category
	Questions      []models.Question
	Changes        map[int64]map[string]questionChange
	Reports        map[string]QuestionReport
	Quizzes        map[string]*models.ExpressQuiz
	Topics         map[string]*topicCounter
	GameSettings   map[int64]models.GameSettings
	ChatProgress   map[int64]map[int64]*models.PlayerProgress
	LegacyProgress map[int64]*models.PlayerProgress
}

// Open keeps the coupled game state in one atomic SQLite transaction. Use one
// process and a local persistent disk; revisions reject a second writer.
func Open(ctx context.Context, path string) (*Storage, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*Storage, error) { db.Close(); return nil, err }
	if _, err = db.ExecContext(ctx, `PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL;
 CREATE TABLE IF NOT EXISTS quiz_state (id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL, data BLOB NOT NULL);`); err != nil {
		return fail(err)
	}
	_ = os.Chmod(path, 0600)
	s := New()
	p := &persistentState{db: db}
	s.persistence = p
	var raw []byte
	err = db.QueryRowContext(ctx, "SELECT revision,data FROM quiz_state WHERE id=1").Scan(&p.revision, &raw)
	if err == sql.ErrNoRows {
		if err = s.Save(ctx); err != nil {
			return fail(err)
		}
		return s, nil
	}
	if err != nil {
		return fail(err)
	}
	var v snapshot
	if err = json.Unmarshal(raw, &v); err != nil {
		return fail(fmt.Errorf("повреждена база: %w", err))
	}
	if (v.Version != 1 && v.Version != 2) || v.Version == 2 && v.ChatProgress == nil || v.Users == nil || v.Duels == nil || v.Members == nil || v.ChatCategories == nil || v.Changes == nil || v.Reports == nil || v.Quizzes == nil || v.Topics == nil || v.Chats == nil {
		return fail(fmt.Errorf("неподдерживаемый или неполный формат базы"))
	}
	s.users, s.chats, s.chatMembers, s.duels = v.Users, v.Chats, v.Members, v.Duels
	s.categories, s.chatCategories, s.questions, s.questionChanges = v.Categories, v.ChatCategories, v.Questions, v.Changes
	s.reports, s.expressQuiz, s.topicStats = v.Reports, v.Quizzes, v.Topics
	for _, u := range s.users {
		if u == nil {
			return fail(fmt.Errorf("некорректный пользователь в базе"))
		}
	}
	if v.Version == 2 {
		s.chatProgress = v.ChatProgress
		if v.LegacyProgress != nil {
			s.legacyProgress = v.LegacyProgress
		}
		for _, players := range s.chatProgress {
			for _, progress := range players {
				if progress == nil || progress.XP < 0 || progress.Level < 1 {
					return fail(fmt.Errorf("некорректный прогресс игрока в базе"))
				}
			}
		}
	}
	// Older snapshots have no settings and retain the default three questions.
	for chatID, settings := range v.GameSettings {
		if err := s.SetGameSettings(chatID, settings); err != nil {
			return fail(fmt.Errorf("некорректные настройки в базе: %w", err))
		}
	}
	for _, d := range s.duels {
		if d == nil || d.Player1 == nil || len(d.Rounds) < 1 || len(d.Rounds) > MaxQuestionCount || d.CurrentRound < 1 || d.CurrentRound > len(d.Rounds) {
			return fail(fmt.Errorf("некорректная дуэль в базе"))
		}
		if d.Player2 == nil && (d.Status == "preparing" || d.Status == "countdown" || d.Status == "in_progress" || d.Status == "finished") {
			return fail(fmt.Errorf("в дуэли в базе отсутствует второй игрок"))
		}
		if d.Status == "countdown" || d.Status == "preparing" {
			d.Status = "preparing"
			r := &d.Rounds[d.CurrentRound-1]
			r.Player1Loaded = false
			r.Player2Loaded = false
			r.Player1Ready = false
			r.Player2Ready = false
			r.StartsAt = time.Time{}
			r.Player1StartedAt = time.Time{}
			r.Player2StartedAt = time.Time{}
		}
	}
	p.hash = sha256.Sum256(raw)
	if v.Version == 1 {
		s.migrateChatProgressLocked()
		if err = s.Save(ctx); err != nil {
			return fail(fmt.Errorf("перенос рейтингов по чатам: %w", err))
		}
	}
	return s, nil
}

func (s *Storage) Save(ctx context.Context) error {
	p := s.persistence
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	s.mu.RLock()
	raw, err := json.Marshal(snapshot{Version: 2, Users: s.users, Chats: s.chats, Members: s.chatMembers, Duels: s.duels, Categories: s.categories, ChatCategories: s.chatCategories, Questions: s.questions, Changes: s.questionChanges, Reports: s.reports, Quizzes: s.expressQuiz, Topics: s.topicStats, GameSettings: s.gameSettings, ChatProgress: s.chatProgress, LegacyProgress: s.legacyProgress})
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	hash := sha256.Sum256(raw)
	if hash == p.hash {
		return nil
	}
	var result sql.Result
	if p.revision == 0 {
		result, err = p.db.ExecContext(ctx, "INSERT INTO quiz_state(id,revision,data) VALUES(1,1,?) ON CONFLICT(id) DO NOTHING", raw)
	} else {
		result, err = p.db.ExecContext(ctx, "UPDATE quiz_state SET revision=revision+1,data=? WHERE id=1 AND revision=?", raw, p.revision)
	}
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("база изменена другим процессом: запустите только один экземпляр бота")
	}
	p.revision++
	p.hash = hash
	return nil
}
func (s *Storage) Close() error {
	if s.persistence == nil {
		return nil
	}
	return s.persistence.db.Close()
}
