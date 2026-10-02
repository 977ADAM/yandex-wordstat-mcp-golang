// Package mymcp_test — чёрный ящик для слоя инструментов: MCP-сервер с
// зарегистрированными инструментами поднимается через in-memory транспорт SDK,
// клиент вызывает инструменты по протоколу, клиент Wordstat подменён заглушкой.
package mymcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/mymcp"
	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeClient записывает переданные аргументы и отдаёт заранее заданные ответы.
// Поля-эхо (NumPhrases/Period/Region) заполняются так же, как это делает
// настоящий клиент.
type fakeClient struct {
	topPhrase     string
	topNum        int
	dynPeriod     string
	dynFrom       string
	dynTo         string
	regionMode    string
	treeCalls     int
	err           error
	topResponse   *wordstat.TopRequestsResponse
	dynResponse   *wordstat.DynamicsResponse
	regionResp    *wordstat.RegionsResponse
	regionTreeRes *wordstat.RegionsTreeResponse
}

// Проверка на этапе компиляции, что заглушка реализует интерфейс инструментов.
var _ mymcp.WordstatClient = (*fakeClient)(nil)

func (f *fakeClient) TopRequests(_ context.Context, phrase string, numPhrases int) (*wordstat.TopRequestsResponse, error) {
	f.topPhrase, f.topNum = phrase, numPhrases
	if f.err != nil {
		return nil, f.err
	}
	res := *f.topResponse
	res.NumPhrases = numPhrases
	return &res, nil
}

func (f *fakeClient) Dynamics(_ context.Context, phrase, period, fromDate, toDate string) (*wordstat.DynamicsResponse, error) {
	f.topPhrase, f.dynPeriod, f.dynFrom, f.dynTo = phrase, period, fromDate, toDate
	if f.err != nil {
		return nil, f.err
	}
	return f.dynResponse, nil
}

func (f *fakeClient) Regions(_ context.Context, phrase, regionMode string) (*wordstat.RegionsResponse, error) {
	f.topPhrase, f.regionMode = phrase, regionMode
	if f.err != nil {
		return nil, f.err
	}
	return f.regionResp, nil
}

func (f *fakeClient) RegionsTree(_ context.Context) (*wordstat.RegionsTreeResponse, error) {
	f.treeCalls++
	if f.err != nil {
		return nil, f.err
	}
	return f.regionTreeRes, nil
}

func newTestFakeClient() *fakeClient {
	return &fakeClient{
		topResponse: &wordstat.TopRequestsResponse{
			TotalCount:   "21500",
			Results:      []wordstat.PhraseStat{{Phrase: "чатбот", Count: "7371"}},
			Associations: []wordstat.PhraseStat{{Phrase: "чатбот нейросеть", Count: "372"}},
		},
		dynResponse: &wordstat.DynamicsResponse{
			Period:   wordstat.PeriodDaily,
			FromDate: "2026-01-01T00:00:00Z",
			ToDate:   "2026-01-31T00:00:00Z",
			Results:  []wordstat.DynamicsPoint{{Date: "2026-01-31T00:00:00Z", Count: "1050", Share: 8.7e-06}},
		},
		regionResp: &wordstat.RegionsResponse{
			Region:  wordstat.RegionCities,
			Results: []wordstat.RegionStat{{RegionID: "213", Count: "235", Share: 0.0000109, AffinityIndex: 120.4}},
		},
		regionTreeRes: &wordstat.RegionsTreeResponse{
			Regions: []wordstat.RegionNode{{ID: "225", Name: "Россия", Children: []wordstat.RegionNode{{ID: "213", Name: "Москва"}}}},
		},
	}
}

// connect создаёт MCP-сервер с зарегистрированными инструментами и подключает к нему клиент.
func connect(t *testing.T, fake *fakeClient) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "0.0.0"}, nil)
	mymcp.RegisterTools(server, fake)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return session
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return res
}

