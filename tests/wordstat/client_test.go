// Package wordstat_test — чёрный ящик для клиента Wordstat: запросы уходят на
// подставной httptest-сервер, ответы берутся из фикстур testdata.
package wordstat_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
)

// fixedNow — детерминированное «сейчас» (четверг) для проверки дефолтных диапазонов.
var fixedNow = time.Date(2026, 10, 1, 12, 30, 0, 0, time.UTC)

// tooManyRegions — 101 уникальный geo ID: на один больше лимита proto.
var tooManyRegions = func() []string {
	regions := make([]string, 0, wordstat.MaxRegions+1)
	for i := 0; i <= wordstat.MaxRegions; i++ {
		regions = append(regions, strconv.Itoa(1000+i))
	}
	return regions
}()

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
	// Эхо фактически отправленных параметров (NumPhrases/Period/Region) заполняет
	// клиент, поэтому ожидания строятся функциями.
	topRequestsWant := func(numPhrases int, regions, devices []string) *wordstat.TopRequestsResponse {
		return &wordstat.TopRequestsResponse{
			NumPhrases: numPhrases,
			Regions:    regions,
			Devices:    devices,
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
	}
	dynamicsWant := func(period, from, to string, regions, devices []string) *wordstat.DynamicsResponse {
		return &wordstat.DynamicsResponse{
			Period:   period,
			FromDate: from,
			ToDate:   to,
			Regions:  regions,
			Devices:  devices,
			Results: []wordstat.DynamicsPoint{
				{Date: "2026-01-31T00:00:00Z", Count: "1050", Share: 8.7e-06},
				{Date: "2026-02-28T00:00:00Z", Count: "1180", Share: 9.4e-06},
				{Date: "2026-03-31T00:00:00Z", Count: "1100", Share: 9.1e-06},
			},
		}
	}
	// Регионы приходят от API в произвольном порядке, клиент сортирует их по
	// убыванию count.
	regionsWant := func(region string) *wordstat.RegionsResponse {
		return &wordstat.RegionsResponse{
			Region: region,
			Results: []wordstat.RegionStat{
				{RegionID: "2", Count: "2330855", Share: 0.5818950367758946, AffinityIndex: 123.75386731541778},
				{RegionID: "213", Count: "235", Share: 0.0000109, AffinityIndex: 120.4},
			},
		}
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
				return c.TopRequests(ctx, wordstat.TopParams{Phrase: "чат бот для бизнеса"})
			},
			wantPath: "/v2/wordstat/topRequests",
			wantPayload: map[string]any{
				"phrase":     "чат бот для бизнеса",
				"numPhrases": float64(wordstat.DefaultNumPhrases),
				"folderId":   "test-folder",
			},
			want: topRequestsWant(wordstat.DefaultNumPhrases, nil, nil),
		},
		{
			name:    "topRequests: явный numPhrases",
			fixture: "top_requests.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.TopRequests(ctx, wordstat.TopParams{Phrase: "яндекс", NumPhrases: 100})
			},
			wantPath: "/v2/wordstat/topRequests",
			wantPayload: map[string]any{
				"phrase":     "яндекс",
				"numPhrases": float64(100),
				"folderId":   "test-folder",
			},
			want: topRequestsWant(100, nil, nil),
		},
		{
			name:    "topRequests: numPhrases выше границы proto",
			fixture: "top_requests.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.TopRequests(ctx, wordstat.TopParams{Phrase: "яндекс", NumPhrases: 2001})
			},
			wantErr:    "numPhrases must be in 1..2000",
			wantNoCall: true,
		},
		{
			name:    "topRequests: пустая фраза",
			fixture: "top_requests.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.TopRequests(ctx, wordstat.TopParams{Phrase: "   ", NumPhrases: 10})
			},
			wantErr:    "phrase must not be empty",
			wantNoCall: true,
		},
		{
			name:    "dynamics: monthly с явными датами (YYYY-MM-DD → RFC3339)",
			fixture: "dynamics.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Dynamics(ctx, wordstat.DynamicsParams{Phrase: "чат бот для бизнеса", Period: "monthly", FromDate: "2026-01-01", ToDate: "2026-03-31"})
			},
			wantPath: "/v2/wordstat/dynamics",
			wantPayload: map[string]any{
				"phrase":   "чат бот для бизнеса",
				"period":   wordstat.PeriodMonthly,
				"fromDate": "2026-01-01T00:00:00Z",
				"toDate":   "2026-03-31T00:00:00Z",
				"folderId": "test-folder",
			},
			want: dynamicsWant(wordstat.PeriodMonthly, "2026-01-01T00:00:00Z", "2026-03-31T00:00:00Z", nil, nil),
		},
		{
			name:    "dynamics: period по умолчанию и диапазон 12 месяцев",
			fixture: "dynamics.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Dynamics(ctx, wordstat.DynamicsParams{Phrase: "яндекс"})
			},
			wantPath: "/v2/wordstat/dynamics",
			wantPayload: map[string]any{
				"phrase":   "яндекс",
				"period":   wordstat.PeriodMonthly,
				"fromDate": "2025-10-01T00:00:00Z",
				"toDate":   "2026-09-30T00:00:00Z",
				"folderId": "test-folder",
			},
			want: dynamicsWant(wordstat.PeriodMonthly, "2025-10-01T00:00:00Z", "2026-09-30T00:00:00Z", nil, nil),
		},
		{
			name:    "dynamics: weekly с понедельника по воскресенье",
			fixture: "dynamics.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Dynamics(ctx, wordstat.DynamicsParams{Phrase: "яндекс", Period: "PERIOD_WEEKLY", FromDate: "2026-03-02T10:00:00Z", ToDate: "2026-04-05T23:59:59Z"})
			},
			wantPath: "/v2/wordstat/dynamics",
			wantPayload: map[string]any{
				"phrase":   "яндекс",
				"period":   wordstat.PeriodWeekly,
				"fromDate": "2026-03-02T00:00:00Z",
				"toDate":   "2026-04-05T00:00:00Z",
				"folderId": "test-folder",
			},
			want: dynamicsWant(wordstat.PeriodWeekly, "2026-03-02T00:00:00Z", "2026-04-05T00:00:00Z", nil, nil),
		},
		{
			name:    "dynamics: weekly по умолчанию (12 недель)",
			fixture: "dynamics.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Dynamics(ctx, wordstat.DynamicsParams{Phrase: "яндекс", Period: "weekly"})
			},
			wantPath: "/v2/wordstat/dynamics",
			wantPayload: map[string]any{
				"phrase":   "яндекс",
				"period":   wordstat.PeriodWeekly,
				"fromDate": "2026-07-06T00:00:00Z",
				"toDate":   "2026-09-27T00:00:00Z",
				"folderId": "test-folder",
			},
			want: dynamicsWant(wordstat.PeriodWeekly, "2026-07-06T00:00:00Z", "2026-09-27T00:00:00Z", nil, nil),
		},
		{
			name:    "dynamics: daily по умолчанию (60 дней)",
			fixture: "dynamics.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Dynamics(ctx, wordstat.DynamicsParams{Phrase: "яндекс", Period: "daily"})
			},
			wantPath: "/v2/wordstat/dynamics",
			wantPayload: map[string]any{
				"phrase":   "яндекс",
				"period":   wordstat.PeriodDaily,
				"fromDate": "2026-08-02T00:00:00Z",
				"toDate":   "2026-09-30T00:00:00Z",
				"folderId": "test-folder",
			},
			want: dynamicsWant(wordstat.PeriodDaily, "2026-08-02T00:00:00Z", "2026-09-30T00:00:00Z", nil, nil),
		},
		{
			name:    "dynamics: неизвестный period",
			fixture: "dynamics.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Dynamics(ctx, wordstat.DynamicsParams{Phrase: "яндекс", Period: "yearly"})
			},
			wantErr:    `invalid period "yearly"`,
			wantNoCall: true,
		},
		{
			name:    "dynamics: monthly с fromDate не первого числа",
			fixture: "dynamics.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Dynamics(ctx, wordstat.DynamicsParams{Phrase: "яндекс", Period: "monthly", FromDate: "2026-01-15", ToDate: "2026-03-31"})
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
			want: regionsWant(wordstat.RegionCities),
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
			want: regionsWant(wordstat.RegionAll),
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
			name:    "topRequests: регионы и устройства уходят в payload",
			fixture: "top_requests.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.TopRequests(ctx, wordstat.TopParams{
					Phrase:  "зимняя резина",
					Regions: []string{"213", "1", "213"},
					Devices: []string{"Phone", "desktop", "phone"},
				})
			},
			wantPath: "/v2/wordstat/topRequests",
			wantPayload: map[string]any{
				"phrase":     "зимняя резина",
				"numPhrases": float64(wordstat.DefaultNumPhrases),
				"folderId":   "test-folder",
				"regions":    []any{"213", "1"},
				"devices":    []any{wordstat.DevicePhone, wordstat.DeviceDesktop},
			},
			want: topRequestsWant(wordstat.DefaultNumPhrases, []string{"213", "1"}, []string{wordstat.DevicePhone, wordstat.DeviceDesktop}),
		},
		{
			name:    "topRequests: пустые списки не попадают в payload",
			fixture: "top_requests.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.TopRequests(ctx, wordstat.TopParams{
					Phrase:  "яндекс",
					Regions: []string{},
					Devices: []string{},
				})
			},
			wantPath: "/v2/wordstat/topRequests",
			wantPayload: map[string]any{
				"phrase":     "яндекс",
				"numPhrases": float64(wordstat.DefaultNumPhrases),
				"folderId":   "test-folder",
			},
			want: topRequestsWant(wordstat.DefaultNumPhrases, nil, nil),
		},
		{
			name:    "topRequests: нецифровой регион",
			fixture: "top_requests.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.TopRequests(ctx, wordstat.TopParams{Phrase: "яндекс", Regions: []string{"abc"}})
			},
			wantErr:    `invalid region "abc": expected numeric geo id`,
			wantNoCall: true,
		},
		{
			name:    "topRequests: регионов больше лимита",
			fixture: "top_requests.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.TopRequests(ctx, wordstat.TopParams{Phrase: "яндекс", Regions: tooManyRegions})
			},
			wantErr:    "too many regions: 101, allowed at most 100",
			wantNoCall: true,
		},
		{
			name:    "topRequests: неизвестное устройство",
			fixture: "top_requests.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.TopRequests(ctx, wordstat.TopParams{Phrase: "яндекс", Devices: []string{"watch"}})
			},
			wantErr:    `invalid device "watch": allowed all, desktop, phone, tablet`,
			wantNoCall: true,
		},
		{
			name:    "topRequests: устройств больше лимита",
			fixture: "top_requests.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.TopRequests(ctx, wordstat.TopParams{
					Phrase:  "яндекс",
					Devices: []string{"all", "desktop", "phone", "tablet"},
				})
			},
			wantErr:    "too many devices: 4, allowed at most 3",
			wantNoCall: true,
		},
		{
			name:    "dynamics: регионы и устройства уходят в payload",
			fixture: "dynamics.json",
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.Dynamics(ctx, wordstat.DynamicsParams{
					Phrase:  "зимняя резина",
					Period:  "monthly",
					Regions: []string{"213"},
					Devices: []string{"all"},
				})
			},
			wantPath: "/v2/wordstat/dynamics",
			wantPayload: map[string]any{
				"phrase":   "зимняя резина",
				"period":   wordstat.PeriodMonthly,
				"fromDate": "2025-10-01T00:00:00Z",
				"toDate":   "2026-09-30T00:00:00Z",
				"folderId": "test-folder",
				"regions":  []any{"213"},
				"devices":  []any{wordstat.DeviceAll},
			},
			want: dynamicsWant(wordstat.PeriodMonthly, "2025-10-01T00:00:00Z", "2026-09-30T00:00:00Z", []string{"213"}, []string{wordstat.DeviceAll}),
		},
		{
			name:    "ошибка API: сообщение из JSON попадает в текст ошибки",
			fixture: "error_invalid_argument.json",
			status:  http.StatusBadRequest,
			call: func(ctx context.Context, c *wordstat.Client) (any, error) {
				return c.TopRequests(ctx, wordstat.TopParams{Phrase: "яндекс", NumPhrases: 10})
			},
			wantPath: "/v2/wordstat/topRequests",
			wantPayload: map[string]any{
				"phrase":     "яндекс",
				"numPhrases": float64(10),
				"folderId":   "test-folder",
			},
			wantErr: "invalid argument: HTTP 400: rpc error: code = InvalidArgument desc = The to field value should be the last day of the month",
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

// TestErrorClassification проверяет, что сбои API разбираются по sentinel-ошибкам,
// а текст от API сохраняется.
func TestErrorClassification(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		wantSentinel error
		wantMessage  string
	}{
		{
			name:         "400 → invalid argument",
			status:       http.StatusBadRequest,
			body:         `{"code":3,"message":"rpc error: code = InvalidArgument desc = phrase is too long"}`,
			wantSentinel: wordstat.ErrInvalidArgument,
			wantMessage:  "phrase is too long",
		},
		{
			name:         "429 → quota exceeded",
			status:       http.StatusTooManyRequests,
			body:         `{"code":8,"message":"quota exceeded"}`,
			wantSentinel: wordstat.ErrQuotaExceeded,
			wantMessage:  "quota exceeded",
		},
		{
			name:         "500 → upstream unavailable",
			status:       http.StatusInternalServerError,
			body:         `{"code":13,"message":"internal error"}`,
			wantSentinel: wordstat.ErrUnavailable,
			wantMessage:  "internal error",
		},
		{
			name:         "503 без тела → upstream unavailable",
			status:       http.StatusServiceUnavailable,
			body:         ``,
			wantSentinel: wordstat.ErrUnavailable,
		},
		{
			name:         "403 → internal (проблема доступа на нашей стороне)",
			status:       http.StatusForbidden,
			body:         `{"code":7,"message":"permission denied"}`,
			wantSentinel: wordstat.ErrInternal,
			wantMessage:  "permission denied",
		},
		{
			name:         "ошибка внутри HTTP 200 → не «спроса нет», а сбой",
			status:       http.StatusOK,
			body:         `{"code":3,"message":"rpc error: code = InvalidArgument desc = bad region"}`,
			wantSentinel: wordstat.ErrInvalidArgument,
			wantMessage:  "bad region",
		},
		{
			name:         "недоступность внутри HTTP 200",
			status:       http.StatusOK,
			body:         `{"code":14,"message":"unavailable"}`,
			wantSentinel: wordstat.ErrUnavailable,
			wantMessage:  "unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)

			c := wordstat.NewClient("test-api-key", "test-folder", wordstat.WithBaseURL(srv.URL+"/v2/wordstat/"))

			_, err := c.TopRequests(context.Background(), wordstat.TopParams{Phrase: "яндекс", NumPhrases: 5})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tt.wantSentinel) {
				t.Errorf("errors.Is(err, %v) = false, err = %v", tt.wantSentinel, err)
			}
			if tt.wantMessage != "" && !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantMessage)
			}
		})
	}
}

