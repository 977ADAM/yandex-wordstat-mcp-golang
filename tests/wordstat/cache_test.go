package wordstat_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
)

// cacheHarness — httptest-сервер со счётчиком запросов и управляемыми часами.
type cacheHarness struct {
	server   *httptest.Server
	client   *wordstat.Client
	mu       sync.Mutex
	requests int

	clockMu sync.Mutex
	now     time.Time
}

func newCacheHarness(t *testing.T, opts ...wordstat.Option) *cacheHarness {
	t.Helper()

	h := &cacheHarness{now: fixedNow}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.requests++
		h.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/topRequests"):
			_, _ = w.Write([]byte(`{"totalCount":"100","results":[{"phrase":"зимняя резина","count":"100"}]}`))
		default:
			_, _ = w.Write([]byte(`{"regions":[{"id":"225","label":"Россия"}]}`))
		}
	}))
	t.Cleanup(server.Close)

	h.server = server
	base := []wordstat.Option{
		wordstat.WithBaseURL(server.URL + "/v2/wordstat/"),
		wordstat.WithClock(func() time.Time {
			h.clockMu.Lock()
			defer h.clockMu.Unlock()
			return h.now
		}),
	}
	h.client = wordstat.NewClient("test-api-key", "test-folder", append(base, opts...)...)
	return h
}

func (h *cacheHarness) advance(d time.Duration) {
	h.clockMu.Lock()
	defer h.clockMu.Unlock()
	h.now = h.now.Add(d)
}

func (h *cacheHarness) requestCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.requests
}

func TestCacheHitAndMiss(t *testing.T) {
	h := newCacheHarness(t)
	ctx := context.Background()
	params := wordstat.TopParams{Phrase: "зимняя резина"}

	first, err := h.client.TopRequests(ctx, params)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if first.CacheHit {
		t.Error("first call: CacheHit = true, want false")
	}

	second, err := h.client.TopRequests(ctx, params)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !second.CacheHit {
		t.Error("second call: CacheHit = false, want true")
	}
	if h.requestCount() != 1 {
		t.Errorf("HTTP requests = %d, want 1", h.requestCount())
	}
	if second.TotalCount != first.TotalCount {
		t.Errorf("cached response differs: %s vs %s", second.TotalCount, first.TotalCount)
	}
}

func TestCacheKeyIncludesParams(t *testing.T) {
	h := newCacheHarness(t)
	ctx := context.Background()

	if _, err := h.client.TopRequests(ctx, wordstat.TopParams{Phrase: "зимняя резина"}); err != nil {
		t.Fatalf("call 1: %v", err)
	}
	if _, err := h.client.TopRequests(ctx, wordstat.TopParams{Phrase: "зимняя резина", NumPhrases: 100}); err != nil {
		t.Fatalf("call 2: %v", err)
	}
	if _, err := h.client.TopRequests(ctx, wordstat.TopParams{Phrase: "зимняя резина", Regions: []string{"213"}}); err != nil {
		t.Fatalf("call 3: %v", err)
	}
	if _, err := h.client.TopRequests(ctx, wordstat.TopParams{Phrase: "зимняя резина", Devices: []string{"phone"}}); err != nil {
		t.Fatalf("call 4: %v", err)
	}
	if _, err := h.client.Dynamics(ctx, wordstat.DynamicsParams{Phrase: "зимняя резина", Period: "monthly"}); err != nil {
		t.Fatalf("call 5: %v", err)
	}

	// Разные numPhrases / regions / devices / период — разные ключи, поэтому
	// кэш не должен схлопывать их в один ответ.
	if h.requestCount() != 5 {
		t.Errorf("HTTP requests = %d, want 5", h.requestCount())
	}
}

func TestCacheTTLExpiry(t *testing.T) {
	h := newCacheHarness(t, wordstat.WithCacheTTL(time.Hour))
	ctx := context.Background()
	params := wordstat.TopParams{Phrase: "зимняя резина"}

	if _, err := h.client.TopRequests(ctx, params); err != nil {
		t.Fatalf("first call: %v", err)
	}

	h.advance(30 * time.Minute)
	hit, err := h.client.TopRequests(ctx, params)
	if err != nil {
		t.Fatalf("call within TTL: %v", err)
	}
	if !hit.CacheHit {
		t.Error("within TTL: CacheHit = false, want true")
	}

	h.advance(2 * time.Hour)
	expired, err := h.client.TopRequests(ctx, params)
	if err != nil {
		t.Fatalf("call after TTL: %v", err)
	}
	if expired.CacheHit {
		t.Error("after TTL: CacheHit = true, want false")
	}
	if h.requestCount() != 2 {
		t.Errorf("HTTP requests = %d, want 2 (кэш должен истечь)", h.requestCount())
	}
}

func TestCacheDisabled(t *testing.T) {
	h := newCacheHarness(t, wordstat.WithCacheTTL(0))
	ctx := context.Background()
	params := wordstat.TopParams{Phrase: "зимняя резина"}

	for i := 0; i < 2; i++ {
		res, err := h.client.TopRequests(ctx, params)
		if err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
		if res.CacheHit {
			t.Errorf("call %d: CacheHit = true, но кэш отключён", i+1)
		}
	}

	if h.requestCount() != 2 {
		t.Errorf("HTTP requests = %d, want 2", h.requestCount())
	}
}

// TestRegionsTreeCached проверяет, что справочник регионов (31 КБ) кэшируется:
// потребитель не должен тянуть его на каждый вызов.
func TestRegionsTreeCached(t *testing.T) {
	h := newCacheHarness(t)
	ctx := context.Background()

	first, err := h.client.RegionsTree(ctx)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if first.CacheHit {
		t.Error("first call: CacheHit = true, want false")
	}

	second, err := h.client.RegionsTree(ctx)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !second.CacheHit {
		t.Error("second call: CacheHit = false, want true")
	}
	if h.requestCount() != 1 {
		t.Errorf("HTTP requests = %d, want 1", h.requestCount())
	}
	if len(second.Regions) != 1 || second.Regions[0].Name != "Россия" {
		t.Errorf("cached tree = %+v", second.Regions)
	}
}

// TestCachedResponseIsCopy проверяет, что правки вызывающего кода не портят
// закэшированный ответ.
func TestCachedResponseIsCopy(t *testing.T) {
	h := newCacheHarness(t)
	ctx := context.Background()
	params := wordstat.TopParams{Phrase: "зимняя резина"}

	first, err := h.client.TopRequests(ctx, params)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	first.TotalCount = "испорчено"
	first.Results[0].Phrase = "испорчено"
	first.NumPhrases = 999

	second, err := h.client.TopRequests(ctx, params)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if second.TotalCount != "100" {
		t.Errorf("TotalCount = %q, want %q", second.TotalCount, "100")
	}
	if second.Results[0].Phrase != "зимняя резина" {
		t.Errorf("Phrase = %q, want %q", second.Results[0].Phrase, "зимняя резина")
	}
	if second.NumPhrases != wordstat.DefaultNumPhrases {
		t.Errorf("NumPhrases = %d, want %d", second.NumPhrases, wordstat.DefaultNumPhrases)
	}
	if !second.CacheHit {
		t.Error("second call: CacheHit = false, want true")
	}
}
