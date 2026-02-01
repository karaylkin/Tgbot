package models

import "fmt"

// Book — DTO (Data Transfer Object) для книги.
type Book struct {
	ID     string
	Title  string
	Author string
}

// String — метод для красивого вывода в консоль.
func (b Book) String() string {
	return fmt.Sprintf("📚 %s\n   Автор: %s\n   ID: %s\n", b.Title, b.Author, b.ID)
}
