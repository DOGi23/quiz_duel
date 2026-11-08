package bot

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"first-max-bot/internal/config"
	"first-max-bot/internal/models"
	"first-max-bot/internal/storage"
	maxbot "github.com/max-messenger/max-bot-api-client-go/v2"
	"github.com/max-messenger/max-bot-api-client-go/v2/model"
)

type Bot struct {
	cfg     *config.Config
	storage *storage.Storage
	client  atomic.Pointer[maxbot.Api]
	botID   atomic.Int64
	appURL  atomic.Value // string: HTTPS link to the bot returned by GetMyInfo
}

func NewBot(cfg *config.Config, store *storage.Storage) *Bot {
	b := &Bot{cfg: cfg, storage: store}
	b.botID.Store(cfg.BotID)
	b.appURL.Store("")
	return b
}

// API возвращает клиент MAX Bot API (для REST-слоя Mini App)
func (b *Bot) API() *maxbot.Api {
	return b.client.Load()
}

func (b *Bot) Run(ctx context.Context) error {
	if b.cfg.BotToken == "" {
		return fmt.Errorf("не задан BOT_TOKEN")
	}

	httpClient := newMAXHTTPClient(b.cfg.MaxInsecureSkipVerify)
	if b.cfg.MaxInsecureSkipVerify {
		log.Println("⚠️ MAX API: проверка TLS-сертификатов отключена (MAX_INSECURE_SKIP_VERIFY=true)")
	}

	api, err := maxbot.NewApi(b.cfg.BotToken, maxbot.WithHTTPClient(httpClient))
	if err != nil {
		return fmt.Errorf("ошибка инициализации MAX Bot API: %w", err)
	}
	b.client.Store(api)

	info, err := api.Bots.GetMyInfo(ctx)
	if err != nil {
		log.Printf("⚠️ Ошибка GetMyInfo: %v", err)
	} else {
		if link := botAppURL(info.Username); link != "" {
			b.appURL.Store(link)
		}
		log.Printf("🤖 Бот авторизован в MAX: ID=%d, Имя=%s, Username=%s", info.UserID, info.FirstName, info.Username)
		if b.botID.Load() == 0 {
			b.botID.Store(info.UserID)
		}
	}

	if err := b.registerCommands(ctx); err != nil {
		log.Printf("Не удалось обновить меню команд MAX: %v", err)
	}
	log.Printf("🚀 MAX Бот «Квиз-Дуэль» слушает обновления...")

	var marker int64
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			updates, newMarker, err := api.Subscriptions.GetUpdates(ctx, marker)
			if _, tErr := errors.AsType[*maxbot.TimeoutError](err); tErr {
				continue
			}
			if err != nil {
				log.Printf("⚠️ Ошибка GetUpdates: %v. Повтор через 3с...", err)
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(3 * time.Second):
				}
				continue
			}
			marker = newMarker

			for _, update := range updates {
				b.handleUpdate(ctx, update)
				if err := b.storage.Save(ctx); err != nil {
					log.Printf("Сохранение обновления MAX: %v", err)
				}
			}
		}
	}
}

// Keep the operator's TLS exception local to this client. Never modify the
// shared default transport, which may be used by unrelated HTTP clients.
func newMAXHTTPClient(insecure bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: insecure} // operator-controlled MAX-only exception
	return &http.Client{Transport: transport, Timeout: 40 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Do not forward the token or TLS exception to a redirected host.
			return http.ErrUseLastResponse
		},
	}
}

func (b *Bot) handleUpdate(ctx context.Context, update model.Update) {
	switch update.UpdateType {
	case model.UpdateBotStarted:
		b.cmdStart(ctx, update)
	case model.UpdateUserRemoved:
		b.storage.RemoveChatMember(update.ChatID, update.UserID)
	case model.UpdateBotRemoved:
		b.storage.RemoveChat(update.ChatID)
	case model.UpdateMessageCreated:
		b.handleMessage(ctx, update)
	case model.UpdateMessageCallback:
		b.handleCallback(ctx, update)
	}
}