// textOf достаёт текст из успешного результата вызова.
func textOf(t *testing.T, name string, res *mcp.CallToolResult) string {
	t.Helper()
	if res.IsError {
		t.Fatalf("call %s returned error result: %v", name, res.Content)
	}
	if len(res.Content) != 1 {
		t.Fatalf("call %s: expected 1 content item, got %d", name, len(res.Content))
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("call %s: content is %T, want *mcp.TextContent", name, res.Content[0])
	}
	return text.Text
}

// structuredJSON достаёт structuredContent в виде JSON.
func structuredJSON(t *testing.T, name string, res *mcp.CallToolResult) string {
	t.Helper()
	if res.StructuredContent == nil {
		t.Fatalf("call %s: structuredContent is empty", name)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("call %s: marshal structuredContent: %v", name, err)
	}
	return string(raw)
}

// assertStructured сравнивает structuredContent с ожидаемой структурой.
// Сравнение семантическое (через any): после round-trip порядок ключей теряется.
func assertStructured(t *testing.T, name string, res *mcp.CallToolResult, want any) {
	t.Helper()
	gotJSON := structuredJSON(t, name, res)

	wantRaw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}

	var got, wantAny any
	if err := json.Unmarshal([]byte(gotJSON), &got); err != nil {
		t.Fatalf("unmarshal structuredContent: %v", err)
	}
	if err := json.Unmarshal(wantRaw, &wantAny); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}

	if !reflect.DeepEqual(got, wantAny) {
		t.Errorf("structuredContent =\n%s\nwant\n%s", gotJSON, wantRaw)
	}
}

func TestRegisteredTools(t *testing.T) {
	session := connect(t, newTestFakeClient())

	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	wantNames := []string{"top_requests", "dynamics", "regions", "list_regions"}
	if len(res.Tools) != len(wantNames) {
		t.Fatalf("got %d tools, want %d", len(res.Tools), len(wantNames))
	}

	byName := make(map[string]*mcp.Tool, len(res.Tools))
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
		if tool.Description == "" {
			t.Errorf("tool %s has empty description", tool.Name)
		}
		if tool.OutputSchema == nil {
			t.Errorf("tool %s has no outputSchema", tool.Name)
		}
	}
	for _, name := range wantNames {
		if byName[name] == nil {
			t.Errorf("tool %s is not registered", name)
		}
	}

	// Оговорка про "required," в теге jsonschema: он целиком уходит в description,
	// поэтому проверяем, что префикс не просочился в схему.
	for name, tool := range byName {
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s: unexpected input schema type %T", name, tool.InputSchema)
		}
		props, _ := schema["properties"].(map[string]any)
		for prop, raw := range props {
			desc, _ := raw.(map[string]any)["description"].(string)
			if strings.HasPrefix(desc, "required,") {
				t.Errorf("tool %s property %s description leaks jsonschema keyword: %q", name, prop, desc)
			}
		}
		if required, _ := schema["required"].([]any); name == "top_requests" {
			if len(required) != 1 || required[0] != "phrase" {
				t.Errorf("top_requests required = %v, want [phrase]", required)
			}
		}
	}

	// Схема вывода top_requests: обязательные поля структурированного результата.
	outputSchema, ok := byName["top_requests"].OutputSchema.(map[string]any)
	if !ok {
		t.Fatalf("top_requests: unexpected output schema type %T", byName["top_requests"].OutputSchema)
	}
	outProps, _ := outputSchema["properties"].(map[string]any)
	for _, field := range []string{"phrase", "totalCount", "requests"} {
		if _, ok := outProps[field]; !ok {
			t.Errorf("top_requests outputSchema has no %q property", field)
		}
	}
}

