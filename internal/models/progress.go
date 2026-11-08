package models

// PlayerProgress belongs to a (chatID, userID) pair. Identity is shared by chats.
type PlayerProgress struct {
	XP             int           `json:"xp"`
	Level          int           `json:"level"`
	Wins           int           `json:"wins"`
	Losses         int           `json:"losses"`
	Draws          int           `json:"draws"`
	DailyDuelsWith map[int64]int `json:"daily_duels_with"`
	LastResetDate  string        `json:"last_reset_date"`
	Achievements   []Achievement `json:"achievements"`
}
