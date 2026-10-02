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
//
// Ошибки оборачивают sentinel-ошибки (ErrInvalidArgument, ErrQuotaExceeded,
// ErrUnavailable, ErrInternal), чтобы вызывающий код мог отличить сбой от
// «данных нет» и понять, стоит ли повторять запрос.
func (c *Client) do(ctx context.Context, method string, payload any) ([]byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("%w: rate limit: %v", ErrUnavailable, err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal: %v", ErrInternal, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+method, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: create request: %v", ErrInternal, err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Api-Key "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: http request: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read response: %v", ErrUnavailable, err)
	}

	code, message, hasEnvelope := apiErrorEnvelope(data)

	if resp.StatusCode != http.StatusOK {
		if !hasEnvelope {
			message = apiMessage(data)
		}
		return nil, apiFailure(resp.StatusCode, code, message)
	}

	// API иногда отдаёт ошибку с HTTP 200. Без этой проверки потребитель
	// увидел бы «спроса нет» вместо сбоя.
	if hasEnvelope {
		return nil, apiFailure(resp.StatusCode, code, message)
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

	var result TopRequestsResponse
	if err := c.post(ctx, "topRequests", payload, &result); err != nil {
		return nil, err
	}
	result.NumPhrases = numPhrases
	result.Regions, result.Devices = regions, devices
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

	var result DynamicsResponse
	if err := c.post(ctx, "dynamics", payload, &result); err != nil {
		return nil, err
	}
	result.Period, result.FromDate, result.ToDate = normalizedPeriod, from, to
	result.Regions, result.Devices = regions, devices
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
	result.Region = region
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