func (b *Bot) sendMessage(ctx context.Context, update model.Update, text string, keyboard *model.Keyboard) error {
	if b.API() == nil {
		return fmt.Errorf("MAX API не готов")
	}
	update, err := b.groupDestination(update)
	if err != nil {
		return err
	}
	msg := maxbot.NewMessage().
		SetFormat(model.FormatHTML).
		SetText(text)

	msg.SetChat(update.ChatID)

	if keyboard != nil {
		msg.AddKeyboard(keyboard)
	}

	res, err := b.API().Messages.Send(ctx, msg)
	if err != nil {
		log.Printf("⚠️ Ошибка отправки (ChatID=%d, UserID=%d): %v", update.ChatID, update.UserID, err)
		if keyboard != nil && (strings.Contains(err.Error(), "proto.payload") && (strings.Contains(err.Error(), "links format") || strings.Contains(err.Error(), "webApp"))) {
			// The API rejected the message, so retry once with callback actions.
			// Do not let an invalid app/link button suppress the entire menu.
			fallback := model.NewKeyboard()
			removed := false
			for _, row := range keyboard.Build().Payload.Buttons {
				var callbacks []*model.Button
				for _, button := range row {
					if button.Type == model.ButtonCallback {
						callbacks = append(callbacks, button)
					} else {
						removed = true
						log.Printf("⚠️ MAX отклонил кнопку: type=%s, url=%q, web_app=%q, contact_id=%d", button.Type, button.URL, button.WebApp, button.ContactID)
					}
				}
				if len(callbacks) > 0 {
					target := fallback.AddRow()
					for _, button := range callbacks {
						target.AddCallBack(button.Text, button.Payload)
					}
				}
			}
			var safeKeyboard *model.Keyboard
			if len(fallback.Build().Payload.Buttons) > 0 {
				safeKeyboard = fallback
			}
			if removed {
				return b.sendMessage(ctx, update, text, safeKeyboard)
			}
		}
	} else {
		log.Printf("✅ Сообщение доставлено (ChatID=%d, MsgID=%s)", update.ChatID, res.Message.Body.Mid)
	}
	return err
}

// touchFromUpdate регистрирует пользователя и чат из апдейта
func (b *Bot) touchFromUpdate(ctx context.Context, update model.Update) *models.User {
	userID, first, last, username := getUpdateUser(update)
	if userID <= 0 {
		return &models.User{}
	}
	user := b.storage.GetUser(userID)
	user.Role = "student"
	if b.cfg.IsTeacher(userID) {
		user.Role = "teacher"
	}
	if first != "" {
		user.FirstName = first
	}
	if last != "" {
		user.LastName = last
	}
	if username != "" {
		user.Username = username
	}
	b.storage.SaveUser(user)

	if isGroupUpdate(update) {
		b.storage.RegisterChatMember(update.ChatID, user)
		b.ensureChatMeta(ctx, update.ChatID)
		user = b.storage.GetChatUser(update.ChatID, userID)
		if b.cfg.IsTeacher(userID) {
			user.Role = "teacher"
		}
	}
	return user
}

func getUpdateUser(update model.Update) (int64, string, string, string) {
	if update.Callback != nil && update.Callback.User.UserID > 0 {
		u := update.Callback.User
		return u.UserID, u.FirstName, u.LastName, u.Username
	}
	if u := update.GetUser(); u.UserID > 0 {
		return u.UserID, u.FirstName, u.LastName, u.Username
	}
	if update.User != nil && update.User.UserID > 0 {
		return update.User.UserID, update.User.FirstName, update.User.LastName, update.User.Username
	}
	return update.UserID, "", "", ""
}

func (b *Bot) ensureChatMeta(ctx context.Context, chatID int64) {
	if chatID == 0 || b.API() == nil {
		return
	}
	if info := b.storage.GetChat(chatID); info != nil && info.Title != "" {
		return
	}
	chat, err := b.API().Chats.GetChat(ctx, chatID)
	if err != nil {
		log.Printf("⚠️ GetChat(%d): %v", chatID, err)
		return
	}
	b.storage.UpsertChat(models.ChatInfo{
		ID:                chat.ChatID,
		Title:             chat.Title,
		ParticipantsCount: int(chat.ParticipantsCount),
	})
}

