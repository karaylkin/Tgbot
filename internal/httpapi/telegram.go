package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type TelegramUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

func ValidateInitData(initData string, botToken string) (TelegramUser, error) {
	if initData == "" {
		return TelegramUser{}, fmt.Errorf("initData пустой")
	}
	if botToken == "" {
		return TelegramUser{}, fmt.Errorf("bot token пустой")
	}

	// 1) Варианты предварительной обработки строки (для случаев, когда '+' превратили в пробел).
	preprocess := []func(string) string{
		func(s string) string { return s },
		func(s string) string {
			if strings.Contains(s, "+") {
				return strings.ReplaceAll(s, "+", "%2B")
			}
			return s
		},
	}

	// 2) Два допустимых варианта секрета (WebApp и legacy)
	secrets := [][]byte{
		secretWebApp(botToken),
		secretLegacy(botToken),
	}

	var lastErr error
	for _, pre := range preprocess {
		s := pre(initData)
		for _, sec := range secrets {
			user, err := validateWithSecret(s, sec)
			if err == nil {
				return user, nil
			}
			lastErr = err
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("initData validation failed")
	}
	return TelegramUser{}, lastErr
}

func secretWebApp(botToken string) []byte {
	// HMAC-SHA256("WebAppData", botToken) — официальный для WebApp.
	h := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = h.Write([]byte(botToken))
	return h.Sum(nil)
}

func secretLegacy(botToken string) []byte {
	// Legacy: sha256(botToken) как ключ (некоторые клиенты могут прислать так).
	sum := sha256.Sum256([]byte(botToken))
	return sum[:]
}

func allowUnverifiedInitData() bool {
	return strings.TrimSpace(strings.ToLower(os.Getenv("ALLOW_UNVERIFIED_INITDATA"))) == "1"
}

func validateWithSecret(initData string, secretKey []byte) (TelegramUser, error) {
	values, err := url.ParseQuery(initData)
	if err != nil {
		return TelegramUser{}, fmt.Errorf("ошибка парсинга initData: %w", err)
	}

	hash := values.Get("hash")
	if hash == "" {
		return TelegramUser{}, fmt.Errorf("нет hash в initData")
	}
	values.Del("hash")
	// signature используется для third-party валидации; убираем из строки проверки
	values.Del("signature")

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var parts []string
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}

	checkString := strings.Join(parts, "\n")

	mac := hmac.New(sha256.New, secretKey)
	_, _ = mac.Write([]byte(checkString))
	expected := mac.Sum(nil)

	hashBytes, err := hex.DecodeString(hash)
	if err != nil {
		return TelegramUser{}, fmt.Errorf("hash не hex")
	}
	if !hmac.Equal(expected, hashBytes) {
		if allowUnverifiedInitData() {
			// Продолжаем ниже — распарсим user и вернем, но залогируем предупреждение.
			log.Printf("WARN: initData подпись не совпадает, но ALLOW_UNVERIFIED_INITDATA=1 — пропускаю")
		} else {
			return TelegramUser{}, fmt.Errorf("initData подпись не совпадает")
		}
	}

	if authDate := values.Get("auth_date"); authDate != "" {
		ts, err := strconv.ParseInt(authDate, 10, 64)
		if err == nil {
			authTime := time.Unix(ts, 0)
			if time.Since(authTime) > 24*time.Hour {
				return TelegramUser{}, fmt.Errorf("initData просрочен")
			}
		}
	}

	userRaw := values.Get("user")
	if userRaw == "" {
		return TelegramUser{}, fmt.Errorf("нет user в initData")
	}

	var user TelegramUser
	if err := json.Unmarshal([]byte(userRaw), &user); err != nil {
		return TelegramUser{}, fmt.Errorf("ошибка парсинга user: %w", err)
	}

	if user.ID == 0 {
		return TelegramUser{}, fmt.Errorf("user.id пустой")
	}
	return user, nil
}
