package domain

import "errors"

// Ошибки хранилища, которые понимает сервис (без знания о SQL).
var (
	ErrNotFound = errors.New("не найдено")
	ErrConflict = errors.New("уже существует")
)
