# Bearer-авторизация MCP-сервера Wordstat — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use subagent-driven-development (recommended) or executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Закрыть `/mcp` статическим Bearer-токеном на уровне сервиса, оставить `/healthz` открытым и убрать общий basic-auth из цепочки доступа к MCP.

**Architecture:** Мидлварь `protectMCP` из `internal/server/auth.go` оборачивает только маршрут `/mcp`; она использует `auth.RequireBearerToken` из MCP SDK с нашим `TokenVerifier` (константное сравнение с `WORDSTAT_MCP_TOKEN`), обёрткой `challengeWriter` для заголовка `WWW-Authenticate` и отдельной веткой `503` при пустом токене. Токен читается один раз при старте в `cmd/server/main.go` и передаётся через `server.Options`.

**Tech Stack:** Go 1.27, `github.com/modelcontextprotocol/go-sdk` v1.8.0 (пакеты `mcp` и `auth`), Streamable HTTP, `httptest` для тестов, Docker Compose для деплоя.

**Spec:** [docs/superpowers/specs/2026-10-12-wordstat-mcp-bearer-auth-design.md](../specs/2026-10-12-wordstat-mcp-bearer-auth-design.md)

## Global Constraints

- Версия контракта после задачи 4: `serverInfo.version = "1.3.0"` (в `cmd/server/main.go`).
- Новая переменная окружения — ровно `WORDSTAT_MCP_TOKEN`, других имён не вводить.
- В `docker-compose.yml` токен пробрасывается как `${WORDSTAT_MCP_TOKEN:-}`; `WORDSTAT_API_KEY` и `WORDSTAT_FOLDER_ID` сохраняют `${…:?…}`.
- `auth.RequireBearerTokenOptions` всегда с `AllowMissingExpiration: true` и без `Scopes`/`ResourceMetadataURL`.
- Верификатор возвращает только `auth.ErrInvalidToken` (без `%w`-обёртки) либо `nil`-ошибку; иначе SDK ответит `500`.
- Токен не логируется ни в одной ветке и не попадает в git (`.env` уже в `.gitignore`).
- Новых зависимостей не добавлять.
- Тесты — только в `tests/`, чёрный ящик (`package server_test`), рядом с кодом `_test.go` не создавать.
- Команды сборки/тестов в этой песочнице: `export GOCACHE=/tmp/gocache-dsh` (доступ к общему кэшу Go закрыт).
- Каждая задача заканчивается коммитом; дерево после каждой задачи обязано собираться и проходить `make test`.

## Review Focus

- `Authorization: bearer <верный токен>` в нижнем регистре схемы — должен проходить: SDK сравнивает схему регистронезависимо. Тест в задаче 1.
- Лишние пробелы вокруг верного токена (`Bearer   <токен>`) — должны проходить: SDK разбивает заголовок по пробелам. Тест в задаче 1.
- Заголовок с двумя значениями (`Bearer a, Bearer b`) — должен давать `401`, а не «склеивать» токен. Тест в задаче 1.
- Запрос с верным session id, но неверным токеном — `401`, то есть токен проверяется раньше сессии. Тест в задаче 2.
- Авторизованный SSE-стрим не должен залипать: обёртка обязана реализовать `Unwrap() http.ResponseWriter` и `Flush()`, иначе `http.NewResponseController.Flush()` вернёт `ErrNotSupported` и события не уйдут клиенту. Тест в задаче 2 (с таймаутом контекста).
- `GET /mcp` без авторизации (standalone SSE) — `401`, а не пустой поток. Тест в задаче 1.
- `/healthz` при пустом токене — `200` (иначе балансировщик снимет контейнер и никто не увидит причину `503`). Тест в задаче 1.

---

### Task 1: Мидлварь и `Options` в `internal/server`

**Files:**
- Create: `internal/server/auth.go`
- Modify: `internal/server/server.go`
- Modify: `tests/server/server_test.go` — `newTestServer` (строка ~55), `TestNewTimeouts` (строка ~152), **удалить `TestStreamableHTTPTransport`** (строки 96–145): он ходит в `/mcp` без заголовка и после этой задачи получит `401`; его покрытие переезжает в задачу 2
- Modify: `cmd/server/main.go:51` (передать `server.Options{}`, чтобы дерево собиралось)
- Test: `tests/server/auth_test.go`

