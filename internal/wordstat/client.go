package wordstat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const defaultBaseURL = "https://searchapi.api.cloud.yandex.net/v2/wordstat/"

// maxErrBody ограничивает размер тела ошибки API в сообщении.
const maxErrBody = 512

// Client — клиент Wordstat API (Search API v2).
type Client struct {
	apiKey   string
	folderID string
	baseURL  string
	http     *http.Client
	limiter  *rate.Limiter
	now      func() time.Time
}

// Option настраивает клиент: используется тестами (подмена API и часов), а
// также позволяет направить клиент на прокси или тестовый стенд.
type Option func(*Client)

// WithBaseURL переопределяет базовый URL API.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = baseURL }
}

// WithClock переопределяет источник текущего времени — от него зависят
// дефолтные границы периода в Dynamics.
func WithClock(now func() time.Time) Option {
	return func(c *Client) { c.now = now }
}

// NewClient создаёт клиент. folderID обязателен для каждого запроса.
func NewClient(apiKey, folderID string, opts ...Option) *Client {
	c := &Client{
		apiKey:   apiKey,
		folderID: folderID,
		baseURL:  defaultBaseURL,
		http:     &http.Client{Timeout: 60 * time.Second},
		limiter:  rate.NewLimiter(10, 1), // 10 запросов/сек
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// do выполняет POST-запрос к указанному методу Wordstat.
func (c *Client) do(ctx context.Context, method string, payload any) ([]byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit: %w", err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+method, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Api-Key "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("quota exceeded (HTTP 429): %s", apiMessage(data))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, apiMessage(data))
	}

	return data, nil
}

// post — do + разбор ответа в out.
func (c *Client) post(ctx context.Context, method string, payload, out any) error {
	data, err := c.do(ctx, method, payload)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("unmarshal %s response: %w", method, err)
	}
	return nil
}

// apiMessage достаёт человекочитаемое сообщение из ошибки API
// ({"code":3,"message":"..."}), иначе отдаёт тело как есть.
func apiMessage(data []byte) string {
	var apiErr struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &apiErr); err == nil && apiErr.Message != "" {
		return apiErr.Message
	}
	msg := strings.TrimSpace(string(data))
	if len(msg) > maxErrBody {
		msg = msg[:maxErrBody] + "…"
	}
	return msg
}

// TopRequests возвращает топ запросов и ассоциации по фразе (GetTop).
// numPhrases <= 0 → DefaultNumPhrases (20), > MaxNumPhrases → ошибка.
func (c *Client) TopRequests(ctx context.Context, phrase string, numPhrases int) (*TopRequestsResponse, error) {
	if err := validatePhrase(phrase); err != nil {
		return nil, err
	}
	switch {
	case numPhrases <= 0:
		numPhrases = DefaultNumPhrases
	case numPhrases > MaxNumPhrases:
		return nil, fmt.Errorf("numPhrases must be in 1..%d, got %d", MaxNumPhrases, numPhrases)
	}

	payload := map[string]any{
		"phrase":     phrase,
		"numPhrases": numPhrases,
		"folderId":   c.folderID,
	}

	var result TopRequestsResponse
	if err := c.post(ctx, "topRequests", payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Dynamics возвращает динамику частотности (GetDynamics).
// period: daily | weekly | monthly (по умолчанию monthly).
// fromDate/toDate: RFC3339 или YYYY-MM-DD; если не заданы, диапазон
// досчитывается от текущей даты с выравниванием по границам периода.
func (c *Client) Dynamics(ctx context.Context, phrase, period, fromDate, toDate string) (*DynamicsResponse, error) {
	if err := validatePhrase(phrase); err != nil {
		return nil, err
	}

	normalizedPeriod, err := NormalizePeriod(period)
	if err != nil {
		return nil, err
	}

	from, to, err := ResolveDynamicsRange(normalizedPeriod, fromDate, toDate, c.now())
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"phrase":   phrase,
		"period":   normalizedPeriod,
		"fromDate": from,
		"toDate":   to,
		"folderId": c.folderID,
	}

	var result DynamicsResponse
	if err := c.post(ctx, "dynamics", payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Regions возвращает распределение по регионам (GetRegionsDistribution).
// regionMode: all | cities | regions (по умолчанию all).
func (c *Client) Regions(ctx context.Context, phrase, regionMode string) (*RegionsResponse, error) {
	if err := validatePhrase(phrase); err != nil {
		return nil, err
	}

	region, err := NormalizeRegion(regionMode)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"phrase":   phrase,
		"region":   region,
		"folderId": c.folderID,
	}

	var result RegionsResponse
	if err := c.post(ctx, "regions", payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RegionsTree возвращает справочник регионов (GetRegionsTree).
func (c *Client) RegionsTree(ctx context.Context) (*RegionsTreeResponse, error) {
	payload := map[string]any{"folderId": c.folderID}

	var result RegionsTreeResponse
	if err := c.post(ctx, "getRegionsTree", payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
