package telegram

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"tor_project/internal/db"
	"tor_project/internal/models"
	"tor_project/internal/service"
	"tor_project/internal/storage"
)

type Bot struct {
	bot        *tgbotapi.BotAPI
	service    *service.FlibustaClient
	store      *db.Store
	storageDir string
	miniAppURL string
	sessions   map[int64]*searchSession
	sessionsMu sync.Mutex
}

type searchSession struct {
	books    []models.Book
	page     int
	pageSize int
}

func NewBot(token string, svc *service.FlibustaClient, store *db.Store, storageDir string, miniAppURL string) (*Bot, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	bot.Debug = false
	log.Printf("Авторизован как %s", bot.Self.UserName)

	return &Bot{
		bot:        bot,
		service:    svc,
		store:      store,
		storageDir: storageDir,
		miniAppURL: miniAppURL,
		sessions:   make(map[int64]*searchSession),
	}, nil
}

const (
	defaultPageSize  = 10
	cbBookPrefix     = "book:"
	cbPagePrefix     = "page:"
	cbDownloadPrefix = "dl:"
)

var (
	sizeInParensRe = regexp.MustCompile(`\(([^)]+)\)\s*$`)
	// Best-effort size pattern for cases where the site doesn't use parentheses.
	sizeLooseRe = regexp.MustCompile(`(?i)\b\d+(?:[.,]\d+)?\s*(?:kb|mb|gb|kib|mib|gib|кб|мб|гб)\b`)
)

// Start — главный цикл
func (b *Bot) Start() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.bot.GetUpdatesChan(u)

	for update := range updates {
		// 1. Текстовое сообщение (Поиск)
		if update.Message != nil {
			b.handleMessage(update.Message)
		}

		// 2. Нажатие на кнопку (Скачивание)
		if update.CallbackQuery != nil {
			b.handleCallback(update.CallbackQuery)
		}
	}
}

// handleMessage — Обработка текста (ПОИСК)
func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	if msg.IsCommand() && msg.Command() == "start" {
		b.sendMessage(msg.Chat.ID, "Привет! Напиши название книги, я найду её)")
		return
	}

	query := msg.Text
	chatID := msg.Chat.ID

	b.sendMessage(chatID, "🔎 Ищу: "+query+"...")

	// Вызов сервиса поиска
	books, err := b.service.Search(query)
	if err != nil {
		b.sendMessage(chatID, "❌ Ошибка поиска (возможно, Tor устал).")
		log.Printf("Error searching: %v", err)
		return
	}

	if len(books) == 0 {
		b.sendMessage(chatID, "😔 Ничего не найдено.")
		return
	}

	// Сохраняем результаты и отправляем первую страницу
	b.storeSession(chatID, books)
	b.sendBooksPage(chatID, 0)
}

func (b *Bot) storeSession(chatID int64, books []models.Book) {
	b.sessionsMu.Lock()
	defer b.sessionsMu.Unlock()

	b.sessions[chatID] = &searchSession{
		books:    books,
		page:     0,
		pageSize: defaultPageSize,
	}
}

func (b *Bot) getSession(chatID int64) (*searchSession, bool) {
	b.sessionsMu.Lock()
	defer b.sessionsMu.Unlock()

	session, ok := b.sessions[chatID]
	return session, ok
}

func clampPage(page, totalPages int) int {
	if totalPages <= 0 {
		return 0
	}
	if page < 0 {
		return 0
	}
	if page >= totalPages {
		return totalPages - 1
	}
	return page
}

func totalPages(total, pageSize int) int {
	if total == 0 {
		return 0
	}
	return (total + pageSize - 1) / pageSize
}

func (b *Bot) findBookInSession(chatID int64, bookID string) (models.Book, bool) {
	session, ok := b.getSession(chatID)
	if !ok {
		return models.Book{}, false
	}

	for _, book := range session.books {
		if book.ID == bookID {
			return book, true
		}
	}

	return models.Book{}, false
}