// SyncChatMembersFromAPI подтягивает участников чата из MAX
func (b *Bot) SyncChatMembersFromAPI(ctx context.Context, chatID int64) error {
	if chatID == 0 || b.API() == nil {
		return nil
	}

	chat, err := b.API().Chats.GetChat(ctx, chatID)
	if err != nil {
		return err
	}
	b.storage.UpsertChat(models.ChatInfo{
		ID:                chat.ChatID,
		Title:             chat.Title,
		ParticipantsCount: int(chat.ParticipantsCount),
	})

	var all []models.User
	var marker int64
	for {
		list, err := b.API().Chats.GetMembers(ctx, chatID, marker, 100, nil)
		if err != nil {
			return err
		}
		for _, m := range list.Members {
			if m.IsBot || m.UserID == 0 {
				continue
			}
			all = append(all, models.User{
				ID:        m.UserID,
				FirstName: m.FirstName,
				LastName:  m.LastName,
				Username:  m.Username,
				PhotoURL:  m.AvatarURL,
				Role:      "student",
			})
		}
		if list.Marker == 0 || len(list.Members) == 0 {
			break
		}
		if list.Marker == marker {
			return fmt.Errorf("повтор маркера пагинации MAX")
		}
		marker = list.Marker
	}

	b.storage.SyncChatMembers(chatID, chat.Title, all)
	log.Printf("👥 Синхронизировано участников чата %d (%s): %d", chatID, chat.Title, len(all))
	return nil
}

func (b *Bot) chatTitle(chatID int64) string {
	if info := b.storage.GetChat(chatID); info != nil && info.Title != "" {
		return html.EscapeString(info.Title)
	}
	return "этот чат"
}

func (b *Bot) handleMessage(ctx context.Context, update model.Update) {
	var err error
	update, err = b.groupDestination(update)
	if err != nil {
		log.Printf("Команда бота пропущена: %v", err)
		return
	}
	b.touchFromUpdate(ctx, update)

	cmd := update.GetCommand()
	command := strings.ToLower(cmd.Command)
	if atIdx := strings.Index(command, "@"); atIdx != -1 {
		command = command[:atIdx]
	}

	text := strings.TrimSpace(update.GetMessage().Body.Text)
	lower := strings.ToLower(text)

	if command == "" && strings.HasPrefix(lower, "/") {
		parts := strings.Fields(lower)
		if len(parts) > 0 {
			command = parts[0]
			if atIdx := strings.Index(command, "@"); atIdx != -1 {
				command = command[:atIdx]
			}
		}
	}

	isGroupChat := isGroupUpdate(update)

	switch command {
	case "/start":
		b.cmdStart(ctx, update)
	case "/app", "/arena", "/miniapp":
		b.cmdApp(ctx, update)
	case "/duel", "/play", "/дуэль":
		b.cmdDuel(ctx, update)
	case "/top", "/leaderboard", "/топ", "/рейтинг":
		b.cmdTop(ctx, update)
	case "/quiz", "/квиз":
		b.cmdQuiz(ctx, update)
	case "/teacher", "/учитель":
		b.cmdTeacher(ctx, update)
	case "/stats", "/стата", "/профиль":
		b.cmdStats(ctx, update)
	case "/members", "/чат", "/класс":
		b.cmdMembers(ctx, update)
	case "/team", "/команда", "/about", "/проект":
		b.cmdAbout(ctx, update)
	case "/help", "/помощь", "/правила":
		b.cmdHelp(ctx, update)
	default:
		if strings.Contains(lower, "дуэль") || strings.Contains(lower, "сыграем") {
			b.cmdDuel(ctx, update)
		} else if strings.Contains(lower, "таблица лидеров") || strings.Contains(lower, "кто первый") {
			b.cmdTop(ctx, update)
		} else if !isGroupChat {
			b.cmdStart(ctx, update)
		}
	}
}

