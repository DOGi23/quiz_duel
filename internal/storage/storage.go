package storage

import (
	"sync"

	"first-max-bot/internal/models"
)

type Storage struct {
	mu              sync.RWMutex
	users           map[int64]*models.User
	chatProgress    map[int64]map[int64]*models.PlayerProgress
	legacyProgress  map[int64]*models.PlayerProgress
	chats           map[int64]*models.ChatInfo
	chatMembers     map[int64]map[int64]struct{} // chatID -> set of userIDs
	duels           map[string]*models.Duel
	categories      []models.Category
	chatCategories  map[int64][]models.Category
	gameSettings    map[int64]models.GameSettings
	questions       []models.Question
	questionChanges map[int64]map[string]questionChange
	reports         map[string]QuestionReport
	persistence     *persistentState
	expressQuiz     map[string]*models.ExpressQuiz
	topicStats      map[string]*topicCounter
}

type topicCounter struct {
	ChatID        int64
	Subject       string
	Topic         string
	TotalAttempts int
	ErrorsCount   int
}

func New() *Storage {
	s := &Storage{
		users:           make(map[int64]*models.User),
		chatProgress:    make(map[int64]map[int64]*models.PlayerProgress),
		legacyProgress:  make(map[int64]*models.PlayerProgress),
		chats:           make(map[int64]*models.ChatInfo),
		chatMembers:     make(map[int64]map[int64]struct{}),
		duels:           make(map[string]*models.Duel),
		chatCategories:  make(map[int64][]models.Category),
		gameSettings:    make(map[int64]models.GameSettings),
		questionChanges: make(map[int64]map[string]questionChange),
		reports:         make(map[string]QuestionReport),
		expressQuiz:     make(map[string]*models.ExpressQuiz),
		topicStats:      make(map[string]*topicCounter),
	}

	s.initCategories()
	s.initQuestions()

	return s
}

func (s *Storage) initCategories() {
	s.categories = []models.Category{
		{ID: "algebra", Name: "Алгебра", Icon: "📐", Color: "#8B5CF6", Description: "Уравнения, функции, матрицы и проценты"},
		{ID: "physics", Name: "Физика", Icon: "⚡", Color: "#EC4899", Description: "Механика, электричество, законы Ньютона и термодинамика"},
		{ID: "cs", Name: "Информатика", Icon: "💻", Color: "#3B82F6", Description: "Алгоритмы, сети, структуры данных и логика"},
		{ID: "history", Name: "История", Icon: "📜", Color: "#F59E0B", Description: "Великие эпохи, даты, правители и открытия"},
		{ID: "geography", Name: "География", Icon: "🌍", Color: "#10B981", Description: "Страны, столицы, рельеф и природные зоны"},
		{ID: "chemistry", Name: "Химия", Icon: "🧪", Color: "#06B6D4", Description: "Элементы, реакции, кислоты и химические законы"},
		{ID: "biology", Name: "Биология", Icon: "🧬", Color: "#84CC16", Description: "Клетки, экология, анатомия и генетика"},
	}
}

