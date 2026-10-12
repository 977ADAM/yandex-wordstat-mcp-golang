package server_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/server"
)

// testToken — предвыданный Bearer-токен, которым в тестах защищён /mcp.
// Константа общая для пакета server_test: её используют server_test.go и
// тесты авторизованной сессии.
const testToken = "test-token"

// initializeRequest — минимальный initialize по Streamable HTTP: тело
// запроса, который проходит дальше мидлвари к MCP-обработчику.
const initializeRequest = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"auth-test","version":"0.0.0"}}}`

// newTestServerWithToken поднимает httptest.Server с заданным токеном.
func newTestServerWithToken(t *testing.T, token string) *httptest.Server {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	httpServer := httptest.NewServer(server.NewHandler(newMCPServer(), logger, server.Options{AuthToken: token}))
	t.Cleanup(httpServer.Close)

	return httpServer
}

// mcpRequest собирает запрос к MCPPath: POST несёт initialize, остальные
// методы — без тела. Пустая строка authorization означает «заголовка нет».
func mcpRequest(t *testing.T, httpServer *httptest.Server, method, authorization string) *http.Request {
	t.Helper()

	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader(initializeRequest)
	}
	req, err := http.NewRequest(method, httpServer.URL+server.MCPPath, body)
	if err != nil {
		t.Fatalf("new %s %s: %v", method, server.MCPPath, err)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	return req
}

// doRequest выполняет запрос и закрывает тело после теста.
func doRequest(t *testing.T, req *http.Request) *http.Response {
	t.Helper()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })

	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}

// TestMCPRequiresBearer проверяет таблицу «метод × заголовок»: любой запрос
// к /mcp без верного токена получает 401 и challenge-заголовок.
func TestMCPRequiresBearer(t *testing.T) {
	httpServer := newTestServerWithToken(t, testToken)

	tests := []struct {
		name          string
		authorization string
		wantBody      string
	}{
		{name: "нет заголовка", authorization: "", wantBody: "no bearer token"},
		{name: "другая схема", authorization: "Basic abc", wantBody: "no bearer token"},
		{name: "пустое значение", authorization: "Bearer", wantBody: "no bearer token"},
		{name: "неверный токен", authorization: "Bearer wrong", wantBody: "invalid token"},
		{name: "две схемы", authorization: "Bearer a, Bearer b", wantBody: "no bearer token"},
		{name: "мусорный 512 байт", authorization: "Bearer " + strings.Repeat("garbage-", 64), wantBody: "invalid token"},
	}

	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		for _, tt := range tests {
			t.Run(method+"/"+tt.name, func(t *testing.T) {
				resp := doRequest(t, mcpRequest(t, httpServer, method, tt.authorization))

				if resp.StatusCode != http.StatusUnauthorized {
					t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
				}
				if got := resp.Header.Get("WWW-Authenticate"); got != "Bearer" {
					t.Errorf("WWW-Authenticate = %q, want %q", got, "Bearer")
				}
				if body := readBody(t, resp); !strings.Contains(body, tt.wantBody) {
					t.Errorf("body = %q, want it to contain %q", body, tt.wantBody)
				}
			})
		}
	}
}

// TestBearerSchemeIsCaseInsensitive: SDK сравнивает схему регистронезависимо,
// поэтому «bearer <токен>» в нижнем регистре проходит.
func TestBearerSchemeIsCaseInsensitive(t *testing.T) {
	httpServer := newTestServerWithToken(t, testToken)

	resp := doRequest(t, mcpRequest(t, httpServer, http.MethodPost, "bearer "+testToken))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if body := readBody(t, resp); !strings.Contains(body, "serverInfo") {
		t.Errorf("body = %q, want it to contain %q", body, "serverInfo")
	}
}

// TestBearerToleratesExtraSpaces: заголовок разбивается по пробелам, поэтому
// лишние пробелы вокруг верного токена не мешают.
func TestBearerToleratesExtraSpaces(t *testing.T) {
	httpServer := newTestServerWithToken(t, testToken)

	resp := doRequest(t, mcpRequest(t, httpServer, http.MethodPost, "Bearer   "+testToken))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if body := readBody(t, resp); !strings.Contains(body, "serverInfo") {
		t.Errorf("body = %q, want it to contain %q", body, "serverInfo")
	}
}

// TestMCPAuthNotConfigured: пустой токен — fail closed: /mcp отвечает 503
// на любой метод даже с предъявленным токеном, /healthz остаётся открытым.
func TestMCPAuthNotConfigured(t *testing.T) {
	httpServer := newTestServerWithToken(t, "")

	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			resp := doRequest(t, mcpRequest(t, httpServer, method, "Bearer "+testToken))

			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
			}
			if body := readBody(t, resp); !strings.Contains(body, "auth not configured") {
				t.Errorf("body = %q, want it to contain %q", body, "auth not configured")
			}
		})
	}

	t.Run("healthz открыт", func(t *testing.T) {
		resp, err := http.Get(httpServer.URL + server.HealthPath)
		if err != nil {
			t.Fatalf("GET %s: %v", server.HealthPath, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})
}

// TestHealthzStaysOpen: health-check не закрыт авторизацией и не отдаёт
// challenge-заголовок.
func TestHealthzStaysOpen(t *testing.T) {
	httpServer := newTestServerWithToken(t, "secret")

	resp, err := http.Get(httpServer.URL + server.HealthPath)
	if err != nil {
		t.Fatalf("GET %s: %v", server.HealthPath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("WWW-Authenticate"); got != "" {
		t.Errorf("WWW-Authenticate = %q, want empty", got)
	}
}

// TestChallengeHeaderOnlyOn401: на успешном ответе challenge-заголовка нет.
func TestChallengeHeaderOnlyOn401(t *testing.T) {
	httpServer := newTestServerWithToken(t, "secret")

	resp := doRequest(t, mcpRequest(t, httpServer, http.MethodPost, "Bearer secret"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("WWW-Authenticate"); got != "" {
		t.Errorf("WWW-Authenticate = %q, want empty on 200", got)
	}
	if body := readBody(t, resp); !strings.Contains(body, "serverInfo") {
		t.Errorf("body = %q, want it to contain %q", body, "serverInfo")
	}
}

// TestTokenIsNotLogged: ни предъявленный, ни настроенный токен не попадают в лог.
func TestTokenIsNotLogged(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	httpServer := httptest.NewServer(server.NewHandler(newMCPServer(), logger, server.Options{AuthToken: testToken}))
	t.Cleanup(httpServer.Close)

	resp := doRequest(t, mcpRequest(t, httpServer, http.MethodPost, "Bearer secret"))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	if got := logs.String(); strings.Contains(got, "secret") {
		t.Errorf("лог содержит предъявленный токен: %q", got)
	}
	if got := logs.String(); strings.Contains(got, testToken) {
		t.Errorf("лог содержит настроенный токен: %q", got)
	}
}