func (b *Bot) buildMainMenuKeyboard(chatID int64) *model.Keyboard {
	keyboard := model.NewKeyboard()

	var row1 *model.KeyboardRow
	if b.appURL.Load().(string) != "" && b.botID.Load() != 0 {
		row1 = keyboard.AddRow()
	}
	if b.botID.Load() != 0 && b.appURL.Load().(string) != "" {
		row1.AddOpenApp("⚔️ Открыть Арену «Квиз-Дуэль»", b.botID.Load())
		row1.Build()[0].WebApp = b.appURL.Load().(string)
		row1.Build()[0].Payload = fmt.Sprintf("chat-%d", chatID)
	}

	keyboard.AddRow().
		AddCallBack("⚡ Быстрая дуэль", "cb_quick_duel").
		AddCallBack("🏆 Лидеры чата", "cb_top")

	keyboard.AddRow().
		AddCallBack("👨‍🏫 Кабинет учителя", "cb_teacher").
		AddCallBack("📊 Моя статистика", "cb_stats")

	keyboard.AddRow().
		AddCallBack("👥 Участники чата", "cb_members").
		AddCallBack("❓ Правила", "cb_help")

	return keyboard
}

func (b *Bot) cmdStart(ctx context.Context, update model.Update) {
	var err error
	update, err = b.groupDestination(update)
	if err != nil {
		return
	}
	user := b.touchFromUpdate(ctx, update)
	_ = b.SyncChatMembersFromAPI(ctx, update.ChatID)
	chatLabel := "«" + b.chatTitle(update.ChatID) + "»"

	text := fmt.Sprintf(`⚔️ <b>Добро пожаловать на Арену «Квиз-Дуэль»!</b>

👋 Привет, <b>%s</b>!
Сейчас бот работает в %s. Рейтинг и дуэли — только между участниками этого чата.

🎮 <b>Команды:</b>
• <b>/app</b> — открыть мини-приложение
• <b>/duel</b> — вызов участнику чата (или Reply + /duel)
• <b>/top</b> — таблица лидеров чата
• <b>/members</b> — кто уже в рейтинге
• <b>/quiz</b> — экспресс-квиз на уроке (2x XP)
• <b>/stats</b> — ваша статистика
• <b>/help</b> — правила и anti-abuse

Откройте Mini App кнопкой ниже:`, html.EscapeString(user.FullName()), chatLabel)

	b.sendReply(ctx, update, text, b.buildMainMenuKeyboard(groupContext(update)))
}

func (b *Bot) cmdDuel(ctx context.Context, update model.Update) {
	sender := b.touchFromUpdate(ctx, update)
	if !isGroupUpdate(update) {
		b.chooseDuelChat(ctx, update)
		return
	}

	if isGroupUpdate(update) {
		_ = b.SyncChatMembersFromAPI(ctx, update.ChatID)
	}

	var opponent *models.User
	if update.Message != nil && update.Message.Link != nil && update.Message.Link.Sender != nil && update.Message.Link.Sender.UserID > 0 {
		oppUser := update.Message.Link.Sender
		if oppUser.UserID != sender.ID && !oppUser.IsBot {
			opponent = b.storage.GetUser(oppUser.UserID)
			if oppUser.FirstName != "" {
				opponent.FirstName = oppUser.FirstName
				opponent.LastName = oppUser.LastName
				opponent.Username = oppUser.Username
				b.storage.SaveUser(opponent)
			}

		}
	}

	if opponent != nil {
		if update.ChatID != 0 && !b.storage.IsChatMember(update.ChatID, opponent.ID) {
			b.sendReply(ctx, update, "⚠️ Вызвать на дуэль можно только участника этого чата.", nil)
			return
		}

		b.challengeOpponent(ctx, update, opponent.ID)
		return
	}

	duel, err := b.storage.CreateDuel(update.ChatID, sender.ID, 0)
	if err != nil {
		b.sendReply(ctx, update, "⚠️ "+err.Error(), nil)
		return
	}

	keyboard := model.NewKeyboard()
	if b.botID.Load() != 0 {
		b.addGameButton(keyboard, "📱 Открыть Арену", duel.ID)
	}
	keyboard.AddRow().AddCallBack("⚔️ ПРИНЯТЬ ВЫЗОВ!", fmt.Sprintf("cb_join_%s", duel.ID))

	text := fmt.Sprintf(`⚔️ <b>Открытый вызов в чате!</b>

🛡️ <b>%s</b> (Ур. %d | %d XP) ждёт соперника из «%s»
⏱️ Вопросов: %d, по 15 секунд

Кто готов? Нажмите <b>«ПРИНЯТЬ ВЫЗОВ!»</b> 👇`,
		html.EscapeString(sender.FullName()), sender.Level, sender.XP, b.chatTitle(update.ChatID), len(duel.Rounds))

	b.sendMessage(ctx, update, text, keyboard)
}

