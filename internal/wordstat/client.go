package wordstat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const defaultBaseURL = "https://searchapi.api.cloud.yandex.net/v2/wordstat/"

// maxErrBody ограничивает размер тела ошибки API в сообщении.
const maxErrBody = 512

const (
	// DefaultMaxAttempts — сколько всего попыток делает клиент для временно
	// неуспешного запроса (1 попытка + 2 повтора).
	DefaultMaxAttempts = 3
	// DefaultRetryBackoff — базовая пауза перед повтором, если API не прислал
	// Retry-After. Дальше пауза удваивается.
	DefaultRetryBackoff = 500 * time.Millisecond
	// maxRetryDelay ограничивает паузу, чтобы большой Retry-After не подвесил вызов.
	maxRetryDelay = 30 * time.Second
)

// Client — клиент Wordstat API (Search API v2).
type Client struct {
	apiKey   string
	folderID string
	baseURL  string
	http     *http.Client
	limiter  *rate.Limiter
	now      func() time.Time
	cache    *cache

	maxAttempts  int
	retryBackoff time.Duration
}

// Option настраивает клиент: используется тестами (подмена API и часов), а
// также позволяет направить клиент на прокси или тестовый стенд.
type Option func(*Client)

// WithBaseURL переопределяет базовый URL API.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = baseURL }
}

// WithClock переопределяет источник текущего времени — от него зависят
// дефолтные границы периода в Dynamics и истечение кэша.
func WithClock(now func() time.Time) Option {
	return func(c *Client) { c.now = now }
}

// WithCacheTTL задаёт время жизни записей кэша (по умолчанию DefaultCacheTTL).
// Значение <= 0 отключает кэширование.
func WithCacheTTL(ttl time.Duration) Option {
	return func(c *Client) { c.cache = newCache(ttl) }
}

// WithRetries задаёт общее число попыток для временно неуспешных запросов
// (429 и 5xx). Значение <= 1 отключает повторы.
func WithRetries(attempts int) Option {
	return func(c *Client) { c.maxAttempts = attempts }
}

// WithRetryBackoff задаёт базовую паузу перед повтором, когда API не прислал
// Retry-After (по умолчанию DefaultRetryBackoff).
func WithRetryBackoff(backoff time.Duration) Option {
	return func(c *Client) { c.retryBackoff = backoff }
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
		cache:    newCache(DefaultCacheTTL),

		maxAttempts:  DefaultMaxAttempts,
		retryBackoff: DefaultRetryBackoff,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// do выполняет POST-запрос к указанному методу Wordstat.
//
// Ошибки оборачивают sentinel-ошибки (ErrInvalidArgument, ErrQuotaExceeded,
// ErrUnavailable, ErrInternal), чтобы вызывающий код мог отличить сбой от
// «данных нет» и понять, стоит ли повторять запрос.
func (c *Client) do(ctx context.Context, method string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal: %v", ErrInternal, err)
	}

	attempts := c.maxAttempts
	if attempts < 1 {
		attempts = 1
	}

	for attempt := 1; ; attempt++ {
		data, retryAfter, err := c.attempt(ctx, method, body)
		if err == nil {
			return data, nil
		}

		// Повторяем только временные сбои (429 и 5xx/сеть): невалидный запрос
		// или внутренняя ошибка от повтора не изменятся.
		if attempt >= attempts || !isRetryable(err) {
			return nil, err
		}

		if err := sleepCtx(ctx, c.retryDelay(attempt, retryAfter)); err != nil {
			return nil, err
		}
	}
}

// attempt выполняет одну попытку запроса. Возвращает Retry-After, если API его
// прислал, — чтобы вызывающий выбрал паузу.
func (c *Client) attempt(ctx context.Context, method string, body []byte) (data []byte, retryAfter time.Duration, err error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, 0, fmt.Errorf("%w: rate limit: %v", ErrUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+method, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: create request: %v", ErrInternal, err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Api-Key "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: http request: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: read response: %v", ErrUnavailable, err)
	}

	retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), c.now())

	code, message, hasEnvelope := apiErrorEnvelope(data)

	if resp.StatusCode != http.StatusOK {
		if !hasEnvelope {
			message = apiMessage(data)
		}
		return nil, retryAfter, apiFailure(resp.StatusCode, code, message)
	}

	// API иногда отдаёт ошибку с HTTP 200. Без этой проверки потребитель
	// увидел бы «спроса нет» вместо сбоя.
	if hasEnvelope {
		return nil, retryAfter, apiFailure(resp.StatusCode, code, message)
	}

	return data, 0, nil
}

// isRetryable сообщает, имеет ли смысл повторять запрос: те же ошибки помечены
// retryable = true в структурированном ответе инструментов.
func isRetryable(err error) bool {
	return errors.Is(err, ErrQuotaExceeded) || errors.Is(err, ErrUnavailable)
}

// retryDelay выбирает паузу: Retry-After от API, иначе экспоненциальный откат.
func (c *Client) retryDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > maxRetryDelay {
			return maxRetryDelay
		}
		return retryAfter
	}

	delay := c.retryBackoff
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay > maxRetryDelay {
			return maxRetryDelay
		}
	}
	return delay
}

// parseRetryAfter разбирает заголовок Retry-After: секунды или HTTP-дату.
func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}

	if at, err := http.ParseTime(value); err == nil {
		if delay := at.Sub(now); delay > 0 {
			return delay
		}
	}

	return 0
}