**Interfaces:**
- Consumes: `mcp.Server`, `auth.RequireBearerToken`, `auth.TokenVerifier`, `auth.ErrInvalidToken`, `auth.TokenInfo`.
- Produces:
  - `type Options struct { AuthToken string }`
  - `func NewHandler(mcpServer *mcp.Server, logger *slog.Logger, opts Options) http.Handler`
  - `func New(addr string, mcpServer *mcp.Server, logger *slog.Logger, opts Options) *http.Server`
  - `func Run(ctx context.Context, addr string, mcpServer *mcp.Server, logger *slog.Logger, opts Options) error`
  - константа `MCPPath`/`HealthPath` без изменений;
  - тестовая константа `testToken = "test-token"` в `tests/server/auth_test.go` — её использует и `server_test.go`, и задача 2 (один пакет `server_test`).

- [ ] **Step 1: Написать падающий тест** — `tests/server/auth_test.go`:

```go
func TestMCPRequiresBearer(t *testing.T) {
	// таблица: method × заголовок; ожидание 401 и WWW-Authenticate: Bearer
	//   "нет заголовка"            → 401, тело содержит "no bearer token"
	//   "другая схема" Basic abc   → 401
	//   "пустое значение" Bearer   → 401
	//   "неверный токен"           → 401, тело содержит "invalid token"
	//   "две схемы"  Bearer a, Bearer b → 401
	//   "мусорный 512 байт"        → 401, без паники
	// методы: POST, GET, DELETE
}

func TestBearerSchemeIsCaseInsensitive(t *testing.T) {
	// POST /mcp, "authorization: bearer <верный токен>", initialize → 200, тело содержит "serverInfo"
}

func TestBearerToleratesExtraSpaces(t *testing.T) {
	// POST /mcp, "Authorization: Bearer   <верный токен>", initialize → 200
}

func TestMCPAuthNotConfigured(t *testing.T) {
	// Options{AuthToken: ""}: POST/GET/DELETE /mcp → 503, тело "auth not configured"
	// GET /healthz → 200
}

func TestHealthzStaysOpen(t *testing.T) {
	// Options{AuthToken: "secret"}: GET /healthz без заголовка → 200, нет WWW-Authenticate
}

func TestChallengeHeaderOnlyOn401(t *testing.T) {
	// Options{AuthToken: "secret"}: initialize с верным токеном → 200 и нет WWW-Authenticate
}

func TestTokenIsNotLogged(t *testing.T) {
	// slog в bytes.Buffer: запрос с неверным токеном "secret" → буфер не содержит "secret"
}
```

Хелпер: `newTestServerWithToken(t *testing.T, token string) *httptest.Server` через `server.NewHandler(newMCPServer(), logger, server.Options{AuthToken: token})`.

- [ ] **Step 2: Убедиться, что тесты падают**

Run: `export GOCACHE=/tmp/gocache-dsh && go test ./tests/server/ -run 'Bearer|Auth|Healthz|Challenge|Logged' -v`
Expected: FAIL — компиляция не проходит (`too many arguments in call to server.NewHandler`).

- [ ] **Step 3: Реализовать `internal/server/auth.go`**

Сигнатуры: `type Options struct{ AuthToken string }`, `func protectMCP(next http.Handler, token string) http.Handler`, `func verifyToken(token string) auth.TokenVerifier`, тип `challengeWriter` и константа `userIDPreShared = "pre-shared"`. У `challengeWriter` четыре метода: `WriteHeader(code int)` — при `401` и пустом `WWW-Authenticate` выставляет `Bearer`, затем делегирует; `Write` — делегирует; `Unwrap() http.ResponseWriter` — возвращает вложенный writer (нужен `http.NewResponseController` для SSE); `Flush()` — делегирует `Flush` вложенного writer'а, если тот его поддерживает. Без `Unwrap`/`Flush` ломается стриминг — это проверяется тестом задачи 2.

Ветка пустого токена — обработчик, отдающий `http.Error(w, "auth not configured", http.StatusServiceUnavailable)` (это `503`, не `500`). Сравнение — `subtle.ConstantTimeCompare([]byte(presented), []byte(token))`; несовпадение → `auth.ErrInvalidToken` без обёртки; совпадение → `&auth.TokenInfo{UserID: userIDPreShared}`.