func (b *Bot) cmdTop(ctx context.Context, update model.Update) {
	b.touchFromUpdate(ctx, update)

	if isGroupUpdate(update) {
		_ = b.SyncChatMembersFromAPI(ctx, update.ChatID)
	}

	leaders := b.storage.GetLeaderboard(update.ChatID)

	var sb strings.Builder
	title := b.chatTitle(update.ChatID)
	if !isGroupUpdate(update) {
		sb.WriteString("🏆 <b>Ваш личный прогресс</b>\n")
		sb.WriteString("<i>Таблица лидеров доступна в групповом чате (/top)</i>\n\n")
	} else {
		sb.WriteString(fmt.Sprintf("🏆 <b>Лидеры чата «%s»</b>\n\n", title))
	}

	if len(leaders) == 0 {
		sb.WriteString("Пока никого нет в рейтинге. Напишите /duel или /start — участники появятся автоматически.")
	} else {
		for i, l := range leaders {
			if i >= 10 {
				break
			}
			badge := "▫️"
			switch i {
			case 0:
				badge = "🥇"
			case 1:
				badge = "🥈"
			case 2:
				badge = "🥉"
			}
			sb.WriteString(fmt.Sprintf("%s <b>%d. %s</b> — %d XP (Ур. %d, побед: %d)\n",
				badge, l.Rank, html.EscapeString(l.Name), l.XP, l.Level, l.Wins))
		}
		sb.WriteString("\n💡 <i>Победа над лидером даёт больше XP. Вопросы учителя — 2x XP.</i>")
	}

	keyboard := model.NewKeyboard()
	if b.botID.Load() != 0 {
		b.addGameButton(keyboard, "📱 Полный рейтинг в Mini App", fmt.Sprintf("chat-%d", groupContext(update)))
	}
	keyboard.AddRow().AddCallBack("⚔️ Бросить вызов", "cb_quick_duel")

	b.sendReply(ctx, update, sb.String(), keyboard)
}

func (b *Bot) cmdMembers(ctx context.Context, update model.Update) {
	b.touchFromUpdate(ctx, update)
	if !isGroupUpdate(update) {
		b.sendReply(ctx, update, "👥 Список участников доступен в групповом чате. Добавьте бота в классный чат.", nil)
		return
	}

	_ = b.SyncChatMembersFromAPI(ctx, update.ChatID)
	members := b.storage.GetChatMembers(update.ChatID)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("👥 <b>Участники чата «%s»</b>\n", b.chatTitle(update.ChatID)))
	sb.WriteString(fmt.Sprintf("Всего в рейтинге бота: <b>%d</b>\n\n", len(members)))

	if len(members) == 0 {
		sb.WriteString("Пока пусто. Участники появляются после сообщений боту или синхронизации.")
	} else {
		for i, m := range members {
			if i >= 20 {
				sb.WriteString(fmt.Sprintf("… и ещё %d\n", len(members)-20))
				break
			}
			sb.WriteString(fmt.Sprintf("%d. <b>%s</b> — %d XP\n", i+1, html.EscapeString(m.FullName()), m.XP))
		}
	}

	b.sendReply(ctx, update, sb.String(), nil)
}

