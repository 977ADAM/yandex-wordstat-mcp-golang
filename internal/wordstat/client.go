package wordstat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

const baseURL = "https://searchapi.api.cloud.yandex.net/v2/wordstat/"

type Client struct {
	apiKey   string
	folderID string
	http     *http.Client
	limiter  *rate.Limiter
}

func NewClient(apiKey, folderID string) *Client {
	return &Client{
		apiKey:   apiKey,
		folderID: folderID,
		http:     &http.Client{Timeout: 60 * time.Second},
		limiter:  rate.NewLimiter(10, 1), // 10 запросов/сек
	}
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+method, bytes.NewReader(body))
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

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("quota exceeded (HTTP 429)")
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(data))
	}

	return io.ReadAll(resp.Body)
}

// TopRequests возвращает топ запросов и ассоциации по фразе.
func (c *Client) TopRequests(ctx context.Context, phrase string, numPhrases int) (*TopRequestsResponse, error) {
	if numPhrases <= 0 || numPhrases > 2000 {
		numPhrases = 2000
	}
	payload := map[string]any{
		"phrase":     phrase,
		"numPhrases": numPhrases,
		"folderId":   c.folderID,
	}
	data, err := c.do(ctx, "topRequests", payload)
	if err != nil {
		return nil, err
	}
	var result TopRequestsResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &result, nil
}

// Dynamics возвращает динамику частотности.
func (c *Client) Dynamics(ctx context.Context, phrase, period, fromDate, toDate string) (*DynamicsResponse, error) {
	payload := map[string]any{
		"phrase":   phrase,
		"period":   period, // daily | weekly | monthly
		"folderId": c.folderID,
	}
	if fromDate != "" { payload["fromDate"] = fromDate }
	if toDate != ""   { payload["toDate"] = toDate }

	data, err := c.do(ctx, "dynamics", payload)
	if err != nil {
		return nil, err
	}
	var result DynamicsResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &result, nil
}

// Regions возвращает распределение по регионам.
func (c *Client) Regions(ctx context.Context, phrase, regionMode string) (*RegionsResponse, error) {
	payload := map[string]any{
		"phrase":     phrase,
		"regionMode": regionMode, // all | cities | regions
		"folderId":   c.folderID,
	}
	data, err := c.do(ctx, "regions", payload)
	if err != nil {
		return nil, err
	}
	var result RegionsResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &result, nil
}

// RegionsTree возвращает справочник регионов.
func (c *Client) RegionsTree(ctx context.Context) (*RegionsTreeResponse, error) {
	payload := map[string]any{"folderId": c.folderID}
	data, err := c.do(ctx, "getRegionsTree", payload)
	if err != nil {
		return nil, err
	}
	var result RegionsTreeResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &result, nil
}