func TestCallTools(t *testing.T) {
	tests := []struct {
		name       string
		tool       string
		args       map[string]any
		wantText   string
		wantStruct any
		wantCheck  func(t *testing.T, f *fakeClient)
	}{
		{
			name: "top_requests: текст и структура",
			tool: "top_requests",
			args: map[string]any{"phrase": "чат бот для бизнеса", "numPhrases": 5},
			wantText: "Фраза: чат бот для бизнеса\n" +
				"Всего показов: 21500\n\n" +
				"Популярные запросы:\n- чатбот: 7371\n\n" +
				"Похожие запросы:\n- чатбот нейросеть: 372\n",
			wantStruct: mymcp.TopRequestsOutput{
				Phrase:     "чат бот для бизнеса",
				TotalCount: 21500,
				Requests:   []mymcp.PhraseCount{{Phrase: "чатбот", Count: 7371}},
				Associations: []mymcp.PhraseCount{
					{Phrase: "чатбот нейросеть", Count: 372},
				},
			},
			wantCheck: func(t *testing.T, f *fakeClient) {
				if f.topPhrase != "чат бот для бизнеса" || f.topNum != 5 {
					t.Errorf("got phrase=%q numPhrases=%d", f.topPhrase, f.topNum)
				}
			},
		},
		{
			name: "dynamics: фактическое окно попадает в структуру",
			tool: "dynamics",
			args: map[string]any{
				"phrase":   "яндекс",
				"period":   "daily",
				"fromDate": "2026-01-01",
				"toDate":   "2026-01-31",
			},
			wantText: "Динамика для «яндекс»:\n2026-01-31T00:00:00Z: count=1050 share=8.7e-06\n",
			wantStruct: mymcp.DynamicsOutput{
				Phrase:   "яндекс",
				Period:   wordstat.PeriodDaily,
				FromDate: "2026-01-01T00:00:00Z",
				ToDate:   "2026-01-31T00:00:00Z",
				Points:   []mymcp.DynamicsPoint{{Date: "2026-01-31T00:00:00Z", Count: 1050, Share: 8.7e-06}},
			},
			wantCheck: func(t *testing.T, f *fakeClient) {
				if f.dynPeriod != "daily" || f.dynFrom != "2026-01-01" || f.dynTo != "2026-01-31" {
					t.Errorf("got period=%q from=%q to=%q", f.dynPeriod, f.dynFrom, f.dynTo)
				}
			},
		},
		{
			name:     "regions: фактическая группировка попадает в структуру",
			tool:     "regions",
			args:     map[string]any{"phrase": "яндекс", "regionMode": "cities"},
			wantText: "Регионы для «яндекс»:\nregionId=213 count=235 share=1.09e-05 affinity=120.4\n",
			wantStruct: mymcp.RegionsOutput{
				Phrase: "яндекс",
				Region: wordstat.RegionCities,
				Regions: []mymcp.RegionCount{
					{RegionID: "213", Count: 235, Share: 0.0000109, AffinityIndex: 120.4},
				},
			},
			wantCheck: func(t *testing.T, f *fakeClient) {
				if f.regionMode != "cities" {
					t.Errorf("got regionMode=%q", f.regionMode)
				}
			},
		},
		{
			name:     "list_regions: дерево в структуре",
			tool:     "list_regions",
			args:     nil,
			wantText: "Регионы:\nРоссия (id=225)\n  Москва (id=213)\n",
			wantStruct: mymcp.RegionsTreeOutput{
				Count: 2,
				Regions: []mymcp.RegionEntry{
					{ID: "225", Name: "Россия", Depth: 0},
					{ID: "213", Name: "Москва", Depth: 1, ParentID: "225"},
				},
			},
			wantCheck: func(t *testing.T, f *fakeClient) {
				if f.treeCalls != 1 {
					t.Errorf("RegionsTree calls = %d, want 1", f.treeCalls)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newTestFakeClient()
			session := connect(t, fake)

			res := callTool(t, session, tt.tool, tt.args)

			if got := textOf(t, tt.tool, res); got != tt.wantText {
				t.Errorf("tool text =\n%q\nwant\n%q", got, tt.wantText)
			}
			assertStructured(t, tt.tool, res, tt.wantStruct)
			tt.wantCheck(t, fake)
		})
	}
}

// TestEmptyResults проверяет пустые ответы API: нулевые значения proto3 JSON
// опускает, поэтому totalCount приходит пустой строкой и должен стать нулём.
func TestEmptyResults(t *testing.T) {
	tests := []struct {
		name       string
		tool       string
		args       map[string]any
		wantText   string
		wantStruct any
	}{
		{
			name: "top_requests без результатов и ассоциаций",
			tool: "top_requests",
			args: map[string]any{"phrase": "узкая ниша"},
			wantText: "Фраза: узкая ниша\n" +
				"Всего показов: \n\n" +
				"Популярные запросы:\n" +
				"(пусто)\n",
			wantStruct: mymcp.TopRequestsOutput{
				Phrase:     "узкая ниша",
				TotalCount: 0,
				Requests:   []mymcp.PhraseCount{},
			},
		},
		{
			name:       "dynamics без точек",
			tool:       "dynamics",
			args:       map[string]any{"phrase": "узкая ниша"},
			wantText:   "Динамика для «узкая ниша»:\n(пусто)\n",
			wantStruct: mymcp.DynamicsOutput{Phrase: "узкая ниша", Points: []mymcp.DynamicsPoint{}},
		},
		{
			name:       "regions без регионов",
			tool:       "regions",
			args:       map[string]any{"phrase": "узкая ниша"},
			wantText:   "Регионы для «узкая ниша»:\n(пусто)\n",
			wantStruct: mymcp.RegionsOutput{Phrase: "узкая ниша", Regions: []mymcp.RegionCount{}},
		},
		{
			name:       "list_regions с пустым деревом",
			tool:       "list_regions",
			wantText:   "Регионы:\n",
			wantStruct: mymcp.RegionsTreeOutput{Regions: []mymcp.RegionEntry{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeClient{
				topResponse:   &wordstat.TopRequestsResponse{},
				dynResponse:   &wordstat.DynamicsResponse{},
				regionResp:    &wordstat.RegionsResponse{},
				regionTreeRes: &wordstat.RegionsTreeResponse{},
			}
			session := connect(t, fake)

			res := callTool(t, session, tt.tool, tt.args)

			if got := textOf(t, tt.tool, res); got != tt.wantText {
				t.Errorf("tool text =\n%q\nwant\n%q", got, tt.wantText)
			}
			assertStructured(t, tt.tool, res, tt.wantStruct)
		})
	}
}

// TestUnexpectedCountIsError проверяет, что нечисловой count из API не
// превращается в тихую ложь, а приводит к ошибке инструмента.
func TestUnexpectedCountIsError(t *testing.T) {
	fake := newTestFakeClient()
	fake.topResponse = &wordstat.TopRequestsResponse{TotalCount: "много"}

	session := connect(t, fake)

	res := callTool(t, session, "top_requests", map[string]any{"phrase": "яндекс"})
	if !res.IsError {
		t.Fatalf("expected error result, got %#v", res.Content)
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want *mcp.TextContent", res.Content[0])
	}
	if !strings.Contains(text.Text, `"много"`) {
		t.Errorf("error text %q does not mention the raw value", text.Text)
	}
}

func TestCallToolPropagatesClientError(t *testing.T) {
	fake := newTestFakeClient()
	fake.err = errors.New("unexpected status 400: phrase must not be empty")

	session := connect(t, fake)

	res := callTool(t, session, "top_requests", map[string]any{"phrase": " "})
	if !res.IsError {
		t.Fatalf("expected error result, got %#v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("error result has no content")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want *mcp.TextContent", res.Content[0])
	}
	if !strings.Contains(text.Text, "phrase must not be empty") {
		t.Errorf("error text %q does not mention the client error", text.Text)
	}
}