func (b *Bot) cmdQuiz(ctx context.Context, update model.Update) {
	teacher := b.touchFromUpdate(ctx, update)
	if !b.IsTeacher(ctx, update.ChatID, teacher.ID) {
		b.sendReply(ctx, update, "Доступно только преподавателю.", nil)
		return
	}

	subject := "algebra"
	topic := "Проверка знаний"
	textBody := strings.TrimSpace(update.GetMessage().Body.Text)
	parts := strings.Fields(textBody)
	if len(parts) >= 2 {
		subject = strings.TrimPrefix(strings.ToLower(parts[1]), "@")
		if at := strings.Index(subject, "@"); at != -1 {
			subject = subject[:at]
		}
	}
	if len(parts) >= 3 {
		topic = strings.Join(parts[2:], " ")
	}

	quiz, err := b.storage.StartExpressQuiz(update.ChatID, teacher.ID, teacher.FullName(), subject, topic)
	if err != nil {
		b.sendReply(ctx, update, "⚠️ Ошибка запуска квиза: "+err.Error(), nil)
		return
	}

	keyboard := model.NewKeyboard()
	if b.botID.Load() != 0 {
		b.addGameButton(keyboard, "📝 Пройти экспресс-тест", quiz.ID)
	}

	chatLabel := b.chatTitle(update.ChatID)
	text := fmt.Sprintf(`⚡ <b>Экспресс-квиз для чата!</b>

👩‍🏫 <b>Запустил:</b> %s
📍 <b>Чат:</b> %s
📚 <b>Предмет:</b> %s
📝 <b>Тема:</b> %s
⏱️ 15 секунд на вопрос
🔥 Награда: <b>2x XP</b>

Участники чата — нажмите кнопку и отвечайте:`,
		html.EscapeString(teacher.FullName()), chatLabel, html.EscapeString(subject), html.EscapeString(quiz.Topic))

	b.sendMessage(ctx, update, text, keyboard)
}

func (b *Bot) cmdStats(ctx context.Context, update model.Update) {
	user := b.touchFromUpdate(ctx, update)

	totalGames := user.Wins + user.Losses + user.Draws
	winRate := 0.0
	if totalGames > 0 {
		winRate = float64(user.Wins) / float64(totalGames) * 100.0
	}

	chatLine := ""
	if isGroupUpdate(update) {
		chatLine = fmt.Sprintf("📍 <b>Чат:</b> %s\n", b.chatTitle(update.ChatID))
	}

	text := fmt.Sprintf(`📊 <b>Профиль в «Квиз-Дуэль»</b>

👤 <b>Игрок:</b> %s
%s⭐ <b>Уровень:</b> %d
🔥 <b>XP:</b> %d
⚔️ <b>Дуэлей:</b> %d (Побед: %d, Поражений: %d, Ничьих: %d)
🎯 <b>Винрейт:</b> %.1f%%

🛡️ Anti-abuse: макс. 3 дуэли с XP против одного участника в день в этом чате. XP, уровень и статистика относятся только к этой группе.`,
		html.EscapeString(user.FullName()), chatLine, user.Level, user.XP, totalGames, user.Wins, user.Losses, user.Draws, winRate)

	keyboard := model.NewKeyboard()
	if b.botID.Load() != 0 {
		b.addGameButton(keyboard, "⚔️ Открыть Арену", fmt.Sprintf("chat-%d", groupContext(update)))
	}

	b.sendReply(ctx, update, text, keyboard)
}

