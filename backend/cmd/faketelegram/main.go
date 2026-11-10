// Подставной Telegram Bot API — только для e2e (docker-compose.e2e.yml): настоящий Telegram в тестах не вызывается.
// Любой метод /bot<токен>/<метод> отвечает ok; getMe — бот FAKE_BOT_NAME. GET /__calls — что сервер отправлял
// (метод и параметры), DELETE /__calls — очистить.
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type call struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func main() {
	name := os.Getenv("FAKE_BOT_NAME")
	if name == "" {
		name = "ohvat_test_bot"
	}
	var mu sync.Mutex
	var calls []call
	mux := http.NewServeMux()
	mux.HandleFunc("/__calls", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodDelete {
			calls = nil
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(append([]call{}, calls...))
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "bot") {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if !json.Valid(body) {
			body = []byte("{}")
		}
		mu.Lock()
		if len(calls) > 1000 {
			calls = calls[500:]
		}
		calls = append(calls, call{Method: parts[1], Params: body})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch parts[1] {
		case "getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"username":"` + name + `"}}`))
		case "getWebhookInfo":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"url":"","pending_update_count":0}}`))
		case "getUpdates":
			time.Sleep(time.Second)
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	})
	srv := &http.Server{Addr: ":8081", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Println("faketelegram :8081")
	log.Fatal(srv.ListenAndServe())
}
