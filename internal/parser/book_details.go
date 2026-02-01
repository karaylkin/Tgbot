package parser

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"tor_project/internal/models"

	"github.com/PuerkitoBio/goquery"
)

var (
	sizeInParensRe = regexp.MustCompile(`\(([^)]+)\)\s*$`)
)

func normalizeFlibustaTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}

	// Common page title format: "<Book Name> | Флибуста"
	title = strings.TrimSuffix(title, "| Флибуста")
	title = strings.TrimSpace(title)

	if strings.EqualFold(title, "Флибуста") {
		return ""
	}
	return title
}

func normalizeAuthor(text string) string {
	text = strings.TrimSpace(text)
	text = strings.Trim(text, "[]()")
	text = strings.TrimSpace(text)

	if text == "" {
		return ""
	}

	up := strings.ToUpper(text)
	switch up {
	case "ВСЕ", "АВТОРЫ", "АВТОР":
		return ""
	}

	return text
}

func findFirstAuthor(sel *goquery.Selection) string {
	if sel == nil || sel.Length() == 0 {
		return ""
	}

	var author string
	sel.Find("a[href^='/a/']").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		candidate := normalizeAuthor(a.Text())
		if candidate == "" {
			return true
		}
		author = candidate
		return false
	})

	return author
}

// ParseBookDetails parses a Flibusta "book page" (/b/<id>) and extracts cover + available download formats.
// It is best-effort: HTML markup can vary, so some fields might be empty.
func ParseBookDetails(body io.Reader, bookID string) (models.BookDetails, error) {
	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return models.BookDetails{}, fmt.Errorf("ошибка чтения HTML: %w", err)
	}

	details := models.BookDetails{
		ID: bookID,
	}

	// Title (best-effort)
	// Drupal themes often use: <h1 class="title" id="page-title">...</h1>
	title := strings.TrimSpace(doc.Find("#page-title").First().Text())
	if title == "" {
		title = strings.TrimSpace(doc.Find("h1.title").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(doc.Find("h1").First().Text())
	}
	if title == "" {
		title = strings.TrimSpace(doc.Find("title").First().Text())
	}
	details.Title = normalizeFlibustaTitle(title)

	// Author (best-effort; may be wrong if the page layout changes)
	// Try inside content first to avoid menu links.
	content := doc.Find("#content").First()
	if content.Length() == 0 {
		content = doc.Find("#main").First()
	}
	author := findFirstAuthor(content)
	if author == "" {
		author = findFirstAuthor(doc.Find("body"))
	}
	details.Author = author

	// Cover (best-effort)
	doc.Find("img").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		src, ok := s.Attr("src")
		if !ok {
			return true
		}

		src = strings.TrimSpace(src)
		if src == "" {
			return true
		}

		low := strings.ToLower(src)
		if strings.Contains(low, "bluebreeze_logo") || strings.Contains(low, "favicon") {
			return true
		}

		if strings.Contains(low, "cover") || strings.Contains(low, "/i/") || strings.Contains(low, "/covers/") {
			details.CoverPath = src
			return false
		}

		return true
	})

	// Formats: collect links like /b/<id>/<format>
	prefix := "/b/" + bookID + "/"
	seen := make(map[string]struct{})
	var opts []models.BookFormatOption

	doc.Find("a").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok {
			return
		}

		if !strings.HasPrefix(href, prefix) {
			return
		}

		rest := strings.TrimPrefix(href, prefix)
		rest = strings.TrimSpace(rest)
		if rest == "" {
			return
		}
		if strings.Contains(rest, "/") {
			return
		}
		if strings.Contains(rest, "?") || strings.Contains(rest, "#") {
			rest = strings.SplitN(rest, "?", 2)[0]
			rest = strings.SplitN(rest, "#", 2)[0]
		}
		rest = strings.TrimSpace(rest)
		if rest == "" {
			return
		}

		// Avoid navigation actions.
		switch strings.ToLower(rest) {
		case "read", "edit", "comments":
			return
		}

		if _, ok := seen[rest]; ok {
			return
		}
		seen[rest] = struct{}{}

		label := strings.TrimSpace(a.Text())
		if label == "" {
			label = strings.ToUpper(rest)
		}

		// Normalize label if it contains size in parentheses.
		if m := sizeInParensRe.FindStringSubmatch(label); len(m) == 2 {
			// Keep as is; caller can show it on the button.
		}

		opts = append(opts, models.BookFormatOption{
			Path:  rest,
			Label: label,
		})
	})

	details.Formats = opts
	return details, nil
}
