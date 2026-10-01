package mymcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeClient записывает переданные аргументы и отдаёт заранее заданные ответы.
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

func (f *fakeClient) TopRequests(_ context.Context, phrase string, numPhrases int) (*wordstat.TopRequestsResponse, error) {
	f.topPhrase, f.topNum = phrase, numPhrases
	if f.err != nil {
		return nil, f.err
	}
	return f.topResponse, nil
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
			Results: []wordstat.DynamicsPoint{{Date: "2026-01-31T00:00:00Z", Count: "1050", Share: 8.7e-06}},
		},
		regionResp: &wordstat.RegionsResponse{
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
	RegisterTools(server, fake)

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

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
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
			t.Fatalf("tool %s: unexpected schema type %T", name, tool.InputSchema)
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
}

func TestCallTools(t *testing.T) {
	tests := []struct {
		name       string
		tool       string
		args       map[string]any
		wantText   string
		wantCheck  func(t *testing.T, f *fakeClient)
		wantErrMsg string
	}{
		{
			name: "top_requests прокидывает аргументы",
			tool: "top_requests",
			args: map[string]any{"phrase": "чат бот для бизнеса", "numPhrases": 5},
			wantText: "Фраза: чат бот для бизнеса\n" +
				"Всего показов: 21500\n\n" +
				"Популярные запросы:\n- чатбот: 7371\n\n" +
				"Похожие запросы:\n- чатбот нейросеть: 372\n",
			wantCheck: func(t *testing.T, f *fakeClient) {
				if f.topPhrase != "чат бот для бизнеса" || f.topNum != 5 {
					t.Errorf("got phrase=%q numPhrases=%d", f.topPhrase, f.topNum)
				}
			},
		},
		{
			name: "dynamics прокидывает период и даты",
			tool: "dynamics",
			args: map[string]any{
				"phrase":   "яндекс",
				"period":   "daily",
				"fromDate": "2026-01-01",
				"toDate":   "2026-01-31",
			},
			wantText: "Динамика для «яндекс»:\n2026-01-31T00:00:00Z: count=1050 share=8.7e-06\n",
			wantCheck: func(t *testing.T, f *fakeClient) {
				if f.dynPeriod != "daily" || f.dynFrom != "2026-01-01" || f.dynTo != "2026-01-31" {
					t.Errorf("got period=%q from=%q to=%q", f.dynPeriod, f.dynFrom, f.dynTo)
				}
			},
		},
		{
			name:     "regions прокидывает regionMode",
			tool:     "regions",
			args:     map[string]any{"phrase": "яндекс", "regionMode": "cities"},
			wantText: "Регионы для «яндекс»:\nregionId=213 count=235 share=1.09e-05 affinity=120.4\n",
			wantCheck: func(t *testing.T, f *fakeClient) {
				if f.regionMode != "cities" {
					t.Errorf("got regionMode=%q", f.regionMode)
				}
			},
		},
		{
			name:     "list_regions без аргументов",
			tool:     "list_regions",
			args:     nil,
			wantText: "Регионы:\nРоссия (id=225)\n  Москва (id=213)\n",
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

			if got := callTool(t, session, tt.tool, tt.args); got != tt.wantText {
				t.Errorf("tool output =\n%q\nwant\n%q", got, tt.wantText)
			}
			tt.wantCheck(t, fake)
		})
	}
}

func TestCallToolPropagatesClientError(t *testing.T) {
	fake := newTestFakeClient()
	fake.err = errors.New("unexpected status 400: phrase must not be empty")

	session := connect(t, fake)

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "top_requests",
		Arguments: map[string]any{"phrase": " "},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
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
