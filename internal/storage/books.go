package storage

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type SavedFile struct {
	RelativePath string
	SizeBytes    int64
}

func SaveBookFile(baseDir string, originalName string, data io.Reader, maxSize int64) (SavedFile, error) {
	if baseDir == "" {
		return SavedFile{}, fmt.Errorf("пустая директория хранения")
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return SavedFile{}, fmt.Errorf("не удалось создать директорию хранения: %w", err)
	}

	ext := filepath.Ext(originalName)
	if ext == "" {
		ext = ".bin"
	}

	name, err := randomHex(16)
	if err != nil {
		return SavedFile{}, fmt.Errorf("не удалось сгенерировать имя файла: %w", err)
	}

	filename := name + ext
	fullPath := filepath.Join(baseDir, filename)

	out, err := os.Create(fullPath)
	if err != nil {
		return SavedFile{}, fmt.Errorf("не удалось создать файл: %w", err)
	}
	defer out.Close()

	reader := data
	if maxSize > 0 {
		reader = io.LimitReader(data, maxSize+1)
	}

	n, err := io.Copy(out, reader)
	if err != nil {
		_ = os.Remove(fullPath)
		return SavedFile{}, fmt.Errorf("ошибка записи файла: %w", err)
	}

	if maxSize > 0 && n > maxSize {
		_ = os.Remove(fullPath)
		return SavedFile{}, fmt.Errorf("файл слишком большой")
	}

	return SavedFile{
		RelativePath: filename,
		SizeBytes:    n,
	}, nil
}

func randomHex(n int) (string, error) {
	if n <= 0 {
		return "", fmt.Errorf("некорректная длина")
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", buf), nil
}
