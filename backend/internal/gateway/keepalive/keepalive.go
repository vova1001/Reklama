// Package keepalive — HTTP-запрос к своему публичному адресу (${KEEPALIVE_URL}/healthz). Реализует service.Prober.
package keepalive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client — GET ${base}/healthz; таймаут задаёт контекст вызывающего.
type Client struct {
	URL  string // полный адрес проверки
	HTTP *http.Client
}

// New — клиент для базового адреса сайта (без пути).
func New(base string) *Client {
	return &Client{URL: strings.TrimRight(base, "/") + "/healthz", HTTP: &http.Client{}}
}

// Probe — ответ 2xx — nil; иначе ошибка с кодом ответа (тело читается до 4 КБ и отбрасывается — соединение
// переиспользуется).
func (c *Client) Probe(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ohvat-keepalive/1")
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("GET %s: ответ %d", c.URL, resp.StatusCode)
	}
	return nil
}
