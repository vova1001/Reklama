package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

// S3 — S3-совместимое хранилище (Cloudflare R2, Яндекс Object Storage, MinIO): PUT/GET/DELETE объекта с подписью
// AWS Signature V4, адрес вида {endpoint}/{bucket}/{key}. Без SDK: три запроса — проще и без лишних зависимостей.
type S3 struct {
	Endpoint  string // https://<аккаунт>.r2.cloudflarestorage.com, https://storage.yandexcloud.net
	Bucket    string
	Region    string // auto (R2), ru-central1 (Яндекс)
	AccessKey string
	SecretKey string
	Client    *http.Client
	now       func() time.Time
}

// NewS3 создаёт клиент S3 (таймаут запроса — 30 с).
func NewS3(endpoint, bucket, region, accessKey, secretKey string) *S3 {
	if region == "" {
		region = "auto"
	}
	return &S3{
		Endpoint: strings.TrimRight(endpoint, "/"), Bucket: bucket, Region: region, AccessKey: accessKey, SecretKey: secretKey,
		Client: &http.Client{Timeout: 30 * time.Second}, now: time.Now,
	}
}

// Put — сохранить объект.
func (s *S3) Put(ctx context.Context, key, contentType string, data []byte) error {
	res, err := s.do(ctx, http.MethodPut, key, data, map[string]string{"content-type": contentType, "cache-control": "public, max-age=31536000, immutable"})
	if err != nil {
		return err
	}
	defer drain(res)
	if res.StatusCode/100 != 2 {
		return s3Err("PUT", key, res)
	}
	return nil
}

// Get — объект; нет — domain.ErrNotFound.
func (s *S3) Get(ctx context.Context, key string) (service.Blob, error) {
	res, err := s.do(ctx, http.MethodGet, key, nil, nil)
	if err != nil {
		return service.Blob{}, err
	}
	defer drain(res)
	if res.StatusCode == http.StatusNotFound {
		return service.Blob{}, domain.ErrNotFound
	}
	if res.StatusCode/100 != 2 {
		return service.Blob{}, s3Err("GET", key, res)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return service.Blob{}, err
	}
	mt, _ := http.ParseTime(res.Header.Get("Last-Modified"))
	return service.Blob{ContentType: res.Header.Get("Content-Type"), Data: data, ModTime: mt}, nil
}

// Delete — удалить объекты (по 4 параллельно; нет объекта — не ошибка).
func (s *S3) Delete(ctx context.Context, keys []string) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)
	for _, k := range keys {
		g.Go(func() error {
			res, err := s.do(gctx, http.MethodDelete, k, nil, nil)
			if err != nil {
				return err
			}
			defer drain(res)
			if res.StatusCode/100 != 2 && res.StatusCode != http.StatusNotFound {
				return s3Err("DELETE", k, res)
			}
			return nil
		})
	}
	return g.Wait()
}

func (s *S3) do(ctx context.Context, method, key string, body []byte, extra map[string]string) (*http.Response, error) {
	u, err := url.Parse(s.Endpoint + "/" + s.Bucket + "/" + key)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.ContentLength = int64(len(body))
	headers := map[string]string{}
	for k, v := range extra {
		headers[k] = v
	}
	sign(req, headers, body, s.AccessKey, s.SecretKey, s.Region, s.now().UTC())
	return s.Client.Do(req)
}

// sign — подпись AWS Signature V4 (сервис s3): заголовки host, x-amz-date, x-amz-content-sha256 и extra.
func sign(req *http.Request, extra map[string]string, body []byte, accessKey, secretKey, region string, now time.Time) {
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	sum := sha256.Sum256(body)
	payload := hex.EncodeToString(sum[:])
	h := map[string]string{"host": req.URL.Host, "x-amz-date": amzDate, "x-amz-content-sha256": payload}
	for k, v := range extra {
		h[strings.ToLower(k)] = strings.TrimSpace(v)
	}
	names := make([]string, 0, len(h))
	for k := range h {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + h[k] + "\n")
		if k != "host" {
			req.Header.Set(k, h[k])
		}
	}
	signed := strings.Join(names, ";")
	canon := strings.Join([]string{req.Method, escapePath(req.URL.Path), req.URL.RawQuery, canonHeaders.String(), signed, payload}, "\n")
	scope := day + "/" + region + "/s3/aws4_request"
	cs := sha256.Sum256([]byte(canon))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(cs[:])
	k := hmacSHA256([]byte("AWS4"+secretKey), day)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, "s3")
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, toSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+accessKey+"/"+scope+", SignedHeaders="+signed+", Signature="+sig)
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// escapePath — путь по RFC 3986 (S3: «/» не кодируется, каждый сегмент — один раз).
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = strings.ReplaceAll(url.PathEscape(s), "+", "%2B")
	}
	return strings.Join(parts, "/")
}

func drain(res *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	_ = res.Body.Close()
}

func s3Err(op, key string, res *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	return fmt.Errorf("S3 %s %s: %d %s", op, key, res.StatusCode, strings.TrimSpace(string(b)))
}
