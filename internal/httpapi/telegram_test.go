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

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	return string(b)
}