func (s *Storage) initQuestions() {
	s.questions = []models.Question{
		// АЛГЕБРА
		{
			ID:           "alg-1",
			Category:     "algebra",
			Topic:        "Квадратные уравнения",
			Text:         "Чему равен дискриминант уравнения x² - 6x + 8 = 0?",
			Options:      []string{"4", "16", "2", "0"},
			CorrectIndex: 0,
			Explanation:  "D = b² - 4ac = (-6)² - 4*1*8 = 36 - 32 = 4.",
			Multiplier:   1.0,
			Difficulty:   "medium",
		},
		{
			ID:           "alg-2",
			Category:     "algebra",
			Topic:        "Степени и корни",
			Text:         "Чему равно выражение 2⁴ · 2³?",
			Options:      []string{"128", "64", "256", "32"},
			CorrectIndex: 0,
			Explanation:  "При умножении степеней с одинаковым основанием показатели складываются: 2^(4+3) = 2^7 = 128.",
			Multiplier:   1.0,
			Difficulty:   "easy",
		},
		{
			ID:                "alg-3",
			Category:          "algebra",
			Topic:             "Тригонометрия",
			Text:              "Чему равен sin²(α) + cos²(α)?",
			Options:           []string{"1", "0", "2", "tg(α)"},
			CorrectIndex:      0,
			Explanation:       "Основное тригонометрическое тождество: sin²(α) + cos²(α) = 1.",
			IsTeacherQuestion: true,
			TeacherName:       "Учитель",
			Multiplier:        2.0,
			Difficulty:        "medium",
		},
		{
			ID:           "alg-4",
			Category:     "algebra",
			Topic:        "Проценты",
			Text:         "Товар стоил 2000 руб. Цену снизили на 15%. Какова новая цена?",
			Options:      []string{"1700 руб.", "1800 руб.", "1650 руб.", "1750 руб."},
			CorrectIndex: 0,
			Explanation:  "15% от 2000 = 300 руб. 2000 - 300 = 1700 руб.",
			Multiplier:   1.0,
			Difficulty:   "easy",
		},

		// ФИЗИКА
		{
			ID:           "phys-1",
			Category:     "physics",
			Topic:        "Законы Ньютона",
			Text:         "Какой закон Ньютона формулируется формулой F = ma?",
			Options:      []string{"Второй закон Ньютона", "Первый закон Ньютона", "Третий закон Ньютона", "Закон всемирного тяготения"},
			CorrectIndex: 0,
			Explanation:  "Второй закон Ньютона связывает силу, массу и ускорение: F = m * a.",
			Multiplier:   1.0,
			Difficulty:   "easy",
		},
		{
			ID:                "phys-2",
			Category:          "physics",
			Topic:             "Закон Ома",
			Text:              "Как формулируется закон Ома для участка цепи?",
			Options:           []string{"I = U / R", "U = I / R", "I = R / U", "P = U * I"},
			CorrectIndex:      0,
			Explanation:       "Сила тока прямо пропорциональна напряжению и обратно пропорциональна сопротивлению: I = U/R.",
			IsTeacherQuestion: true,
			TeacherName:       "Учитель",
			Multiplier:        2.0,
			Difficulty:        "medium",
		},
		{
			ID:           "phys-3",
			Category:     "physics",
			Topic:        "Оптика",
			Text:         "Какова скорость света в вакууме приблизительно?",
			Options:      []string{"300 000 км/с", "150 000 км/с", "30 000 км/с", "3 000 000 км/с"},
			CorrectIndex: 0,
			Explanation:  "Скорость света в вакууме составляет примерно 299 792 км/с ≈ 300 000 км/с.",
			Multiplier:   1.0,
			Difficulty:   "easy",
		},

		// ИНФОРМАТИКА
		{
			ID:           "cs-1",
			Category:     "cs",
			Topic:        "Системы счисления",
			Text:         "Сколько байт в одном килобайте (Кб) по стандарту двоичных приставок?",
			Options:      []string{"1024 байта", "1000 байт", "512 байт", "2048 байт"},
			CorrectIndex: 0,
			Explanation:  "1 Кбайт = 2^10 байт = 1024 байта.",
			Multiplier:   1.0,
			Difficulty:   "easy",
		},
		{
			ID:                "cs-2",
			Category:          "cs",
			Topic:             "Алгоритмы и структуры",
			Text:              "Какова временная сложность бинарного поиска в отсортированном массиве?",
			Options:           []string{"O(log n)", "O(n)", "O(n²)", "O(1)"},
			CorrectIndex:      0,
			Explanation:       "Бинарный поиск на каждом шаге делит массив пополам, поэтому его сложность O(log n).",
			IsTeacherQuestion: true,
			TeacherName:       "Учитель",
			Multiplier:        2.0,
			Difficulty:        "medium",
		},
		{
			ID:           "cs-3",
			Category:     "cs",
			Topic:        "Сети и протоколы",
			Text:         "Какой протокол отвечает за защищенную передачу гипертекста в веб?",
			Options:      []string{"HTTPS", "HTTP", "FTP", "SSH"},
			CorrectIndex: 0,
			Explanation:  "HTTPS использует шифрование SSL/TLS для безопасной передачи данных.",
			Multiplier:   1.0,
			Difficulty:   "easy",
		},

		// ИСТОРИЯ
		{
			ID:           "hist-1",
			Category:     "history",
			Topic:        "История России",
			Text:         "В каком году произошла Куликовская битва?",
			Options:      []string{"1380 год", "1242 год", "1480 год", "1612 год"},
			CorrectIndex: 0,
			Explanation:  "Куликовская битва состоялась 8 сентября 1380 года на Куликовом поле.",
			Multiplier:   1.0,
			Difficulty:   "medium",
		},
		{
			ID:                "hist-2",
			Category:          "history",
			Topic:             "Эпоха Петра I",
			Text:              "В каком году был основан Санкт-Петербург?",
			Options:           []string{"1703 год", "1700 год", "1721 год", "1699 год"},
			CorrectIndex:      0,
			Explanation:       "Санкт-Петербург был заложен Петром I 27 мая 1703 года.",
			IsTeacherQuestion: true,
			TeacherName:       "Учитель",
			Multiplier:        2.0,
			Difficulty:        "easy",
		},

		// ГЕОГРАФИЯ
		{
			ID:           "geo-1",
			Category:     "geography",
			Topic:        "Озера и реки",
			Text:         "Какое озеро является самым глубоким на планете Земля?",
			Options:      []string{"Байкал", "Каспийское море", "Виктория", "Верхнее"},
			CorrectIndex: 0,
			Explanation:  "Озеро Байкал в России — самое глубокое озеро мира (максимальная глубина 1642 м).",
			Multiplier:   1.0,
			Difficulty:   "easy",
		},
		{
			ID:           "geo-2",
			Category:     "geography",
			Topic:        "Пермский край",
			Text:         "Какая река является главной водной артерией Пермского края?",
			Options:      []string{"Кама", "Чусовая", "Вишера", "Сылва"},
			CorrectIndex: 0,
			Explanation:  "Река Кама — крупнейшая река Западного Урала и главная водная артерия Пермского края.",
			Multiplier:   1.0,
			Difficulty:   "easy",
		},

		// ХИМИЯ
		{
			ID:           "chem-1",
			Category:     "chemistry",
			Topic:        "Таблица Менделеева",
			Text:         "Какой химический элемент обозначается символом Au?",
			Options:      []string{"Золото", "Серебро", "Медь", "Алюминий"},
			CorrectIndex: 0,
			Explanation:  "Au (Aurum) — химический символ золота с атомным номером 79.",
			Multiplier:   1.0,
			Difficulty:   "easy",
		},
		{
			ID:                "chem-2",
			Category:          "chemistry",
			Topic:             "Формулы кислот",
			Text:              "Какова химическая формула серной кислоты?",
			Options:           []string{"H₂SO₄", "HCl", "HNO₃", "H₂CO₃"},
			CorrectIndex:      0,
			Explanation:       "Серная кислота имеет молекулярную формулу H₂SO₄.",
			IsTeacherQuestion: true,
			TeacherName:       "Учитель",
			Multiplier:        2.0,
			Difficulty:        "medium",
		},

		// БИОЛОГИЯ
		{
			ID:           "bio-1",
			Category:     "biology",
			Topic:        "Цитология",
			Text:         "Какая клеточная органелла называется 'энергетической станцией клетки'?",
			Options:      []string{"Митохондрия", "Рибосома", "Аппарат Гольджи", "Лизосома"},
			CorrectIndex: 0,
			Explanation:  "Митохондрии синтезируют АТФ, обеспечивая клетку необходимой энергией.",
			Multiplier:   1.0,
			Difficulty:   "easy",
		},
	}

	// Счётчики тем заполняются только реальными ответами из чатов
}