func (b *Bot) cmdTeacher(ctx context.Context, update model.Update) {
	id, _, _, _ := getUpdateUser(update)
	if !b.IsTeacher(ctx, update.ChatID, id) {
		b.sendReply(ctx, update, "Доступно только преподавателю.", nil)
		return
	}
	b.touchFromUpdate(ctx, update)
	analytics := b.storage.GetTopicAnalytics(update.ChatID)

	var sb strings.Builder
	sb.WriteString("👨‍🏫 <b>Кабинет преподавателя</b>\n\n")
	sb.WriteString("Создавайте вопросы по темам и запускайте экспресс-квизы. Вопросы учителя дают ученикам <b>2x XP</b>.\n\n")

	if len(analytics) == 0 {
		sb.WriteString("📊 Аналитика пока пуста — появятся после ответов учеников в дуэлях и квизах.")
	} else {
		sb.WriteString("📊 <b>Темы с затруднениями:</b>\n")
		for i, a := range analytics {
			if i >= 4 {
				break
			}
			icon := "🟢"
			if a.Status == "Критично" {
				icon = "🔴"
			} else if a.Status == "Требует внимания" {
				icon = "🟡"
			}
			sb.WriteString(fmt.Sprintf("%s <b>%s</b>: %.1f%% ошибок — <i>%s</i>\n", icon, html.EscapeString(a.Topic), a.ErrorRate, a.Status))
		}
	}

	keyboard := model.NewKeyboard()
	if b.botID.Load() != 0 {
		b.addGameButton(keyboard, "🛠️ Кабинет в Mini App", fmt.Sprintf("chat-%d", groupContext(update)))
	}
	keyboard.AddRow().AddCallBack("⚡ Экспресс-квиз", "cb_start_express_quiz")

	b.sendReply(ctx, update, sb.String(), keyboard)
}

func (b *Bot) cmdAbout(ctx context.Context, update model.Update) {
	text := `🏛️ <b>«Квиз-Дуэль»</b> — проект хакатона

🏆 Команда: «Алгоритм успеха»
📍 Пермский край, г. Березники
🏫 ГБПОУ «Березниковский Политехнический Техникум»

Игроки, рейтинг и дуэли берутся из <b>участников чата</b>, куда добавлен бот — не из демо-списка.`

	keyboard := model.NewKeyboard()
	if b.botID.Load() != 0 {
		b.addGameButton(keyboard, "⚔️ Открыть Mini App", fmt.Sprintf("chat-%d", groupContext(update)))
	}
	b.sendReply(ctx, update, text, keyboard)
}

func (b *Bot) cmdHelp(ctx context.Context, update model.Update) {
	text := `📖 <b>Правила «Квиз-Дуэль»</b>

1️⃣ <b>Дуэль только внутри чата:</b>
<code>/duel</code> — открытый вызов, или Reply + <code>/duel</code> — адресный.

2️⃣ <b>Таймер 15 секунд</b> на ответ: быстрее и верно — больше очков.

3️⃣ <b>Anti-abuse:</b> с одним соперником XP максимум за 3 дуэли в сутки в каждом чате. Рейтинг и уровень каждой группы независимы.

4️⃣ <b>Динамический XP:</b> победа над лидером чата даёт больше очков.

5️⃣ <b>Вопросы учителя — 2x XP.</b>

<b>Команды:</b>
/duel /top /members /quiz /stats /teacher /help`

	keyboard := model.NewKeyboard()
	if b.botID.Load() != 0 {
		b.addGameButton(keyboard, "⚔️ В бой!", fmt.Sprintf("chat-%d", groupContext(update)))
	}
	b.sendReply(ctx, update, text, keyboard)
}

func (b *Bot) addGameButton(keyboard *model.Keyboard, text, payload string) {
	if b.appURL.Load().(string) == "" {
		return
	}
	row := keyboard.AddRow().AddOpenApp(text, b.botID.Load())
	row.Build()[0].WebApp = b.appURL.Load().(string)
	row.Build()[0].Payload = payload
}

// Local development addresses cannot be opened by a MAX user or validated by MAX.
func publicButtonURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || !strings.Contains(host, ".") {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast()) {
		return ""
	}
	return u.String()
}

func botAppURL(username string) string {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	if strings.HasPrefix(username, "https://max.ru/") {
		return publicButtonURL(username)
	}
	if username == "" || strings.ContainsAny(username, "/:?#% \\&") {
		return ""
	}
	return "https://max.ru/" + url.PathEscape(username)
}

