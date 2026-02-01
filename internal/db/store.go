package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type LibraryItem struct {
	FileID          int64  `json:"file_id"`
	BookID          int64  `json:"book_id"`
	Title           string `json:"title"`
	Author          string `json:"author"`
	Format          string `json:"format"`
	AddedAt         string `json:"added_at"`
	CurrentLocation string `json:"current_location,omitempty"`
}

type BookFile struct {
	ID        int64
	Path      string
	Format    string
	SizeBytes int64
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("путь к SQLite пустой")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("не удалось создать директорию БД: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия БД: %w", err)
	}

	if err := applyPragmas(db); err != nil {
		db.Close()
		return nil, err
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func applyPragmas(db *sql.DB) error {
	pragma := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
	}

	for _, stmt := range pragma {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("ошибка PRAGMA: %w", err)
		}
	}
	return nil
}

func migrate(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS users (
	telegram_id INTEGER PRIMARY KEY,
	username TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS books (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	source_id TEXT,
	title TEXT,
	author TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_books_source_id ON books(source_id);

CREATE TABLE IF NOT EXISTS book_files (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	book_id INTEGER NOT NULL,
	format TEXT,
	path TEXT NOT NULL,
	size_bytes INTEGER,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY(book_id) REFERENCES books(id)
);

CREATE TABLE IF NOT EXISTS user_library (
	user_id INTEGER NOT NULL,
	book_file_id INTEGER NOT NULL,
	added_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	current_location TEXT,
	PRIMARY KEY(user_id, book_file_id),
	FOREIGN KEY(user_id) REFERENCES users(telegram_id),
	FOREIGN KEY(book_file_id) REFERENCES book_files(id)
);

CREATE INDEX IF NOT EXISTS idx_user_library_user_id ON user_library(user_id);
`

	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("ошибка миграции: %w", err)
	}
	return nil
}

func (s *Store) EnsureUser(ctx context.Context, telegramID int64, username string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO users (telegram_id, username)
VALUES (?, ?)
ON CONFLICT(telegram_id) DO UPDATE SET username = excluded.username
`, telegramID, username)
	if err != nil {
		return fmt.Errorf("ошибка сохранения пользователя: %w", err)
	}
	return nil
}

func (s *Store) UpsertBook(ctx context.Context, sourceID string, title string, author string) (int64, error) {
	if sourceID == "" {
		res, err := s.db.ExecContext(ctx, `
INSERT INTO books (title, author) VALUES (?, ?)
`, title, author)
		if err != nil {
			return 0, fmt.Errorf("ошибка вставки книги: %w", err)
		}
		return res.LastInsertId()
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO books (source_id, title, author)
VALUES (?, ?, ?)
ON CONFLICT(source_id) DO UPDATE SET title = excluded.title, author = excluded.author
`, sourceID, title, author)
	if err != nil {
		return 0, fmt.Errorf("ошибка upsert книги: %w", err)
	}

	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM books WHERE source_id = ?`, sourceID).Scan(&id); err != nil {
		return 0, fmt.Errorf("ошибка поиска книги: %w", err)
	}
	return id, nil
}

func (s *Store) InsertBookFile(ctx context.Context, bookID int64, format string, path string, size int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO book_files (book_id, format, path, size_bytes)
VALUES (?, ?, ?, ?)
`, bookID, format, path, size)
	if err != nil {
		return 0, fmt.Errorf("ошибка вставки файла: %w", err)
	}
	return res.LastInsertId()
}

func (s *Store) AddToLibrary(ctx context.Context, userID int64, fileID int64) error {
	_, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO user_library (user_id, book_file_id)
VALUES (?, ?)
`, userID, fileID)
	if err != nil {
		return fmt.Errorf("ошибка добавления в библиотеку: %w", err)
	}
	return nil
}

func (s *Store) ListLibrary(ctx context.Context, userID int64) ([]LibraryItem, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT bf.id, b.id, b.title, b.author, bf.format, ul.added_at, ul.current_location
FROM user_library ul
JOIN book_files bf ON bf.id = ul.book_file_id
JOIN books b ON b.id = bf.book_id
WHERE ul.user_id = ?
ORDER BY ul.added_at DESC
`, userID)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения библиотеки: %w", err)
	}
	defer rows.Close()

	var items []LibraryItem
	for rows.Next() {
		var item LibraryItem
		var current sql.NullString
		if err := rows.Scan(&item.FileID, &item.BookID, &item.Title, &item.Author, &item.Format, &item.AddedAt, &current); err != nil {
			return nil, fmt.Errorf("ошибка скана библиотеки: %w", err)
		}
		if current.Valid {
			item.CurrentLocation = current.String
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка rows: %w", err)
	}
	return items, nil
}

func (s *Store) GetFileForUser(ctx context.Context, userID int64, fileID int64) (BookFile, error) {
	var file BookFile
	err := s.db.QueryRowContext(ctx, `
SELECT bf.id, bf.path, bf.format, bf.size_bytes
FROM book_files bf
JOIN user_library ul ON ul.book_file_id = bf.id
WHERE ul.user_id = ? AND bf.id = ?
`, userID, fileID).Scan(&file.ID, &file.Path, &file.Format, &file.SizeBytes)
	if err != nil {
		if err == sql.ErrNoRows {
			return BookFile{}, fmt.Errorf("файл не найден")
		}
		return BookFile{}, fmt.Errorf("ошибка поиска файла: %w", err)
	}
	return file, nil
}

func (s *Store) UpdateProgress(ctx context.Context, userID int64, fileID int64, location string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE user_library
SET current_location = ?
WHERE user_id = ? AND book_file_id = ?
`, location, userID, fileID)
	if err != nil {
		return fmt.Errorf("ошибка сохранения прогресса: %w", err)
	}
	return nil
}
