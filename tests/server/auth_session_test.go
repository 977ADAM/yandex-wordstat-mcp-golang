package server_test

// Тесты авторизованной MCP-сессии: Bearer-токен должен доезжать до каждого
// запроса сессии (POST, standalone SSE GET и DELETE при закрытии), а при
// неверном токене мидлварь обязана отсечь запрос раньше, чем транспорт
// вспомнит про session id.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// sessionCallTimeout ограничивает каждый вызов клиента. Таймаут здесь не
// формальность: если обёртка challengeWriter потеряет Unwrap/Flush, поток
// Streamable HTTP залипнет, и без дедлайна прогон повиснет навсегда.
const sessionCallTimeout = 10 * time.Second

// toolsListRequest — тело tools/list для сырых контрольных запросов.
// Берём именно tools/list, а не initialize: повторный initialize на живой
// сессии имеет отдельную семантику (ре-инициализация), а tools/list безопасен
// и доказывает, что запрос дошёл до MCP-обработчика уже созданной сессии.
const toolsListRequest = `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`

// bearerRoundTripper добавляет Authorization: Bearer <token> в каждый запрос
// MCP-клиента. Транспорт использует HTTPClient и для POST, и для standalone
// SSE GET, и для DELETE при закрытии, поэтому токен нужен во всех трёх.
type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (rt *bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.Header.Set("Authorization", "Bearer "+rt.token)
	return rt.base.RoundTrip(cloned)
}

// sessionIDRecorder запоминает Mcp-Session-Id успешного ответа initialize,
// чтобы затем обратиться к уже созданной сессии сырым HTTP-клиентом.
//
// Пишем только первый id из ответа 200: запрос server/discover, которым
// клиент SDK v1.8.0 зондирует современный протокол, на stateful-транспорте
// завершается ошибкой и сессию не отдаёт.
type sessionIDRecorder struct {
	base http.RoundTripper

	mu sync.Mutex
	id string
}

func (rt *sessionIDRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK {
		if id := resp.Header.Get("Mcp-Session-Id"); id != "" {
			rt.mu.Lock()
			if rt.id == "" {
				rt.id = id
			}
			rt.mu.Unlock()
		}
	}
	return resp, nil
}

func (rt *sessionIDRecorder) sessionID() string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.id
}

// callContext выдаёт контекст на один вызов клиента; отмена — за тестом.
func callContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), sessionCallTimeout)
	t.Cleanup(cancel)

	return ctx
}

// TestAuthorizedMCPSession проходит полный цикл авторизованной сессии:
// initialize, tools/list, tools/call и Close (DELETE тоже под токеном).
func TestAuthorizedMCPSession(t *testing.T) {
	httpServer := newTestServerWithToken(t, testToken)

	transport := &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL + server.MCPPath,
		HTTPClient: &http.Client{
			Transport: &bearerRoundTripper{token: testToken, base: http.DefaultTransport},
			Timeout:   sessionCallTimeout,
		},
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "auth-session-test", Version: "0.0.0"}, nil)

	session, err := client.Connect(callContext(t), transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	list, err := session.ListTools(callContext(t), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(list.Tools) != 4 {
		t.Fatalf("tools/list вернул %d инструментов, want 4", len(list.Tools))
	}

	call, err := session.CallTool(callContext(t), &mcp.CallToolParams{
		Name:      "top_requests",
		Arguments: map[string]any{"phrase": "яндекс"},
	})
	if err != nil {
		t.Fatalf("tools/call top_requests: %v", err)
	}
	if call.IsError {
		t.Fatalf("tools/call вернул IsError: %+v", call.Content)
	}
	if call.StructuredContent == nil {
		t.Fatal("tools/call вернул пустой StructuredContent")
	}

	if err := session.Close(); err != nil {
		t.Fatalf("session.Close (DELETE под токеном): %v", err)
	}
}

// postMCPWithSession отправляет сырой POST к /mcp с заданными Authorization и
// Mcp-Session-Id. Таймауты те же, что у клиентских вызовов: дедлайн на контекст
// запроса и Timeout у http.Client. Тело ответа закрывается за тестом.
func postMCPWithSession(t *testing.T, client *http.Client, httpServer *httptest.Server, sessionID, authorization string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(callContext(t), http.MethodPost, httpServer.URL+server.MCPPath, strings.NewReader(toolsListRequest))
	if err != nil {
		t.Fatalf("new POST %s: %v", server.MCPPath, err)
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Mcp-Session-Id", sessionID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", server.MCPPath, err)
	}
	t.Cleanup(func() { resp.Body.Close() })

	return resp
}

// TestWrongTokenBeatsSessionID: при валидном session id запрос с неверным
// токеном получает 401 — мидлварь проверяет токен раньше, чем транспорт
// заглядывает в сессию.
func TestWrongTokenBeatsSessionID(t *testing.T) {
	httpServer := newTestServerWithToken(t, testToken)

	recorder := &sessionIDRecorder{base: http.DefaultTransport}
	transport := &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL + server.MCPPath,
		HTTPClient: &http.Client{
			Transport: &bearerRoundTripper{token: testToken, base: recorder},
			Timeout:   sessionCallTimeout,
		},
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "auth-session-test", Version: "0.0.0"}, nil)

	session, err := client.Connect(callContext(t), transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	sessionID := recorder.sessionID()
	if sessionID == "" {
		t.Fatal("initialize не вернул Mcp-Session-Id")
	}

	rawClient := &http.Client{Transport: recorder, Timeout: sessionCallTimeout}

	// Контрольный запрос: тот же session id, но верный токен. Без него 401
	// ниже неразличим: 401 на неверный токен выдаётся раньше любой проверки
	// сессии, поэтому он сработал бы и с пустым/чужим id. Контроль обязан
	// пройти сквозь мидлварь до MCP-обработчика — то есть вернуть 200 с
	// ответом tools/list, а не 401/404.
	control := postMCPWithSession(t, rawClient, httpServer, sessionID, "Bearer "+testToken)
	if control.StatusCode != http.StatusOK {
		t.Fatalf("контрольный POST с верным токеном и тем же Mcp-Session-Id: status = %d, want %d", control.StatusCode, http.StatusOK)
	}
	if body := readBody(t, control); !strings.Contains(body, "top_requests") {
		t.Errorf("контрольный tools/list: body = %q, want it to contain %q", body, "top_requests")
	}
	t.Logf("контрольный POST с верным токеном и тем же Mcp-Session-Id: status = %d", control.StatusCode)

	// Тот же самый session id, но неверный токен: теперь 401 доказывает
	// приоритет проверки токена над сессией, потому что контроль подтвердил,
	// что сессия с этим id жива.
	wrong := postMCPWithSession(t, rawClient, httpServer, sessionID, "Bearer wrong")
	if wrong.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST с неверным токеном и валидным Mcp-Session-Id: status = %d, want %d", wrong.StatusCode, http.StatusUnauthorized)
	}
	if body := readBody(t, wrong); !strings.Contains(body, "invalid token") {
		t.Errorf("body = %q, want it to contain %q", body, "invalid token")
	}
}
