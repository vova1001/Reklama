// Package config — настройки сервера из переменных окружения.
package config

import (
	"errors"
	"os"
)

// Config — настройки API.
type Config struct {
	DatabaseURL string // DATABASE_URL — обязательно
	HTTPAddr    string // HTTP_ADDR, по умолчанию :8080
}

// Load читает настройки из окружения.
func Load() (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), HTTPAddr: os.Getenv("HTTP_ADDR")}
	if c.HTTPAddr == "" {
		c.HTTPAddr = ":8080"
	}
	if c.DatabaseURL == "" {
		return c, errors.New("не задан DATABASE_URL")
	}
	return c, nil
}