- [ ] **Step 4: Подключить мидлварь в `internal/server/server.go`**

`NewHandler`, `New`, `Run` получают последним параметром `opts Options`; `mux.Handle(MCPPath, protectMCP(mcpHandler, opts.AuthToken))`; `HealthPath` регистрируется без обёртки.

- [ ] **Step 5: Обновить вызовы, чтобы дерево собиралось**

`cmd/server/main.go` → `server.Run(ctx, addr, mcpServer, slog.Default(), server.Options{})`.
`tests/server/server_test.go`: `newTestServer` → `server.NewHandler(newMCPServer(), logger, server.Options{AuthToken: testToken})`; `TestNewTimeouts` → `server.New(":0", newMCPServer(), logger, server.Options{})`; **удалить `TestStreamableHTTPTransport` целиком** — авторизованная версия появится в задаче 2. В `tests/server/auth_test.go` объявить `const testToken = "test-token"`.

- [ ] **Step 6: Прогнать тесты**

Run: `export GOCACHE=/tmp/gocache-dsh && go test ./... -count=1`
Expected: PASS везде. В `tests/server/server_test.go` после шага 5 не остаётся запросов к `/mcp` без заголовка: `TestHealthEndpoint` и `TestUnknownPathIsNotFound` ходят в `/healthz` и `/`, а `TestNewTimeouts` сервер не запускает.

- [ ] **Step 7: Commit**

```bash
git add internal/server/auth.go internal/server/server.go cmd/server/main.go tests/server/auth_test.go tests/server/server_test.go
git commit -m "feat(server): Bearer-мидлварь для /mcp с fail-closed 503"
```

---

### Task 2: Авторизованная MCP-сессия и приоритет токена над сессией

**Files:**
- Test: `tests/server/auth_session_test.go`

**Interfaces:**
- Consumes: `server.NewHandler`, `server.Options{AuthToken}`, `server.MCPPath`, `mymcp.RegisterTools`, `mcp.NewClient`, `mcp.StreamableClientTransport`; хелперы того же пакета — `newMCPServer()` и `testToken` из `tests/server/server_test.go` и `tests/server/auth_test.go`.
- Produces: ничего для других задач (только регрессионное покрытие).

- [ ] **Step 1: Написать падающий тест**

```go
func TestAuthorizedMCPSession(t *testing.T) {
	// RoundTripper добавляет "Authorization: Bearer <token>" в каждый запрос
	// client := &mcp.Client{...}; transport := &mcp.StreamableClientTransport{
	//     Endpoint: srv.URL + server.MCPPath,
	//     HTTPClient: &http.Client{Transport: bearerRoundTripper{token: token}},
	// }
	// ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second); defer cancel()
	// initialize → session.ListTools() == 4 инструмента
	// session.CallTool("top_requests", {"phrase": "яндекс"}) → нет IsError, есть StructuredContent
	// session.Close() → без ошибки (DELETE тоже авторизован)
	// таймаут обязателен: если обёртка потеряет Unwrap/Flush, поток залипнет и тест
	// упадёт по дедлайну, а не повиснет
}

func TestWrongTokenBeatsSessionID(t *testing.T) {
	// сессия создана с верным токеном → берём Mcp-Session-Id из ответа initialize
	// POST /mcp с тем же session id и заголовком Bearer wrong → 401
}
```

- [ ] **Step 2: Прогнать и убедиться, что тест зелёный на реализации задачи 1**

Run: `export GOCACHE=/tmp/gocache-dsh && go test ./tests/server/ -run 'Session|WrongToken' -v`
Expected: PASS. Если `Close()` возвращает ошибку — проверить, что RoundTripper навешан на `HTTPClient` (транспорт использует его для POST/GET/DELETE).

- [ ] **Step 3: Commit**

```bash
git add tests/server/auth_session_test.go
git commit -m "test(server): MCP-сессия под Bearer и приоритет токена над session id"
```

---

### Task 3: Чтение токена в `cmd/server/main.go`

**Files:**
- Modify: `cmd/server/main.go` (рядом с чтением `WORDSTAT_HTTP_ADDR`, около строк 30–51)

**Interfaces:**
- Consumes: `server.Options`, `server.Run`.
- Produces: поведение «пустой токен → WARN при старте».

- [ ] **Step 1: Реализовать**

