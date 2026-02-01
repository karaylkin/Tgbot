package parser

import (
	"fmt"
	"io"
	"strings"

	"github.com/PuerkitoBio/goquery"

	// Импортируем наши модели.
	// Замени 'tor_project' на имя твоего модуля из go.mod!
	"tor_project/internal/models"
)

// ParseBooks принимает поток данных (HTML) и возвращает список книг.
func ParseBooks(body io.Reader) ([]models.Book, error) {
	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения HTML: %w", err)
	}

	var books []models.Book

	// Логика поиска по DOM-дереву
	doc.Find("li").Each(func(i int, s *goquery.Selection) {
		// Ищем ссылку на книгу (/b/...)
		link := s.Find("a[href^='/b/']")
		if link.Length() == 0 {
			return
		}

		title := link.Text()
		href, _ := link.Attr("href")
		id := strings.TrimPrefix(href, "/b/")

		// Ищем автора (/a/...)
		var authors []string
		s.Find("a[href^='/a/']").Each(func(_ int, a *goquery.Selection) {
			authors = append(authors, a.Text())
		})

		authorStr := "Неизвестен"
		if len(authors) > 0 {
			authorStr = strings.Join(authors, ", ")
		}

		books = append(books, models.Book{
			ID:     id,
			Title:  title,
			Author: authorStr,
		})
	})

	return books, nil
}
