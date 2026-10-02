package wordstat_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
)

// retryHarness отдаёт заранее заданную последовательность ответов и считает попытки.
type retryHarness struct {
	client   *wordstat.Client
	mu       sync.Mutex
	attempts int
}

// newRetryHarness: responses — статус и тело для каждой попытки по порядку;
// последний ответ повторяется, если попыток больше.
func newRetryHarness(t *testing.T, responses []struct {
	Status     int
	Body       string
	RetryAfter string
}, opts ...wordstat.Option) *retryHarness {
	t.Helper()

	h := &retryHarness{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		h.mu.Lock()
		index := h.attempts
		h.attempts++
		h.mu.Unlock()

		if index >= len(responses) {
			index = len(responses) - 1
		}
		response := responses[index]

		w.Header().Set("Content-Type", "application/json")
		if response.RetryAfter != "" {
			w.Header().Set("Retry-After", response.RetryAfter)
		}
		w.WriteHeader(response.Status)
		_, _ = w.Write([]byte(response.Body))
	}))
	t.Cleanup(srv.Close)

	base := []wordstat.Option{
		wordstat.WithBaseURL(srv.URL + "/v2/wordstat/"),
		wordstat.WithCacheTTL(0), // кэш здесь только мешает
	}
	h.client = wordstat.NewClient("test-api-key", "test-folder", append(base, opts...)...)
	return h
}

func (h *retryHarness) attemptsCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attempts
}

type fakeResponse = struct {
	Status     int
	Body       string
	RetryAfter string
}

func topOK(body string) fakeResponse {
	return fakeResponse{Status: http.StatusOK, Body: body}
}

const topBody = `{"totalCount":"100","results":[]}`

func TestRetryOnUnavailable(t *testing.T) {
	h := newRetryHarness(t, []fakeResponse{
		{Status: http.StatusInternalServerError, Body: `{"code":13,"message":"internal"}`},
		topOK(topBody),
	}, wordstat.WithRetryBackoff(time.Millisecond))

	res, err := h.client.TopRequests(context.Background(), wordstat.TopParams{Phrase: "яндекс"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalCount != "100" {
		t.Errorf("TotalCount = %q, want 100", res.TotalCount)
	}
	if h.attemptsCount() != 2 {
		t.Errorf("attempts = %d, want 2", h.attemptsCount())
	}
}

func TestRetryExhaustedOnQuota(t *testing.T) {
	h := newRetryHarness(t, []fakeResponse{
		{Status: http.StatusTooManyRequests, Body: `{"code":8,"message":"quota exceeded"}`},
	}, wordstat.WithRetries(3), wordstat.WithRetryBackoff(time.Millisecond))

	_, err := h.client.TopRequests(context.Background(), wordstat.TopParams{Phrase: "яндекс"})
	if !errors.Is(err, wordstat.ErrQuotaExceeded) {
		t.Fatalf("errors.Is(err, ErrQuotaExceeded) = false, err = %v", err)
	}
	if h.attemptsCount() != 3 {
		t.Errorf("attempts = %d, want 3", h.attemptsCount())
	}
}

func TestNoRetryOnInvalidArgument(t *testing.T) {
	h := newRetryHarness(t, []fakeResponse{
		{Status: http.StatusBadRequest, Body: `{"code":3,"message":"bad phrase"}`},
	}, wordstat.WithRetryBackoff(time.Millisecond))

	_, err := h.client.TopRequests(context.Background(), wordstat.TopParams{Phrase: "яндекс"})
	if !errors.Is(err, wordstat.ErrInvalidArgument) {
		t.Fatalf("errors.Is(err, ErrInvalidArgument) = false, err = %v", err)
	}
	if h.attemptsCount() != 1 {
		t.Errorf("attempts = %d, want 1: невалидный запрос незачем повторять", h.attemptsCount())
	}
}

func TestRetriesDisabled(t *testing.T) {
	h := newRetryHarness(t, []fakeResponse{
		{Status: http.StatusServiceUnavailable, Body: `{"code":14,"message":"unavailable"}`},
		topOK(topBody),
	}, wordstat.WithRetries(1), wordstat.WithRetryBackoff(time.Millisecond))

	_, err := h.client.TopRequests(context.Background(), wordstat.TopParams{Phrase: "яндекс"})
	if !errors.Is(err, wordstat.ErrUnavailable) {
		t.Fatalf("errors.Is(err, ErrUnavailable) = false, err = %v", err)
	}
	if h.attemptsCount() != 1 {
		t.Errorf("attempts = %d, want 1", h.attemptsCount())
	}
}

// TestRetryAfterHonored проверяет, что пауза берётся из Retry-After, даже если
// базовый backoff меньше.
func TestRetryAfterHonored(t *testing.T) {
	h := newRetryHarness(t, []fakeResponse{
		{Status: http.StatusTooManyRequests, Body: `{"code":8,"message":"quota"}`, RetryAfter: "1"},
		topOK(topBody),
	}, wordstat.WithRetryBackoff(time.Millisecond))

	start := time.Now()
	if _, err := h.client.TopRequests(context.Background(), wordstat.TopParams{Phrase: "яндекс"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	elapsed := time.Since(start)

	if h.attemptsCount() != 2 {
		t.Fatalf("attempts = %d, want 2", h.attemptsCount())
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("elapsed = %v, want >= ~1s (Retry-After проигнорирован)", elapsed)
	}
}

func TestRetryStopsOnContextCancel(t *testing.T) {
	h := newRetryHarness(t, []fakeResponse{
		{Status: http.StatusTooManyRequests, Body: `{"code":8,"message":"quota"}`, RetryAfter: "30"},
	}, wordstat.WithRetries(3))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := h.client.TopRequests(ctx, wordstat.TopParams{Phrase: "яндекс"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if elapsed > 5*time.Second {
		t.Errorf("elapsed = %v: ожидание не прервалось по контексту", elapsed)
	}
	if !errors.Is(err, wordstat.ErrUnavailable) {
		t.Errorf("errors.Is(err, ErrUnavailable) = false, err = %v", err)
	}
}
