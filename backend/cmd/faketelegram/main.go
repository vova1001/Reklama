// Подставной Telegram Bot API — только для e2e (docker-compose.e2e.yml): настоящий Telegram в тестах не вызывается.
// Любой метод /bot<токен>/<метод> отвечает ok; getMe — бот FAKE_BOT_NAME. GET /__calls — что сервер отправлял
// (метод и параметры), DELETE /__calls — очистить. POST /__chats — задать канал или группу для проверки соцсетей
// (getChat, getChatAdministrators, getChatMemberCount): {"id":-100…,"type":"channel","title":"…","username":"…",
// "admins":[{"id":…,"status":"creator"}],"members":1234}; неизвестный чат — 400 «chat not found», как у Telegram.
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

type admin struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

type fakeChat struct {
	ID       int64   `json:"id"`
	Type     string  `json:"type"`
	Title    string  `json:"title"`
	Username string  `json:"username"`
	Admins   []admin `json:"admins"`
	Members  int64   `json:"members"`
}

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
	chats := map[int64]fakeChat{}
	mux := http.NewServeMux()
	mux.HandleFunc("/__chats", func(w http.ResponseWriter, r *http.Request) {
		var c fakeChat
		if r.Method != http.MethodPost || json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&c) != nil || c.ID == 0 {
			http.Error(w, "POST {id, type, title, admins, members}", http.StatusBadRequest)
			return
		}
		mu.Lock()
		chats[c.ID] = c
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
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
		case "getChat", "getChatAdministrators", "getChatMemberCount":
			var p struct {
				ChatID int64 `json:"chat_id"`
			}
			_ = json.Unmarshal(body, &p)
			mu.Lock()
			c, ok := chats[p.ChatID]
			mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`))
				return
			}
			var res any
			switch parts[1] {
			case "getChat":
				res = map[string]any{"id": c.ID, "type": c.Type, "title": c.Title, "username": c.Username}
			case "getChatAdministrators":
				out := []map[string]any{{"status": "administrator", "user": map[string]any{"id": 1, "is_bot": true, "username": name}}}
				for _, a := range c.Admins {
					out = append(out, map[string]any{"status": a.Status, "user": map[string]any{"id": a.ID, "is_bot": false, "first_name": "Админ"}})
				}
				res = out
			default:
				res = c.Members
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": res})
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
