// Package server_test — чёрный ящик для HTTP-обвязки: health-check, 404
// и настройки http.Server.
package server_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

func (stubClient) TopRequests(_ context.Context, params wordstat.TopParams) (*wordstat.TopRequestsResponse, error) {
	return &wordstat.TopRequestsResponse{
		TotalCount: "21500",
		Results:    []wordstat.PhraseStat{{Phrase: params.Phrase, Count: "1100"}},
	}, nil
}

func (stubClient) Dynamics(_ context.Context, _ wordstat.DynamicsParams) (*wordstat.DynamicsResponse, error) {
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
	httpServer := httptest.NewServer(server.NewHandler(newMCPServer(), logger, server.Options{AuthToken: testToken}))
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

// TestNewTimeouts фиксирует важное решение: у http.Server нет WriteTimeout,
// иначе обрывается standalone SSE-поток Streamable HTTP.
func TestNewTimeouts(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	httpServer := server.New(":0", newMCPServer(), logger, server.Options{})

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
