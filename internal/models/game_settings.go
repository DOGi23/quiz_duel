package models

// GameSettings holds one group's game rules and leaderboard reward.
type GameSettings struct {
	DuelQuestionCount int    `json:"duel_question_count"`
	QuizQuestionCount int    `json:"quiz_question_count"`
	TopReward         string `json:"top_reward"`
}