`token := os.Getenv("WORDSTAT_MCP_TOKEN")`; если пусто — `slog.Warn("WORDSTAT_MCP_TOKEN is empty: /mcp будет отвечать 503, пока токен не задан")`; передать `server.Options{AuthToken: token}` в `server.Run`. В лог не выводить значение токена ни при каком исходе.

- [ ] **Step 2: Проверить поведение без токена**

Run: `export GOCACHE=/tmp/gocache-dsh && make build && WORDSTAT_MCP_TOKEN= WORDSTAT_HTTP_ADDR=127.0.0.1:18090 ./bin/yandex-wordstat-mcp &` затем
`curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18090/mcp` → `503`,
`curl -s http://127.0.0.1:18090/healthz` → `ok`, в логе одна строка `WARN`.
Остановить: `pkill -f 'yandex-wordstat-mcp$'`.

- [ ] **Step 3: Проверить поведение с токеном**

Run: то же с `WORDSTAT_MCP_TOKEN=local-test-token`; ожидается `401` без заголовка и `200` + `serverInfo` при `Authorization: Bearer local-test-token` на POST `initialize`.

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(server): WORDSTAT_MCP_TOKEN из окружения с fail-closed предупреждением"
```

---

### Task 4: Compose, версия контракта 1.3.0

**Files:**
- Modify: `docker-compose.yml` (секция `environment`)
- Modify: `cmd/server/main.go` (Version)

**Interfaces:**
- Consumes: ничего.
- Produces: переменная окружения для контейнера; версия `1.3.0`.

- [ ] **Step 1: Добавить переменную в compose**

`WORDSTAT_MCP_TOKEN: ${WORDSTAT_MCP_TOKEN:-}` — именно `:-`; переменные Wordstat оставить с `:?`.

- [ ] **Step 2: Поднять версию**

`Version: "1.3.0"` в `mcp.NewServer(&mcp.Implementation{…})`.

- [ ] **Step 3: Проверить**

Run: `docker compose config --quiet && echo ok` → `ok`;
`WORDSTAT_MCP_TOKEN= docker compose config | grep -A1 WORDSTAT_MCP_TOKEN` → переменная присутствует и пустая;
`export GOCACHE=/tmp/gocache-dsh && go build ./... && grep -n '1\.3\.0' cmd/server/main.go`.

- [ ] **Step 4: Commit**

```bash
git add docker-compose.yml cmd/server/main.go
git commit -m "chore: проброс WORDSTAT_MCP_TOKEN и версия контракта 1.3.0"
```

---

### Task 5: Деплой, Angie, живая проверка (только по подтверждению владельца)

**Files:**
- Modify (на сервере, вне git): `/etc/angie/http.d/clustering.conf`, `/opt/yandex-wordstat-mcp-golang/.env`

**Interfaces:**
- Consumes: образ из задачи 4.
- Produces: рабочий прод-эндпоинт под Bearer.

- [ ] **Step 1: Дождаться явного подтверждения владельца** — деплой и правка Angie без него не выполняются.

- [ ] **Step 2: Выдать токен и положить на сервер**

`openssl rand -hex 32` → добавить `WORDSTAT_MCP_TOKEN=<значение>` в `/opt/yandex-wordstat-mcp-golang/.env` (файл `600 root:root`), значение владельцу передать вне git.

- [ ] **Step 3: Обновить код и контейнер**

`ssh myserver 'cd /opt/yandex-wordstat-mcp-golang && git pull --ff-only && docker compose up -d --build'`.

- [ ] **Step 4: Правка Angie** — бэкап `clustering.conf.bak.<epoch>`, внутри `location ^~ /wordstat-mcp/` добавить `auth_basic off;` и `auth_request off;`, затем `angie -t && angie -s reload`.

- [ ] **Step 5: Живая проверка**

```
без токена                          → 401
с токеном: initialize               → serverInfo.version = 1.3.0
с токеном: tools/call top_requests  → 200, structuredContent заполнен
curl -u admin:123 /wordstat-mcp/healthz → 200 (healthz открыт)
curl -u admin:123 /wordstat-mcp/mcp     → 401 (basic-auth больше не даёт доступ)
```

- [ ] **Step 6: Откат при проблеме** — вернуть бэкап Angie + `angie -s reload`, затем `docker compose up -d` с прежним образом; токен из `.env` удалить.
