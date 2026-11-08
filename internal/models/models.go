package models

import (
	"fmt"
	"strings"
	"time"
)

// User представляет пользователя (ученика или учителя)
type User struct {
	ID             int64         `json:"id"`
	FirstName      string        `json:"first_name"`
	LastName       string        `json:"last_name"`
	Username       string        `json:"username"`
	PhotoURL       string        `json:"photo_url"`
	School         string        `json:"school"`
	ClassGroup     string        `json:"class_group"`
	Role           string        `json:"role"` // "student" или "teacher"
	XP             int           `json:"xp"`
	Level          int           `json:"level"`
	Wins           int           `json:"wins"`
	Losses         int           `json:"losses"`
	Draws          int           `json:"draws"`
	DailyDuelsWith map[int64]int `json:"daily_duels_with"` // opponentID -> count
	LastResetDate  string        `json:"last_reset_date"`
	Achievements   []Achievement `json:"achievements"`
}

func (u *User) FullName() string {
	if u == nil {
		return "Игрок"
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name != "" {
		return name
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	return fmt.Sprintf("Игрок %d", u.ID)
}

// Achievement достижение пользователя
type Achievement struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	UnlockedAt  string `json:"unlocked_at,omitempty"`
}

// Category школьный предмет
type Category struct {
	MinQuestionCount int    `json:"min_question_count"`
	ImageURL         string `json:"image_url,omitempty"`
	ID               string `json:"id"`
	Name             string `json:"name"`
	Icon             string `json:"icon"`
	Color            string `json:"color"`
	Description      string `json:"description"`
}

// SubjectCatalog adds current bank availability without storing derived counts.
type SubjectCatalog struct {
	Category
	QuestionCount int  `json:"question_count"`
	Ready         bool `json:"ready"`
}

// Question вопрос квиза
type Question struct {
	Disabled          bool     `json:"disabled"`
	ChatID            int64    `json:"chat_id,omitempty"`
	ID                string   `json:"id"`
	Category          string   `json:"category"`
	Topic             string   `json:"topic"`
	Text              string   `json:"text"`
	Options           []string `json:"options"`
	CorrectIndex      int      `json:"correct_index"`
	Explanation       string   `json:"explanation"`
	IsTeacherQuestion bool     `json:"is_teacher_question"`
	TeacherName       string   `json:"teacher_name,omitempty"`
	Multiplier        float64  `json:"multiplier"` // 2.0 для вопросов от учителя
	Difficulty        string   `json:"difficulty"` // easy, medium, hard
}

// Round раунд дуэли.
type Round struct {
	Player1Loaded    bool      `json:"player1_loaded"`
	Player2Loaded    bool      `json:"player2_loaded"`
	Player1Ready     bool      `json:"player1_ready"`
	Player2Ready     bool      `json:"player2_ready"`
	StartsAt         time.Time `json:"starts_at"`
	Player1Answered  bool      `json:"player1_answered"`
	Player2Answered  bool      `json:"player2_answered"`
	Player1StartedAt time.Time `json:"player1_started_at"`
	Player2StartedAt time.Time `json:"player2_started_at"`
	RoundNumber      int       `json:"round_number"`
	Category         string    `json:"category"`
	CategoryPickerID int64     `json:"category_picker_id"`
	Question         Question  `json:"question"`
	Player1Answer    int       `json:"player1_answer"` // -1 - не ответил, 0-3
	Player2Answer    int       `json:"player2_answer"` // -1 - не ответил, 0-3
	Player1TimeSec   float64   `json:"player1_time_sec"`
	Player2TimeSec   float64   `json:"player2_time_sec"`
	Player1Score     int       `json:"player1_score"`
	Player2Score     int       `json:"player2_score"`
}

// Duel сессия дуэли между двумя игроками
type Duel struct {
	CloseReason    string    `json:"close_reason,omitempty"`
	ClosedAt       time.Time `json:"closed_at,omitempty"`
	CloseNotified  bool      `json:"close_notified,omitempty"`
	DismissedBy    []int64   `json:"dismissed_by,omitempty"`
	ExpiresAt      time.Time `json:"expires_at"`
	ID             string    `json:"id"`
	ChatID         int64     `json:"chat_id"`
	RematchOf      string    `json:"rematch_of,omitempty"`
	Player1        *User     `json:"player1"`
	Player2        *User     `json:"player2"`
	Rounds         []Round   `json:"rounds"`
	QuestionCount  int       `json:"question_count"`
	CurrentRound   int       `json:"current_round"`
	Status         string    `json:"status"`    // waiting, preparing, countdown, in_progress, finished, declined
	WinnerID       int64     `json:"winner_id"` // 0 если ничья
	Player1XPDelta int       `json:"player1_xp_delta"`
	Player2XPDelta int       `json:"player2_xp_delta"`
	IsRewarded     bool      `json:"is_rewarded"` // false если превышен лимит 3 дуэлей в сутки
	RewardNotice   string    `json:"reward_notice"`
	CreatedAt      time.Time `json:"created_at"`
	FinishedAt     time.Time `json:"finished_at"`
}

// ExpressQuiz экспресс-квиз на уроке для всего чата
type ExpressQuiz struct {
	ID           string                     `json:"id"`
	ChatID       int64                      `json:"chat_id"`
	TeacherID    int64                      `json:"teacher_id"`
	TeacherName  string                     `json:"teacher_name"`
	Subject      string                     `json:"subject"`
	Topic        string                     `json:"topic"`
	Questions    []Question                 `json:"questions"`
	TimeLimitSec int                        `json:"time_limit_sec"` // 15 по умолчанию
	IsActive     bool                       `json:"is_active"`
	Participants map[int64]*QuizParticipant `json:"participants"`
	CreatedAt    time.Time                  `json:"created_at"`
}

// QuizParticipant участник экспресс-квиза
type QuizParticipant struct {
	QuestionStartedAt time.Time `json:"question_started_at"`
	UserID            int64     `json:"user_id"`
	Name              string    `json:"name"`
	Score             int       `json:"score"`
	CorrectCount      int       `json:"correct_count"`
	TotalAnswers      int       `json:"total_answers"`
	TotalTimeSec      float64   `json:"total_time_sec"`
}

// ChatInfo метаданные группового чата MAX
type ChatInfo struct {
	ID                int64  `json:"id"`
	Title             string `json:"title"`
	ParticipantsCount int    `json:"participants_count"`
}

// LeaderboardEntry запись таблицы лидеров
type LeaderboardEntry struct {
	Rank       int    `json:"rank"`
	UserID     int64  `json:"user_id"`
	Name       string `json:"name"`
	PhotoURL   string `json:"photo_url"`
	ClassGroup string `json:"class_group"`
	School     string `json:"school"`
	XP         int    `json:"xp"`
	Level      int    `json:"level"`
	Wins       int    `json:"wins"`
	DuelsCount int    `json:"duels_count"`
	IsLeader   bool   `json:"is_leader"`
}

// TopicAnalytics аналитика ошибок для учителя
type TopicAnalytics struct {
	Subject        string  `json:"subject"`
	Topic          string  `json:"topic"`
	TotalAttempts  int     `json:"total_attempts"`
	ErrorsCount    int     `json:"errors_count"`
	ErrorRate      float64 `json:"error_rate"` // %
	Status         string  `json:"status"`     // "Отлично", "Требует внимания", "Критично"
	Recommendation string  `json:"recommendation"`
}