func (b *Bot) buildPage(chatID int64, page int) (string, tgbotapi.InlineKeyboardMarkup, bool) {
	session, ok := b.getSession(chatID)
	if !ok || len(session.books) == 0 {
		return "", tgbotapi.InlineKeyboardMarkup{}, false
	}

	total := len(session.books)
	pages := totalPages(total, session.pageSize)
	page = clampPage(page, pages)

	start := page * session.pageSize
	end := start + session.pageSize
	if end > total {
		end = total
	}

	var rows [][]tgbotapi.InlineKeyboardButton

	for _, book := range session.books[start:end] {
		text := fmt.Sprintf("%s - %s", book.Title, book.Author)
		data := cbBookPrefix + book.ID
		btn := tgbotapi.NewInlineKeyboardButtonData(text, data)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{btn})
	}

	// Навигационная строка (если страниц больше одной)
	if pages > 1 {
		var navRow []tgbotapi.InlineKeyboardButton
		if page > 0 {
			prev := tgbotapi.NewInlineKeyboardButtonData("⬅️", fmt.Sprintf("%s%d", cbPagePrefix, page-1))
			navRow = append(navRow, prev)
		}

		center := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("• %d/%d •", page+1, pages),
			fmt.Sprintf("%s%d", cbPagePrefix, page),
		)
		navRow = append(navRow, center)

		if page < pages-1 {
			next := tgbotapi.NewInlineKeyboardButtonData("➡️", fmt.Sprintf("%s%d", cbPagePrefix, page+1))
			navRow = append(navRow, next)
		}

		rows = append(rows, navRow)
	}

	// Обновляем текущую страницу в сессии
	b.sessionsMu.Lock()
	if session, ok := b.sessions[chatID]; ok {
		session.page = page
	}
	b.sessionsMu.Unlock()

	text := fmt.Sprintf("📚 Найдено книг: %d\nСтраница %d/%d", total, page+1, pages)
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return text, markup, true
}

