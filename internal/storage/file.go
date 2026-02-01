package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SaveFile сохраняет данные из reader в файл по указанному пути.
func SaveFile(filename string, data io.Reader) error {
	// 1. Очищаем имя файла от опасных символов (на всякий случай)
	cleanName := filepath.Base(filename)

	// 2. Получаем абсолютный путь к текущей директории
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("не удалось получить рабочую директорию: %w", err)
	}

	fullPath := filepath.Join(cwd, cleanName)

	// 3. Создаем файл
	out, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("не удалось создать файл: %w", err)
	}
	defer out.Close() // Закроем файл, когда закончим писать

	// 4. Копируем данные из сети (data) в файл (out)
	// io.Copy делает это эффективно, кусками, не загружая всё в память.
	bytesWritten, err := io.Copy(out, data)
	if err != nil {
		return fmt.Errorf("ошибка записи данных: %w", err)
	}

	fmt.Printf("Файл сохранен: %s (Размер: %.2f MB)\n", fullPath, float64(bytesWritten)/1024/1024)
	return nil
}