func (b *Bot) handleCallback(ctx context.Context, update model.Update) {
	if update.Callback == nil {
		return
	}
	payload := update.Callback.Payload
	fromPrivate := !isGroupUpdate(update) && b.storage.GetChat(update.ChatID) == nil
	answerCallback := func(text string) {
		if update.Callback.CallbackID != "" && b.API() != nil {
			var notification *string
			if text != "" {
				notification = &text
			}
			_, _ = b.API().Messages.AnswerOnCallback(ctx, update.Callback.CallbackID, model.CallbackAnswer{Notification: notification})
		}
	}
	duelDecision := strings.HasPrefix(payload, "cb_accept_") || strings.HasPrefix(payload, "cb_join_") || strings.HasPrefix(payload, "cb_decline_")
	if !duelDecision {
		answerCallback("")
	}
	if index := strings.LastIndex(payload, "_chat_"); index >= 0 {
		chatID, err := strconv.ParseInt(payload[index+6:], 10, 64)
		id, _, _, _ := getUpdateUser(update)
		if err != nil || chatID == 0 || !b.storage.IsChatMember(chatID, id) {
			if duelDecision {
				answerCallback("Вызов недоступен: вы не состоите в группе.")
			} else {
				b.sendReply(ctx, update, "Контекст чата недоступен. Отправьте /start в группе ещё раз.", nil)
			}
			return
		}
		payload = payload[:index]
		update.ChatID = chatID
		if update.Message == nil {
			update.Message = &model.MessageUpdate{}
		}
		if update.Message != nil {
			cp := *update.Message
			cp.Recipient.ChatType = model.ChatTypeChat
			update.Message = &cp
		}
	}
	clicker := b.touchFromUpdate(ctx, update)
	switch payload {
	case "cb_quick_duel":
		if fromPrivate {
			if isGroupUpdate(update) {
				b.chooseDuelOpponent(ctx, update)
			} else {
				b.chooseDuelChat(ctx, update)
			}
		} else {
			b.cmdDuel(ctx, update)
		}
	case "cb_duel_players":
		b.chooseDuelOpponent(ctx, update)
	case "cb_top":
		b.cmdTop(ctx, update)
	case "cb_teacher":
		b.cmdTeacher(ctx, update)
	case "cb_stats":
		b.cmdStats(ctx, update)
	case "cb_members":
		b.cmdMembers(ctx, update)
	case "cb_help":
		b.cmdHelp(ctx, update)
	case "cb_start_express_quiz":
		b.cmdQuiz(ctx, update)
	default:
		if strings.HasPrefix(payload, "cb_decline_") {
			err := b.storage.DeclineDuel(strings.TrimPrefix(payload, "cb_decline_"), update.ChatID, clicker.ID)
			text := "Приглашение отклонено."
			if err != nil {
				text = err.Error()
			}
			answerCallback(text)
			return
		}
		if strings.HasPrefix(payload, "cb_challenge_") {
			id, err := strconv.ParseInt(strings.TrimPrefix(payload, "cb_challenge_"), 10, 64)
			if err == nil && isGroupUpdate(update) {
				b.challengeOpponent(ctx, update, id)
			}
			return
		}
		id := ""
		if strings.HasPrefix(payload, "cb_join_") {
			id = strings.TrimPrefix(payload, "cb_join_")
		}
		if strings.HasPrefix(payload, "cb_accept_") {
			id = strings.TrimPrefix(payload, "cb_accept_")
		}
		if id == "" {
			return
		}
		before := b.storage.GetDuel(id)
		duel, err := b.storage.AcceptDuel(id, update.ChatID, clicker.ID)
		if err != nil {
			answerCallback(err.Error())
			return
		}
		answerCallback("Вызов принят.")
		if before != nil && before.Status != "waiting" {
			return
		}
		keyboard := model.NewKeyboard()
		if b.botID.Load() != 0 {
			b.addGameButton(keyboard, "⚔️ Войти в дуэль", duel.ID)
		}
		text := fmt.Sprintf("🔥 <b>Вызов принят!</b>\n%s vs %s\nОткройте Mini App и нажмите «Готов» перед первым раундом. Следующие раунды начнутся автоматически после загрузки экранов обоих игроков.", html.EscapeString(duel.Player1.FullName()), html.EscapeString(duel.Player2.FullName()))
		b.sendMessage(ctx, model.Update{ChatID: duel.ChatID}, text, keyboard)
	}
}
