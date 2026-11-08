package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/max-messenger/max-bot-api-client-go/v2/model"
)

type identityKey struct{}

// Decode each URL value once; decoding the whole query corrupts signed names
// containing '+', '&' or '%'. See dev.max.ru/docs/webapps/validation.
func validateInitData(raw, token string, now time.Time) (model.InitData, error) {
	var data model.InitData
	values, err := url.ParseQuery(raw)
	if err != nil || token == "" {
		return data, fmt.Errorf("invalid initData")
	}
	for _, v := range values {
		if len(v) != 1 {
			return data, fmt.Errorf("duplicate initData parameter")
		}
	}
	signature, err := hex.DecodeString(values.Get("hash"))
	if err != nil || len(signature) != sha256.Size {
		return data, fmt.Errorf("invalid hash")
	}
	values.Del("hash")
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+values.Get(k))
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(parts, "\n")))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return data, fmt.Errorf("invalid signature")
	}
	data.AuthDate, err = strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil || data.AuthDate <= 0 || data.AuthDate > now.Add(time.Minute).Unix() || now.Sub(time.Unix(data.AuthDate, 0)) > 24*time.Hour {
		return data, fmt.Errorf("expired initData")
	}
	if err = json.Unmarshal([]byte(values.Get("user")), &data.User); err != nil || data.User.ID <= 0 {
		return data, fmt.Errorf("invalid user")
	}
	if chat := values.Get("chat"); chat != "" {
		if err = json.Unmarshal([]byte(chat), &data.Chat); err != nil {
			return data, err
		}
	}
	data.StartParam = values.Get("start_param")
	return data, nil
}

func userID(r *http.Request) int64 { id, _ := r.Context().Value(identityKey{}).(int64); return id }

func (s *Server) authenticated(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var id int64
		{
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				apiError(w, 401, "Откройте приложение из MAX")
				return
			}
			data, err := validateInitData(strings.TrimPrefix(header, "Bearer "), s.cfg.BotToken, time.Now())
			if err != nil {
				apiError(w, 401, "Сессия MAX недействительна. Откройте приложение заново.")
				return
			}
			id = data.User.ID
		}
		w.Header().Set("Cache-Control", "no-store")
		next(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	}
}

func (s *Server) checkChat(w http.ResponseWriter, r *http.Request, id int64) bool {
	if !s.storage.IsChatMember(id, userID(r)) {
		apiError(w, 403, "Доступ разрешён только участникам этого чата")
		return false
	}
	return true
}

func (s *Server) isTeacher(r *http.Request, chatID int64) bool {
	return s.cfg.IsTeacher(userID(r)) || s.bot != nil && s.bot.IsTeacher(r.Context(), chatID, userID(r))
}

func (s *Server) checkTeacher(w http.ResponseWriter, r *http.Request, chatID int64) bool {
	if !s.isTeacher(r, chatID) {
		apiError(w, 403, "Доступно только преподавателю")
		return false
	}
	return true
}
