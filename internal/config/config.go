package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

// Config — структура, хранящая все настройки приложения.
// Используем её, чтобы передавать параметры одной "пачкой".
type Config struct {
	TorProxyAddr  string
	FlibustaURL   string
	TelegramToken string
	SQLitePath    string
	StorageDir    string
	HTTPAddr      string
	MiniAppURL    string
}

// Load считывает .env файл и заполняет структуру Config.
func Load() (*Config, error) {
	// 1. Загружаем переменные из .env в память программы (переменные окружения OS)
	// Если файла нет, ничего страшного (вдруг мы запустили в Docker и передали env напрямую),
	// но мы выведем предупреждение.
	if err := godotenv.Load(); err != nil {
		fmt.Println("Инфо: файл .env не найден, ищем переменные в окружении OS")
	}

	// 2. Читаем переменные
	proxy := os.Getenv("TOR_PROXY")
	url := os.Getenv("FLIBUSTA_URL")
	token := os.Getenv("TELEGRAM_TOKEN")
	sqlitePath := os.Getenv("SQLITE_PATH")
	storageDir := os.Getenv("STORAGE_DIR")
	httpAddr := os.Getenv("HTTP_ADDR")
	miniAppURL := os.Getenv("MINIAPP_URL")

	// 3. Валидация (проверяем, что настройки не пустые)
	if proxy == "" {
		return nil, fmt.Errorf("переменная TOR_PROXY не задана")
	}
	if url == "" {
		return nil, fmt.Errorf("переменная FLIBUSTA_URL не задана")
	}
	if token == "" {
		return nil, fmt.Errorf("переменная TELEGRAM_TOKEN не задана")
	}

	// 4. Возвращаем готовый конфиг
	return &Config{
		TorProxyAddr:  proxy,
		FlibustaURL:   url,
		TelegramToken: token,
		SQLitePath:    resolvePath(withDefault(sqlitePath, "data/app.db")),
		StorageDir:    resolvePath(withDefault(storageDir, "storage/books")),
		HTTPAddr:      withDefault(httpAddr, ":8080"),
		MiniAppURL:    miniAppURL,
	}, nil
}

func withDefault(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func resolvePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	if filepath.IsAbs(p) {
		return p
	}

	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		return filepath.Clean(filepath.Join(base, p))
	}

	if cwd, err := os.Getwd(); err == nil {
		return filepath.Clean(filepath.Join(cwd, p))
	}

	return p
}
