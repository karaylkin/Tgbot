package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

	values, err := url.ParseQuery(initData)
	if err != nil {
		return TelegramUser{}, fmt.Errorf("ошибка парсинга initData: %w", err)
	}

	hash := values.Get("hash")
	if hash == "" {
		return TelegramUser{}, fmt.Errorf("нет hash в initData")
	}
	values.Del("hash")
	// signature используется для third-party валидации; мы её не проверяем, но убираем из строки проверки
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
	// Правильный ключ: HMAC-SHA256(botToken) с константой "WebAppData" в роли ключа.
	// См. https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app
	secretHMAC := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secretHMAC.Write([]byte(botToken))
	secretKey := secretHMAC.Sum(nil)

	mac := hmac.New(sha256.New, secretKey)
	_, _ = mac.Write([]byte(checkString))
	expected := mac.Sum(nil)

	hashBytes, err := hex.DecodeString(hash)
	if err != nil {
		return TelegramUser{}, fmt.Errorf("hash не hex")
	}
	if !hmac.Equal(expected, hashBytes) {
		return TelegramUser{}, fmt.Errorf("initData подпись не совпадает")
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