// sleepCtx ждёт указанное время или отмену контекста.
func sleepCtx(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return fmt.Errorf("%w: waiting before retry: %v", ErrUnavailable, ctx.Err())
	case <-timer.C:
		return nil
	}
}

// post — do + разбор ответа в out.
func (c *Client) post(ctx context.Context, method string, payload, out any) error {
	data, err := c.do(ctx, method, payload)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: unmarshal %s response: %v", ErrInternal, method, err)
	}
	return nil
}

// apiMessage достаёт человекочитаемое сообщение из ошибки API
// ({"code":3,"message":"..."}), иначе отдаёт тело как есть.
func apiMessage(data []byte) string {
	if _, message, ok := apiErrorEnvelope(data); ok && message != "" {
		return message
	}
	msg := strings.TrimSpace(string(data))
	if len(msg) > maxErrBody {
		msg = msg[:maxErrBody] + "…"
	}
	return msg
}

// TopRequests возвращает топ запросов и ассоциации по фразе (GetTop).
// numPhrases <= 0 → DefaultNumPhrases (20), > MaxNumPhrases → ошибка.
func (c *Client) TopRequests(ctx context.Context, params TopParams) (*TopRequestsResponse, error) {
	if err := validatePhrase(params.Phrase); err != nil {
		return nil, err
	}

	numPhrases := params.NumPhrases
	switch {
	case numPhrases <= 0:
		numPhrases = DefaultNumPhrases
	case numPhrases > MaxNumPhrases:
		return nil, fmt.Errorf("%w: numPhrases must be in 1..%d, got %d", ErrInvalidArgument, MaxNumPhrases, numPhrases)
	}

	regions, err := ValidateRegions(params.Regions)
	if err != nil {
		return nil, err
	}
	devices, err := NormalizeDevices(params.Devices)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"phrase":     params.Phrase,
		"numPhrases": numPhrases,
		"folderId":   c.folderID,
	}
	if len(regions) > 0 {
		payload["regions"] = regions
	}
	if len(devices) > 0 {
		payload["devices"] = devices
	}

	key := cacheKey("topRequests", payload)
	if cached, ok := c.cached(key); ok {
		return cached.(*TopRequestsResponse), nil
	}

	var result TopRequestsResponse
	if err := c.post(ctx, "topRequests", payload, &result); err != nil {
		return nil, err
	}
	result.NumPhrases = numPhrases
	result.Regions, result.Devices = regions, devices
	c.store(key, &result)
	return &result, nil
}

// Dynamics возвращает динамику частотности (GetDynamics).
// period: daily | weekly | monthly (по умолчанию monthly).
// fromDate/toDate: RFC3339 или YYYY-MM-DD; если не заданы, диапазон
// досчитывается от текущей даты с выравниванием по границам периода.
func (c *Client) Dynamics(ctx context.Context, params DynamicsParams) (*DynamicsResponse, error) {
	if err := validatePhrase(params.Phrase); err != nil {
		return nil, err
	}

	normalizedPeriod, err := NormalizePeriod(params.Period)
	if err != nil {
		return nil, err
	}

	from, to, err := ResolveDynamicsRange(normalizedPeriod, params.FromDate, params.ToDate, c.now())
	if err != nil {
		return nil, err
	}

	regions, err := ValidateRegions(params.Regions)
	if err != nil {
		return nil, err
	}
	devices, err := NormalizeDevices(params.Devices)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"phrase":   params.Phrase,
		"period":   normalizedPeriod,
		"fromDate": from,
		"toDate":   to,
		"folderId": c.folderID,
	}
	if len(regions) > 0 {
		payload["regions"] = regions
	}
	if len(devices) > 0 {
		payload["devices"] = devices
	}

	key := cacheKey("dynamics", payload)
	if cached, ok := c.cached(key); ok {
		return cached.(*DynamicsResponse), nil
	}

	var result DynamicsResponse
	if err := c.post(ctx, "dynamics", payload, &result); err != nil {
		return nil, err
	}
	result.Period, result.FromDate, result.ToDate = normalizedPeriod, from, to
	result.Regions, result.Devices = regions, devices
	c.store(key, &result)
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

	key := cacheKey("regions", payload)
	if cached, ok := c.cached(key); ok {
		return cached.(*RegionsResponse), nil
	}

	var result RegionsResponse
	if err := c.post(ctx, "regions", payload, &result); err != nil {
		return nil, err
	}
	result.Region = region
	sortRegionsByCount(result.Results)
	c.store(key, &result)
	return &result, nil
}

// sortRegionsByCount сортирует регионы по убыванию частотности: API отдаёт их
// в произвольном порядке (например, 15816, 496, 19685, 4, 5514).
func sortRegionsByCount(regions []RegionStat) {
	sort.SliceStable(regions, func(i, j int) bool {
		return countValue(regions[i].Count) > countValue(regions[j].Count)
	})
}

// countValue разбирает count (protobuf int64 строкой) для сравнения.
// Нечисловое значение считается нулём: строгость проверки — на уровне
// структурированного вывода (см. mymcp.parseCount).
func countValue(raw string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// RegionsTree возвращает справочник регионов (GetRegionsTree).
func (c *Client) RegionsTree(ctx context.Context) (*RegionsTreeResponse, error) {
	payload := map[string]any{"folderId": c.folderID}

	key := cacheKey("getRegionsTree", payload)
	if cached, ok := c.cached(key); ok {
		return cached.(*RegionsTreeResponse), nil
	}

	var result RegionsTreeResponse
	if err := c.post(ctx, "getRegionsTree", payload, &result); err != nil {
		return nil, err
	}
	c.store(key, &result)
	return &result, nil
}