func (b *Bot) sendBooksPage(chatID int64, page int) {
	text, markup, ok := b.buildPage(chatID, page)
	if !ok {
		b.sendMessage(chatID, "⚠️ Результаты поиска устарели. Напиши запрос ещё раз.")
		return
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = markup
	b.bot.Send(msg)
}

func (b *Bot) editBooksPage(chatID int64, messageID int, page int) {
	text, markup, ok := b.buildPage(chatID, page)
	if !ok {
		b.sendMessage(chatID, "⚠️ Результаты поиска устарели. Напиши запрос ещё раз.")
		return
	}

	editText := tgbotapi.NewEditMessageText(chatID, messageID, text)
	editText.ReplyMarkup = &markup
	if _, err := b.bot.Send(editText); err != nil {
		log.Printf("Edit message error: %v", err)
	}
}

func formatButtonText(opt models.BookFormatOption) string {
	format := strings.ToUpper(strings.TrimSpace(opt.Path))
	if format == "" {
		format = "FILE"
	}

	label := strings.TrimSpace(opt.Label)
	if label == "" {
		return format
	}

	if m := sizeInParensRe.FindStringSubmatch(label); len(m) == 2 {
		inParens := strings.TrimSpace(m[1])
		// The site often renders labels like "EPUB (epub)" – we don't want to show the duplicated format.
		if strings.EqualFold(inParens, strings.TrimSpace(opt.Path)) {
			return format
		}

		return fmt.Sprintf("%s (%s)", format, inParens)
	}

	if m := sizeLooseRe.FindStringSubmatch(label); len(m) == 1 {
		return fmt.Sprintf("%s (%s)", format, strings.TrimSpace(m[0]))
	}

	return format
}

func (b *Bot) sendBookDetails(chatID int64, bookID string) {
	details, err := b.service.GetBookDetails(bookID)
	if err != nil {
		b.sendMessage(chatID, "❌ Не удалось получить информацию о книге (Tor/сайт может тупить).")
		log.Printf("GetBookDetails error: %v", err)
		return
	}

	// Prefer title/author from the search session to avoid parsing mistakes from HTML.
	if book, ok := b.findBookInSession(chatID, bookID); ok {
		details.Title = book.Title
		details.Author = book.Author
	}

	if details.Title == "" {
		details.Title = "Без названия"
	}
	if details.Author == "" {
		details.Author = "Автор неизвестен"
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	if len(details.Formats) == 0 {
		// If parsing fails, offer a small common set as a fallback.
		common := []string{"epub", "fb2", "pdf"}
		var row []tgbotapi.InlineKeyboardButton
		for _, f := range common {
			btn := tgbotapi.NewInlineKeyboardButtonData(strings.ToUpper(f), cbDownloadPrefix+bookID+":"+f)
			row = append(row, btn)
		}
		rows = append(rows, row)
	} else {
		// Sort formats for stable UI.
		sort.Slice(details.Formats, func(i, j int) bool {
			ai := strings.ToUpper(strings.TrimSpace(details.Formats[i].Path))
			aj := strings.ToUpper(strings.TrimSpace(details.Formats[j].Path))
			return ai < aj
		})

		// Render 2 buttons per row for compact UI.
		var row []tgbotapi.InlineKeyboardButton
		for _, opt := range details.Formats {
			// Always the same style: "FORMAT" or "FORMAT (size)".
			text := formatButtonText(opt)
			data := cbDownloadPrefix + bookID + ":" + opt.Path
			row = append(row, tgbotapi.NewInlineKeyboardButtonData(text, data))
			if len(row) == 2 {
				rows = append(rows, row)
				row = nil
			}
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
	}

	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	caption := fmt.Sprintf("📖 %s\n✍️ %s", details.Title, details.Author)

	// If we have a cover URL, download it via Tor and upload as bytes (Telegram can't fetch .onion URLs).
	if details.CoverPath != "" {
		coverBytes, err := b.service.DownloadBytes(details.CoverPath)
		if err != nil {
			log.Printf("Cover download error: %v", err)
		} else if len(coverBytes) > 0 {
			photo := tgbotapi.FileBytes{Name: "cover.jpg", Bytes: coverBytes}
			photoMsg := tgbotapi.NewPhoto(chatID, photo)
			photoMsg.Caption = caption
			photoMsg.ReplyMarkup = markup
			b.bot.Send(photoMsg)
			return
		}
	}

	msg := tgbotapi.NewMessage(chatID, caption)
	msg.ReplyMarkup = markup
	b.bot.Send(msg)
}

// handleCallback — Обработка нажатия на кнопку (СКАЧИВАНИЕ)
func (b *Bot) handleCallback(cb *tgbotapi.CallbackQuery) {
	if cb.Message == nil {
		return
	}

	chatID := cb.Message.Chat.ID
	data := cb.Data

	// Пагинация
	if strings.HasPrefix(data, cbPagePrefix) {
		callbackResp := tgbotapi.NewCallback(cb.ID, "Листаю…")
		b.bot.Request(callbackResp)

		pageStr := strings.TrimPrefix(data, cbPagePrefix)
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			b.sendMessage(chatID, "⚠️ Не удалось переключить страницу.")
			log.Printf("Invalid page callback data: %q", data)
			return
		}

		b.editBooksPage(chatID, cb.Message.MessageID, page)
		return
	}

	// Выбор формата (скачивание)
	if strings.HasPrefix(data, cbDownloadPrefix) {
		rest := strings.TrimPrefix(data, cbDownloadPrefix)
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			b.sendMessage(chatID, "⚠️ Не удалось распознать формат.")
			log.Printf("Invalid download callback data: %q", data)
			return
		}

		bookID := parts[0]
		formatPath := parts[1]

		callbackResp := tgbotapi.NewCallback(cb.ID, "Начинаю скачивание... ⏳")
		b.bot.Request(callbackResp)

		userID := cb.From.ID
		username := cb.From.UserName
		b.downloadAndSend(chatID, userID, username, bookID, formatPath)
		return
	}

	// Выбор книги: показываем карточку (обложка + форматы)
	if strings.HasPrefix(data, cbBookPrefix) {
		callbackResp := tgbotapi.NewCallback(cb.ID, "Открываю…")
		b.bot.Request(callbackResp)

		bookID := strings.TrimPrefix(data, cbBookPrefix)
		b.sendBookDetails(chatID, bookID)
		return
	}

	// Backward compatibility: old callbacks might contain just the numeric bookID.
	if data != "" {
		callbackResp := tgbotapi.NewCallback(cb.ID, "Открываю…")
		b.bot.Request(callbackResp)

		b.sendBookDetails(chatID, data)
		return
	}
}

func (b *Bot) downloadAndSend(chatID int64, userID int64, username string, bookID string, formatPath string) {
	// Отправляем сообщение, чтобы юзер видел прогресс
	loadingMsg, errLoading := b.bot.Send(tgbotapi.NewMessage(chatID, "⏳ Скачиваю файл... Подождите..."))

	// Отправляем сообщение, чтобы юзер видел прогресс
	// Вспомогательная функция для удаления сообщения о загрузке
	deleteLoadingMsg := func() {
		if errLoading == nil && loadingMsg.MessageID != 0 {
			delMsg := tgbotapi.NewDeleteMessage(chatID, loadingMsg.MessageID)
			b.bot.Send(delMsg)
		}
	}

	// 2. Качаем файл (получаем поток stream)
	stream, filename, err := b.service.Download(bookID, formatPath)
	if err != nil {
		// Удаляем сообщение о загрузке при ошибке
		deleteLoadingMsg()
		b.sendMessage(chatID, "❌ Не удалось скачать файл. Возможно, ссылка устарела или Tor тупит.")
		log.Printf("Download error: %v", err)
		return
	}
	// Обязательно закрываем поток после чтения!
	defer stream.Close()

	// 3. Сохраняем файл на диск (Telegram лимит ~50MB)
	const maxFileSize = 50 * 1024 * 1024 // 50MB
	saved, err := storage.SaveBookFile(b.storageDir, filename, stream, maxFileSize)
	if err != nil {
		deleteLoadingMsg()
		if strings.Contains(err.Error(), "слишком большой") {
			b.sendMessage(chatID, "❌ Файл слишком большой. Максимальный размер: 50 MB.")
		} else {
			b.sendMessage(chatID, "❌ Ошибка при сохранении файла.")
		}
		log.Printf("Save file error: %v", err)
		return
	}

	fullPath := filepath.Join(b.storageDir, saved.RelativePath)
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		absPath = fullPath
	}

	// 4. Сохраняем метаданные в БД
	if b.store != nil {
		ctx := context.Background()
		if err := b.store.EnsureUser(ctx, userID, username); err != nil {
			log.Printf("EnsureUser error: %v", err)
		} else {
			title := ""
			author := ""
			if book, ok := b.findBookInSession(chatID, bookID); ok {
				title = book.Title
				author = book.Author
			}
			bookDBID, err := b.store.UpsertBook(ctx, bookID, title, author)
			if err != nil {
				log.Printf("UpsertBook error: %v", err)
			} else {
				fileID, err := b.store.InsertBookFile(ctx, bookDBID, formatPath, saved.RelativePath, saved.SizeBytes)
				if err != nil {
					log.Printf("InsertBookFile error: %v", err)
				} else if err := b.store.AddToLibrary(ctx, userID, fileID); err != nil {
					log.Printf("AddToLibrary error: %v", err)
				}
			}
		}
	}

	// Создаем документ
	docMsg := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(absPath))
	docMsg.Caption = "📖 Ваша книга. Приятного чтения!"

	if b.miniAppURL != "" {
		type webAppInfo struct {
			URL string `json:"url"`
		}
		type inlineKeyboardButton struct {
			Text   string      `json:"text"`
			WebApp *webAppInfo `json:"web_app,omitempty"`
		}
		type inlineKeyboardMarkup struct {
			InlineKeyboard [][]inlineKeyboardButton `json:"inline_keyboard"`
		}

		docMsg.ReplyMarkup = inlineKeyboardMarkup{
			InlineKeyboard: [][]inlineKeyboardButton{
				{
					{Text: "Читать онлайн", WebApp: &webAppInfo{URL: b.miniAppURL}},
				},
			},
		}
	}

	// 5. Отправляем файл
	if _, err := b.bot.Send(docMsg); err != nil {
		// Удаляем сообщение о загрузке при ошибке
		deleteLoadingMsg()
		b.sendMessage(chatID, fmt.Sprintf("❌ Ошибка при отправке файла в Telegram: %v", err))
		log.Printf("Send file error: %v", err)
	} else {
		// Если все ок — удаляем сообщение "Скачиваю..."
		deleteLoadingMsg()
	}
}

// sendMessage — хелпер для отправки текста
func (b *Bot) sendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	b.bot.Send(msg)
}
