package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestValidateInitDataOK(t *testing.T) {
	token := "123456:ABCDEF"
	user := TelegramUser{ID: 42, Username: "reader"}

	initData := buildSignedInitData(t, token, user, time.Now())

	got, err := ValidateInitData(initData, token)
	if err != nil {
		t.Fatalf("expected valid initData, got error: %v", err)
	}
	if got.ID != user.ID || got.Username != user.Username {
		t.Fatalf("unexpected user: %+v", got)
	}
}

func TestValidateInitDataInvalidHash(t *testing.T) {
	token := "123456:ABCDEF"
	user := TelegramUser{ID: 7, Username: "bad"}

	initData := buildSignedInitData(t, token, user, time.Now())
	values, _ := url.ParseQuery(initData)
	values.Set("hash", "deadbeef")

	if _, err := ValidateInitData(values.Encode(), token); err == nil {
		t.Fatal("expected hash mismatch error")
	}
}

func TestValidateInitDataExpired(t *testing.T) {
	token := "123456:ABCDEF"
	user := TelegramUser{ID: 99}

	past := time.Now().Add(-25 * time.Hour)
	initData := buildSignedInitData(t, token, user, past)

	if _, err := ValidateInitData(initData, token); err == nil {
		t.Fatal("expected expiration error")
	}
}

func TestValidateInitDataPlusFallback(t *testing.T) {
	token := "123456:ABCDEF"
	user := TelegramUser{ID: 77, Username: "plusman"}

	// Собираем валидную строку, где одно из полей содержит символ '+'.
	initData := buildSignedInitDataWithValue(t, token, user, time.Now(), map[string]string{
		"query_id": "AAE+AAE",
	})

	// Разбиваем ее: заменим %2B обратно на '+' — это имитирует ситуацию, когда плюсы превратились в пробелы при первом парсинге.
	broken := strings.ReplaceAll(initData, "%2B", "+")

	if _, err := ValidateInitData(broken, token); err != nil {
		t.Fatalf("fallback should recover from '+': %v", err)
	}
}

func buildSignedInitData(t *testing.T, token string, user TelegramUser, ts time.Time) string {
	t.Helper()

	values := url.Values{}
	values.Set("user", mustJSON(t, user))
	values.Set("auth_date", fmt.Sprint(ts.Unix()))
	values.Set("query_id", "AAEAAAE")

	dataCheck := dataCheckString(values)
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secret.Write([]byte(token))
	secretKey := secret.Sum(nil)

	mac := hmac.New(sha256.New, secretKey)
	_, _ = mac.Write([]byte(dataCheck))
	hash := mac.Sum(nil)

	values.Set("hash", hex.EncodeToString(hash))
	return values.Encode()
}

func dataCheckString(values url.Values) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+values.Get(k))
	}
	return strings.Join(parts, "\n")
}

func buildSignedInitDataWithValue(t *testing.T, token string, user TelegramUser, ts time.Time, extra map[string]string) string {
	t.Helper()

	values := url.Values{}
	values.Set("user", mustJSON(t, user))
	values.Set("auth_date", fmt.Sprint(ts.Unix()))
	values.Set("query_id", "AAEAAAE")
	for k, v := range extra {
		values.Set(k, v)
	}

	dataCheck := dataCheckString(values)
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secret.Write([]byte(token))
	secretKey := secret.Sum(nil)

	mac := hmac.New(sha256.New, secretKey)
	_, _ = mac.Write([]byte(dataCheck))
	hash := mac.Sum(nil)

	values.Set("hash", hex.EncodeToString(hash))
	return values.Encode()
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	return string(b)
}
