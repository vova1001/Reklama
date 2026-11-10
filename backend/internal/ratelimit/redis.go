package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// tokenBucketScript — атомарный токен-бакет в хеше {t: токены, l: последнее обращение, мс}. Время — от самого
// Redis (TIME), чтобы часы серверов приложения не расходились. Ответ: {1|0, через сколько мс следующий токен}.
var tokenBucketScript = redis.NewScript(`
local rate  = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local ttl   = tonumber(ARGV[3])
local t     = redis.call('TIME')
local now   = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
local b     = redis.call('HMGET', KEYS[1], 't', 'l')
local tokens = tonumber(b[1])
local last   = tonumber(b[2])
if tokens == nil or last == nil then
  tokens = burst
  last = now
end
local elapsed = now - last
if elapsed > 0 then
  tokens = math.min(burst, tokens + elapsed * rate / 1000)
end
local allowed, wait = 0, 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
else
  wait = math.ceil((1 - tokens) * 1000 / rate)
end
redis.call('HSET', KEYS[1], 't', tostring(tokens), 'l', tostring(now))
redis.call('PEXPIRE', KEYS[1], ttl)
return {allowed, wait}
`)

// Redis — токен-бакеты в Redis: один лимит на все серверы приложения. Ключи живут, пока бакет не наполнится.
type Redis struct {
	rdb    redis.Scripter
	prefix string
	rate   Rate
	ttl    int64 // мс
}

// NewRedis создаёт ограничитель в Redis; prefix отделяет правила друг от друга (например, "rl:api:").
func NewRedis(rdb redis.Scripter, prefix string, r Rate) *Redis {
	r = r.normalized()
	return &Redis{rdb: rdb, prefix: prefix, rate: r, ttl: r.idleAfter().Milliseconds()}
}

// Allow тратит токен ключа.
func (l *Redis) Allow(ctx context.Context, key string) (bool, time.Duration, error) {
	res, err := tokenBucketScript.Run(ctx, l.rdb, []string{l.prefix + key}, l.rate.RPS, l.rate.Burst, l.ttl).Int64Slice()
	if err != nil {
		return false, 0, fmt.Errorf("лимит в Redis: %w", err)
	}
	if len(res) != 2 {
		return false, 0, fmt.Errorf("лимит в Redis: неожиданный ответ %v", res)
	}
	return res[0] == 1, time.Duration(res[1]) * time.Millisecond, nil
}

// Connect — клиент Redis по REDIS_URL (redis:// или rediss://) с проверкой связи.
func Connect(ctx context.Context, url string) (*redis.Client, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("REDIS_URL: %w", err)
	}
	opt.DialTimeout = 3 * time.Second
	opt.ReadTimeout = 300 * time.Millisecond
	opt.WriteTimeout = 300 * time.Millisecond
	c := redis.NewClient(opt)
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := c.Ping(pctx).Err(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("Redis недоступен: %w", err)
	}
	return c, nil
}
