// Package wordstat_test — чёрный ящик для клиента Wordstat: запросы уходят на
// подставной httptest-сервер, ответы берутся из фикстур testdata.
package wordstat_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
)

// fixedNow — детерминированное «сейчас» (четверг) для проверки дефолтных диапазонов.
var fixedNow = time.Date(2026, 10, 1, 12, 30, 0, 0, time.UTC)

type recordedRequest struct {
	path string
	body map[string]any
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// newTestClient поднимает httptest-сервер, отдающий фикстуру, и возвращает
// клиент, направленный на него. Все запросы записываются в calls.
func newTestClient(t *testing.T, fixture string, status int, calls *[]recordedRequest) *wordstat.Client {
	t.Helper()
	body := readFixture(t, fixture)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		*calls = append(*calls, recordedRequest{path: r.URL.Path, body: payload})

		w.Header().Set("Content-Type", "application/json")
		if status != http.StatusOK {
			w.WriteHeader(status)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	return wordstat.NewClient("test-api-key", "test-folder",
		wordstat.WithBaseURL(srv.URL+"/v2/wordstat/"),
		wordstat.WithClock(func() time.Time { return fixedNow }),
	)
}

func TestClientMethods(t *testing.T) {
	topRequestsWant := &wordstat.TopRequestsResponse{
		TotalCount: "21500",
		Results: []wordstat.PhraseStat{
			{Phrase: "чат боты для бизнеса", Count: "1100"},
			{Phrase: "чат бот для бизнеса макс", Count: "145"},
		},
		Associations: []wordstat.PhraseStat{
			{Phrase: "чатбот", Count: "7371"},
			{Phrase: "чатбот нейросеть", Count: "372"},
		},
	}
	dynamicsWant := &wordstat.DynamicsResponse{
		Results: []wordstat.DynamicsPoint{
			{Date: "2026-01-31T00:00:00Z", Count: "1050", Share: 8.7e-06},
			{Date: "2026-02-28T00:00:00Z", Count: "1180", Share: 9.4e-06},
			{Date: "2026-03-31T00:00:00Z", Count: "1100", Share: 9.1e-06},
		},
	}
	regionsWant := &wordstat.RegionsResponse{
		Results: []wordstat.RegionStat{
			{RegionID: "213", Count: "235", Share: 0.0000109, AffinityIndex: 120.4},
			{RegionID: "2", Count: "2330855", Share: 0.5818950367758946, AffinityIndex: 123.75386731541778},
		},
	}

	tests := []struct {
		name        string
		fixture     string
		status      int
		call        func(ctx context.Context, c *wordstat.Client) (any, error)
		wantPath    string
		wantPayload map[string]any
		want        any
		wantErr     string
		wantNoCall  bool
	}{
		{
			name:    "topRequests: numPhrases по умолчанию",
			fixture: "top_requests.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.TopRequests(ctx, "чат бот для бизнеса", 0)
			},
			wantPath: "/v2/wordstat/topRequests",
			wantPayload: map[string]any{
				"phrase":     "чат бот для бизнеса",
				"numPhrases": float64(wordstat.DefaultNumPhrases),
				"folderId":   "test-folder",
			},
			want: topRequestsWant,
		},
		{
			name:    "topRequests: явный numPhrases",
			fixture: "top_requests.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.TopRequests(ctx, "яндекс", 100)
			},
			wantPath: "/v2/wordstat/topRequests",
			wantPayload: map[string]any{
				"phrase":     "яндекс",
				"numPhrases": float64(100),
				"folderId":   "test-folder",
			},
			want: topRequestsWant,
		},
		{
			name:    "topRequests: numPhrases выше границы proto",
			fixture: "top_requests.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.TopRequests(ctx, "яндекс", 2001)
			},
			wantErr:    "numPhrases must be in 1..2000",
			wantNoCall: true,
		},
		{
			name:    "topRequests: пустая фраза",
			fixture: "top_requests.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.TopRequests(ctx, "   ", 10)
			},
			wantErr:    "phrase must not be empty",
			wantNoCall: true,
		},
		{
			name:    "dynamics: monthly с явными датами (YYYY-MM-DD → RFC3339)",
			fixture: "dynamics.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Dynamics(ctx, "чат бот для бизнеса", "monthly", "2026-01-01", "2026-03-31")
			},
			wantPath: "/v2/wordstat/dynamics",
			wantPayload: map[string]any{
				"phrase":   "чат бот для бизнеса",
				"period":   wordstat.PeriodMonthly,
				"fromDate": "2026-01-01T00:00:00Z",
				"toDate":   "2026-03-31T00:00:00Z",
				"folderId": "test-folder",
			},
			want: dynamicsWant,
		},
		{
			name:    "dynamics: period по умолчанию и диапазон 12 месяцев",
			fixture: "dynamics.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Dynamics(ctx, "яндекс", "", "", "")
			},
			wantPath: "/v2/wordstat/dynamics",
			wantPayload: map[string]any{
				"phrase":   "яндекс",
				"period":   wordstat.PeriodMonthly,
				"fromDate": "2025-10-01T00:00:00Z",
				"toDate":   "2026-09-30T00:00:00Z",
				"folderId": "test-folder",
			},
			want: dynamicsWant,
		},
		{
			name:    "dynamics: weekly с понедельника по воскресенье",
			fixture: "dynamics.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Dynamics(ctx, "яндекс", "PERIOD_WEEKLY", "2026-03-02T10:00:00Z", "2026-04-05T23:59:59Z")
			},
			wantPath: "/v2/wordstat/dynamics",
			wantPayload: map[string]any{
				"phrase":   "яндекс",
				"period":   wordstat.PeriodWeekly,
				"fromDate": "2026-03-02T00:00:00Z",
				"toDate":   "2026-04-05T00:00:00Z",
				"folderId": "test-folder",
			},
			want: dynamicsWant,
		},
		{
			name:    "dynamics: weekly по умолчанию (12 недель)",
			fixture: "dynamics.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Dynamics(ctx, "яндекс", "weekly", "", "")
			},
			wantPath: "/v2/wordstat/dynamics",
			wantPayload: map[string]any{
				"phrase":   "яндекс",
				"period":   wordstat.PeriodWeekly,
				"fromDate": "2026-07-06T00:00:00Z",
				"toDate":   "2026-09-27T00:00:00Z",
				"folderId": "test-folder",
			},
			want: dynamicsWant,
		},
		{
			name:    "dynamics: daily по умолчанию (60 дней)",
			fixture: "dynamics.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Dynamics(ctx, "яндекс", "daily", "", "")
			},
			wantPath: "/v2/wordstat/dynamics",
			wantPayload: map[string]any{
				"phrase":   "яндекс",
				"period":   wordstat.PeriodDaily,
				"fromDate": "2026-08-02T00:00:00Z",
				"toDate":   "2026-09-30T00:00:00Z",
				"folderId": "test-folder",
			},
			want: dynamicsWant,
		},
		{
			name:    "dynamics: неизвестный period",
			fixture: "dynamics.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Dynamics(ctx, "яндекс", "yearly", "", "")
			},
			wantErr:    `invalid period "yearly"`,
			wantNoCall: true,
		},
		{
			name:    "dynamics: monthly с fromDate не первого числа",
			fixture: "dynamics.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Dynamics(ctx, "яндекс", "monthly", "2026-01-15", "2026-03-31")
			},
			wantErr:    "must be the first day of a month",
			wantNoCall: true,
		},
		{
			name:    "regions: cities → REGION_CITIES",
			fixture: "regions.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Regions(ctx, "чат бот для бизнеса", "cities")
			},
			wantPath: "/v2/wordstat/regions",
			wantPayload: map[string]any{
				"phrase":   "чат бот для бизнеса",
				"region":   wordstat.RegionCities,
				"folderId": "test-folder",
			},
			want: regionsWant,
		},
		{
			name:    "regions: пустой regionMode → REGION_ALL",
			fixture: "regions.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Regions(ctx, "яндекс", "")
			},
			wantPath: "/v2/wordstat/regions",
			wantPayload: map[string]any{
				"phrase":   "яндекс",
				"region":   wordstat.RegionAll,
				"folderId": "test-folder",
			},
			want: regionsWant,
		},
		{
			name:    "regions: неизвестный regionMode",
			fixture: "regions.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Regions(ctx, "яндекс", "districts")
			},
			wantErr:    `invalid regionMode "districts"`,
			wantNoCall: true,
		},
		{
			name:    "regionsTree: дерево регионов с label",
			fixture: "regions_tree.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.RegionsTree(ctx)
			},
			wantPath: "/v2/wordstat/getRegionsTree",
			wantPayload: map[string]any{
				"folderId": "test-folder",
			},
			want: &wordstat.RegionsTreeResponse{
				Regions: []wordstat.RegionNode{
					{
						ID:   "225",
						Name: "Россия",
						Children: []wordstat.RegionNode{
							{ID: "213", Name: "Москва"},
							{ID: "2", Name: "Санкт-Петербург"},
						},
					},
				},
			},
		},
		{
			name:    "ошибка API: сообщение из JSON попадает в текст ошибки",
			fixture: "error_invalid_argument.json",
			status:  http.StatusBadRequest,
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.TopRequests(ctx, "яндекс", 10)
			},
			wantPath: "/v2/wordstat/topRequests",
			wantPayload: map[string]any{
				"phrase":     "яндекс",
				"numPhrases": float64(10),
				"folderId":   "test-folder",
			},
			wantErr: "unexpected status 400: rpc error: code = InvalidArgument desc = The to field value should be the last day of the month",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []recordedRequest
			status := tt.status
			if status == 0 {
				status = http.StatusOK
			}
			c := newTestClient(t, tt.fixture, status, &calls)

			got, err := tt.call(context.Background(), c)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (result %#v)", tt.wantErr, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantNoCall {
				if len(calls) != 0 {
					t.Fatalf("expected no HTTP request, got %d: %#v", len(calls), calls)
				}
				return
			}

			if len(calls) != 1 {
				t.Fatalf("expected exactly 1 HTTP request, got %d", len(calls))
			}
			if calls[0].path != tt.wantPath {
				t.Errorf("request path = %q, want %q", calls[0].path, tt.wantPath)
			}
			if !reflect.DeepEqual(calls[0].body, tt.wantPayload) {
				t.Errorf("request payload = %#v, want %#v", calls[0].body, tt.wantPayload)
			}
			if tt.wantErr != "" {
				return
			}
			if tt.want == nil {
				t.Fatal("test case must define want or wantErr")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("result = %#v, want %#v", got, tt.want)
			}
		})
	}
}
