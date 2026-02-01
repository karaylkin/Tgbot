package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TelegramUser описывает структуру пользователя, приходящую в initData
type TelegramUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Language  string `json:"language_code"`
	IsPremium bool   `json:"is_premium"`
}

// ValidateInitData проверяет подпись initData и возвращает данные пользователя.
// botToken - токен вашего бота.
// initData - сырая строка, полученная от Telegram.
func ValidateInitData(initData string, botToken string) (TelegramUser, error) {
	if initData == "" {
		return TelegramUser{}, fmt.Errorf("initData is empty")
	}
	if botToken == "" {
		return TelegramUser{}, fmt.Errorf("botToken is empty")
	}

	// 1. Подготовка вариантов строки (на случай проблем с кодировкой при передаче)
	// Часто веб-серверы заменяют "+" на пробел в URL-encoded строках.
	inputs := []string{
		initData,                                   // Оригинал
		strings.ReplaceAll(initData, " ", "+"),     // Исправление пробелов
		strings.ReplaceAll(initData, "%20", "+"),   // Исправление %20
	}

	// 2. Подготовка вариантов ключей (WebApp стандартный и Legacy)
	secrets := []struct {
		name string
		key  []byte
	}{
		{"WebApp", generateSecretWebApp(botToken)},
		{"Legacy", generateSecretLegacy(botToken)},
	}

	var lastErr error

	// Перебор всех комбинаций (обычно срабатывает первая же)
	for _, input := range inputs {
		for _, secret := range secrets {
			user, err := verifyAndParse(input, secret.key)
			if err == nil {
				return user, nil
			}
			lastErr = err
		}
	}

	// Если ничего не подошло, логируем детали для отладки
	if isDebug() {
		log.Printf("[DEBUG] Validation failed. Token starts with: %s...", trunc(botToken, 10))
		log.Printf("[DEBUG] InitData (len=%d): %s", len(initData), initData)
		log.Printf("[DEBUG] Last Error: %v", lastErr)
	}

	// Если разрешено пропускать проверку (для локальной разработки)
	if allowUnverifiedInitData() {
		log.Println("[WARN] initData signature mismatch, but ALLOW_UNVERIFIED_INITDATA=1. Parsing anyway.")
		// Парсим без проверки подписи
		return parseUserOnly(initData)
	}

	return TelegramUser{}, fmt.Errorf("validation failed: %v", lastErr)
}

// verifyAndParse выполняет проверку хеша и парсинг
func verifyAndParse(initData string, secretKey []byte) (TelegramUser, error) {
	// 1. Парсинг параметров
	values, err := url.ParseQuery(initData)
	if err != nil {
		return TelegramUser{}, fmt.Errorf("parse query error: %w", err)
	}

	// 2. Извлечение хеша
	receivedHash := values.Get("hash")
	if receivedHash == "" {
		return TelegramUser{}, fmt.Errorf("hash is missing")
	}

	// 3. Формирование data-check-string
	// Удаляем hash, сортируем ключи по алфавиту
	values.Del("hash")
	var keys []string
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, values.Get(k)))
	}
	checkString := strings.Join(parts, "\n")

	// 4. Вычисление HMAC-SHA256
	h := hmac.New(sha256.New, secretKey)
	h.Write([]byte(checkString))
	calculatedHash := hex.EncodeToString(h.Sum(nil))

	// 5. Сравнение хешей
	if !hmac.Equal([]byte(calculatedHash), []byte(receivedHash)) {
		// Для дебага можно раскомментировать
		// fmt.Printf("DEBUG: Mismatch.\nCheckString:\n%s\nCalc: %s\nRecv: %s\n", checkString, calculatedHash, receivedHash)
		return TelegramUser{}, fmt.Errorf("signature mismatch")
	}

	// 6. Проверка времени (auth_date)
	authDateStr := values.Get("auth_date")
	if authDateStr != "" {
		authTs, err := strconv.ParseInt(authDateStr, 10, 64)
		if err == nil {
			authTime := time.Unix(authTs, 0)
			now := time.Now()

			// Если дата старше 24 часов
			if now.Sub(authTime) > 24*time.Hour {
				return TelegramUser{}, fmt.Errorf("initData expired (older than 24h)")
			}
			// Если дата "из будущего" больше чем на 5 минут (рассинхрон часов)
			if authTime.Sub(now) > 5*time.Minute {
				return TelegramUser{}, fmt.Errorf("initData is from future (check server time)")
			}
		}
	}

	// 7. Извлечение пользователя
	return parseUserFromJSON(values.Get("user"))
}

// parseUserOnly парсит пользователя без проверки подписи (для режима отладки)
func parseUserOnly(initData string) (TelegramUser, error) {
	values, err := url.ParseQuery(initData)
	if err != nil {
		return TelegramUser{}, err
	}
	return parseUserFromJSON(values.Get("user"))
}

func parseUserFromJSON(userJson string) (TelegramUser, error) {
	if userJson == "" {
		return TelegramUser{}, fmt.Errorf("user field is empty")
	}
	var user TelegramUser
	if err := json.Unmarshal([]byte(userJson), &user); err != nil {
		return TelegramUser{}, fmt.Errorf("failed to unmarshal user: %w", err)
	}
	if user.ID == 0 {
		return TelegramUser{}, fmt.Errorf("user_id is 0")
	}
	return user, nil
}

// --- Вспомогательные функции ---

func generateSecretWebApp(token string) []byte {
	h := hmac.New(sha256.New, []byte("WebAppData"))
	h.Write([]byte(token))
	return h.Sum(nil)
}

func generateSecretLegacy(token string) []byte {
	h := sha256.New()
	h.Write([]byte(token))
	return h.Sum(nil)
}

func allowUnverifiedInitData() bool {
	val := os.Getenv("ALLOW_UNVERIFIED_INITDATA")
	return strings.TrimSpace(strings.ToLower(val)) == "1"
}

func isDebug() bool {
	return os.Getenv("DEBUG_INITDATA") == "1"
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}