// TestNetworkErrorIsUnavailable проверяет, что сетевой сбой — это ErrUnavailable
// (повтор имеет смысл), а не «данных нет».
func TestNetworkErrorIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // сервер больше не слушает

	c := wordstat.NewClient("test-api-key", "test-folder", wordstat.WithBaseURL(url+"/v2/wordstat/"))

	_, err := c.TopRequests(context.Background(), wordstat.TopParams{Phrase: "яндекс", NumPhrases: 5})
	if !errors.Is(err, wordstat.ErrUnavailable) {
		t.Fatalf("errors.Is(err, ErrUnavailable) = false, err = %v", err)
	}
}

// TestValidationErrorsAreInvalidArgument проверяет, что ошибки валидации
// помечены ErrInvalidArgument и потому попадают в код invalid_argument.
func TestValidationErrorsAreInvalidArgument(t *testing.T) {
	c := wordstat.NewClient("test-api-key", "test-folder", wordstat.WithBaseURL("http://127.0.0.1:1/v2/wordstat/"))

	tests := []struct {
		name string
		call func() (any, error)
	}{
		{
			name: "пустая фраза",
			call: func() (any, error) {
				return c.TopRequests(context.Background(), wordstat.TopParams{Phrase: " "})
			},
		},
		{
			name: "слишком большой numPhrases",
			call: func() (any, error) {
				return c.TopRequests(context.Background(), wordstat.TopParams{Phrase: "яндекс", NumPhrases: 2001})
			},
		},
		{
			name: "неизвестный period",
			call: func() (any, error) {
				return c.Dynamics(context.Background(), wordstat.DynamicsParams{Phrase: "яндекс", Period: "yearly"})
			},
		},
		{
			name: "неизвестный regionMode",
			call: func() (any, error) {
				return c.Regions(context.Background(), "яндекс", "districts")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.call()
			if !errors.Is(err, wordstat.ErrInvalidArgument) {
				t.Fatalf("errors.Is(err, ErrInvalidArgument) = false, err = %v", err)
			}
		})
	}
}

// TestRegionsSortedByCount проверяет сортировку регионов по убыванию count:
// API отдаёт их в произвольном порядке, а потребителю нужен ранжированный список.
func TestRegionsSortedByCount(t *testing.T) {
	var calls []recordedRequest
	c := newTestClient(t, "regions_unsorted.json", http.StatusOK, &calls)

	res, err := c.Regions(context.Background(), "зимняя резина", "regions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := make([]string, 0, len(res.Results))
	for _, r := range res.Results {
		got = append(got, r.RegionID)
	}

	// 19685, 15816, 5514, 496, 4, затем регион с пустым count (нулевые значения
	// proto3 JSON опускает).
	want := []string{"213", "1", "192", "2", "225", "1000"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("regions order = %v, want %v", got, want)
		}
	}
}
