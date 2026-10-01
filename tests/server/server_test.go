// Package server_test — чёрный ящик для HTTP-обвязки: health-check, 404,
// сквозной вызов инструментов по Streamable HTTP и настройки http.Server.
package server_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/mymcp"
	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/server"
	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubClient — минимальная реализация mymcp.WordstatClient для проверки транспорта.
type stubClient struct{}

var _ mymcp.WordstatClient = stubClient{}

func (stubClient) TopRequests(_ context.Context, phrase string, _ int) (*wordstat.TopRequestsResponse, error) {
	return &wordstat.TopRequestsResponse{
		TotalCount: "21500",
		Results:    []wordstat.PhraseStat{{Phrase: phrase, Count: "1100"}},
	}, nil
}

func (stubClient) Dynamics(_ context.Context, _, _, _, _ string) (*wordstat.DynamicsResponse, error) {
	return &wordstat.DynamicsResponse{}, nil
}

func (stubClient) Regions(_ context.Context, _, _ string) (*wordstat.RegionsResponse, error) {
	return &wordstat.RegionsResponse{}, nil
}

func (stubClient) RegionsTree(_ context.Context) (*wordstat.RegionsTreeResponse, error) {
	return &wordstat.RegionsTreeResponse{}, nil
}

func newMCPServer() *mcp.Server {
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "yandex-wordstat-mcp", Version: "test"}, nil)
	mymcp.RegisterTools(mcpServer, stubClient{})
	return mcpServer
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	httpServer := httptest.NewServer(server.NewHandler(newMCPServer(), logger))
	t.Cleanup(httpServer.Close)

	return httpServer
}

func TestHealthEndpoint(t *testing.T) {
	httpServer := newTestServer(t)

	resp, err := http.Get(httpServer.URL + server.HealthPath)
	if err != nil {
		t.Fatalf("GET %s: %v", server.HealthPath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "ok\n" {
		t.Errorf("body = %q, want %q", body, "ok\n")
	}
}

func TestUnknownPathIsNotFound(t *testing.T) {
	httpServer := newTestServer(t)

	resp, err := http.Get(httpServer.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestStreamableHTTPTransport(t *testing.T) {
	httpServer := newTestServer(t)
	ctx := context.Background()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL + server.MCPPath,
	}, nil)
	if err != nil {
		t.Fatalf("connect over streamable http: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) != 4 {
		t.Fatalf("got %d tools, want 4", len(tools.Tools))
	}
	names := make(map[string]bool, len(tools.Tools))
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"top_requests", "dynamics", "regions", "list_regions"} {
		if !names[want] {
			t.Errorf("tool %s is not registered", want)
		}
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "top_requests",
		Arguments: map[string]any{"phrase": "яндекс"},
	})
	if err != nil {
		t.Fatalf("call top_requests: %v", err)
	}
	if res.IsError {
		t.Fatalf("top_requests returned error result: %v", res.Content)
	}
	if len(res.Content) != 1 {
		t.Fatalf("got %d content items, want 1", len(res.Content))
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want *mcp.TextContent", res.Content[0])
	}
	if !strings.Contains(text.Text, "Всего показов: 21500") {
		t.Errorf("tool output = %q, want it to contain total count", text.Text)
	}
}

// TestNewTimeouts фиксирует важное решение: у http.Server нет WriteTimeout,
// иначе обрывается standalone SSE-поток Streamable HTTP.
func TestNewTimeouts(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	httpServer := server.New(":0", newMCPServer(), logger)

	if httpServer.Addr != ":0" {
		t.Errorf("Addr = %q, want %q", httpServer.Addr, ":0")
	}
	if httpServer.ReadTimeout != 0 {
		t.Errorf("ReadTimeout = %v, want 0", httpServer.ReadTimeout)
	}
	if httpServer.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want 0 (иначе рвётся SSE-поток)", httpServer.WriteTimeout)
	}
	if httpServer.ReadHeaderTimeout != 10*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 10s", httpServer.ReadHeaderTimeout)
	}
	if httpServer.Handler == nil {
		t.Error("Handler is nil")
	}
}
