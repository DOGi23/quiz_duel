package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DataPath              string
	BotToken              string
	Port                  string
	HTTPHost              string
	BaseURL               string
	BotID                 int64
	TeacherIDs            map[int64]bool
	MaxInsecureSkipVerify bool
}

func (c *Config) IsTeacher(id int64) bool { return c.TeacherIDs[id] }

func Load() (*Config, error) {
	// Disabled by the operator's explicit choice; applies only to MAX API.
	insecure := true
	if value := strings.TrimSpace(os.Getenv("MAX_INSECURE_SKIP_VERIFY")); value != "" {
		var err error
		insecure, err = strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("MAX_INSECURE_SKIP_VERIFY должен быть true или false")
		}
	}
	token := strings.TrimSpace(os.Getenv("BOT_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("TOKEN"))
	}
	if token == "" {
		return nil, fmt.Errorf("не задан BOT_TOKEN: укажите токен бота MAX")
	}
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return nil, fmt.Errorf("PORT должен быть числом от 1 до 65535")
	}
	host := strings.TrimSpace(os.Getenv("HTTP_HOST"))
	if host == "" {
		// Native runs stay on loopback. Managed Render ingress must reach the
		// process from outside its container; Docker sets HTTP_HOST explicitly.
		host = "127.0.0.1"
		if strings.EqualFold(strings.TrimSpace(os.Getenv("RENDER")), "true") || strings.TrimSpace(os.Getenv("RENDER_EXTERNAL_URL")) != "" {
			host = "0.0.0.0"
		}
	}
	if net.ParseIP(host) == nil {
		return nil, fmt.Errorf("HTTP_HOST должен быть IP-адресом интерфейса HTTP-сервера")
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("BASE_URL")), "/")
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(os.Getenv("RENDER_EXTERNAL_URL")), "/")
	}
	u, err := url.Parse(base)
	if base != "" && (err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "") {
		return nil, fmt.Errorf("BASE_URL должен быть HTTP(S) URL без параметров и пароля")
	}
	var botID int64
	if v := strings.TrimSpace(os.Getenv("BOT_ID")); v != "" {
		botID, err = strconv.ParseInt(v, 10, 64)
		if err != nil || botID <= 0 {
			return nil, fmt.Errorf("BOT_ID должен быть положительным числом")
		}
	}
	teachers := make(map[int64]bool)
	for _, v := range strings.Split(os.Getenv("TEACHER_IDS"), ",") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("TEACHER_IDS должен содержать ID через запятую")
		}
		teachers[id] = true
	}
	path := strings.TrimSpace(os.Getenv("DATA_PATH"))
	if path == "" {
		path = "data/quiz.db"
	}
	return &Config{HTTPHost: host, DataPath: path, BotToken: token, Port: port, BaseURL: base, BotID: botID, TeacherIDs: teachers, MaxInsecureSkipVerify: insecure}, nil
}
