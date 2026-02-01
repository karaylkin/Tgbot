package service

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"tor_project/internal/models"
	"tor_project/internal/parser"
)

type FlibustaClient struct {
	httpClient *http.Client
	baseURL    string
}

func NewFlibustaClient(client *http.Client, baseURL string) *FlibustaClient {
	return &FlibustaClient{
		httpClient: client,
		baseURL:    baseURL,
	}
}

// Search ищет книги (код из предыдущего этапа)
func (s *FlibustaClient) Search(query string) ([]models.Book, error) {
	const maxAttempts = 3

	// Подготовка запроса
	safeQuery := url.QueryEscape(query)
	targetURL := fmt.Sprintf("%s/booksearch?ask=%s", s.baseURL, safeQuery)

	fmt.Printf("Запрос поиска: %s\n", targetURL)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Выполнение запроса (Сеть)
		resp, err := s.httpClient.Get(targetURL)
		if err != nil {
			return nil, fmt.Errorf("ошибка сети: %w", err)
		}

		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, fmt.Errorf("сервер вернул код: %d", resp.StatusCode)
		}

		// Читаем тело ответа в буфер, чтобы убедиться, что оно полностью загружено
		var bodyBuf bytes.Buffer
		if _, err := io.Copy(&bodyBuf, resp.Body); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
		}
		resp.Body.Close()

		// Проверяем минимальный размер ответа (если меньше 1000 байт, вероятно неполный)
		if bodyBuf.Len() < 1000 {
			return nil, fmt.Errorf("ответ слишком короткий (%d байт), возможно неполный", bodyBuf.Len())
		}

		fmt.Printf("Размер ответа: %d байт (попытка %d/%d)\n", bodyBuf.Len(), attempt, maxAttempts)
		_ = os.WriteFile("last_search_response.html", bodyBuf.Bytes(), 0644)

		// Обработка ответа (Парсер)
		books, err := parser.ParseBooks(&bodyBuf)
		if err != nil {
			return nil, fmt.Errorf("ошибка парсинга: %w", err)
		}

		fmt.Printf("Найдено книг: %d\n", len(books))
		if len(books) > 0 {
			return books, nil
		}

		if attempt < maxAttempts {
			fmt.Printf("Книги не найдены, повторяю через 1с (попытка %d/%d)\n", attempt+1, maxAttempts)
			time.Sleep(1 * time.Second)
		}
	}

	// После всех попыток — пустой результат без ошибки
	return []models.Book{}, nil
}

// DownloadFB2 скачивает книгу по ID.
// Возвращает:
// 1. Поток данных (body), который НУЖНО закрыть после чтения.
// 2. Имя файла (которое предложил сервер, или сгенерированное).
// 3. Ошибку.
func (s *FlibustaClient) DownloadFB2(bookID string) (io.ReadCloser, string, error) {
	return s.Download(bookID, "fb2")
}

// Download скачивает книгу по ID и формату.
// formatPath examples: "fb2", "epub", "mobi", "fb2.zip".
func (s *FlibustaClient) Download(bookID string, formatPath string) (io.ReadCloser, string, error) {
	formatPath = strings.TrimSpace(formatPath)
	if formatPath == "" {
		return nil, "", fmt.Errorf("пустой формат скачивания")
	}

	// Формируем URL: /b/{id}/{format}
	downloadURL := fmt.Sprintf("%s/b/%s/%s", s.baseURL, bookID, formatPath)

	fmt.Printf("Запрос на скачивание: %s\n", downloadURL)

	resp, err := s.httpClient.Get(downloadURL)
	if err != nil {
		return nil, "", fmt.Errorf("ошибка сети при скачивании: %w", err)
	}

	if resp.StatusCode != 200 {
		resp.Body.Close() // Обязательно закрываем, если не возвращаем наружу
		return nil, "", fmt.Errorf("сервер вернул ошибку: %s", resp.Status)
	}

	// Пытаемся узнать имя файла, которое предлагает сервер
	// Обычно оно лежит в заголовке Content-Disposition
	filename := parseFilename(resp.Header, bookID+"."+formatPath)

	// Возвращаем тело ответа (Stream).
	// ВАЖНО: Мы НЕ закрываем resp.Body здесь, это должен сделать тот, кто вызвал функцию.
	return resp.Body, filename, nil
}

// GetBookDetails fetches a book page (/b/<id>) and extracts cover + available formats.
func (s *FlibustaClient) GetBookDetails(bookID string) (models.BookDetails, error) {
	targetURL := fmt.Sprintf("%s/b/%s", s.baseURL, bookID)

	resp, err := s.httpClient.Get(targetURL)
	if err != nil {
		return models.BookDetails{}, fmt.Errorf("ошибка сети: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return models.BookDetails{}, fmt.Errorf("сервер вернул код: %d", resp.StatusCode)
	}

	var bodyBuf bytes.Buffer
	if _, err := io.Copy(&bodyBuf, resp.Body); err != nil {
		return models.BookDetails{}, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	details, err := parser.ParseBookDetails(&bodyBuf, bookID)
	if err != nil {
		return models.BookDetails{}, err
	}

	// Normalize cover path to absolute URL (needed to download the image via Tor client).
	if strings.HasPrefix(details.CoverPath, "/") {
		details.CoverPath = s.baseURL + details.CoverPath
	}

	return details, nil
}

// DownloadBytes downloads an arbitrary URL via the configured HTTP client (Tor) and returns bytes.
func (s *FlibustaClient) DownloadBytes(targetURL string) ([]byte, error) {
	resp, err := s.httpClient.Get(targetURL)
	if err != nil {
		return nil, fmt.Errorf("ошибка сети: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("сервер вернул ошибку: %s", resp.Status)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, resp.Body); err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	return buf.Bytes(), nil
}

// Вспомогательная функция для вытаскивания имени файла
func parseFilename(headers http.Header, fallback string) string {
	disposition := headers.Get("Content-Disposition")
	_, params, err := mime.ParseMediaType(disposition)

	// Если удалось вытащить имя из заголовков — супер
	if err == nil {
		if val, ok := params["filename"]; ok {
			return val
		}
	}

	// Если сервер не сказал имя, используем фолбэк.
	return fallback
}
