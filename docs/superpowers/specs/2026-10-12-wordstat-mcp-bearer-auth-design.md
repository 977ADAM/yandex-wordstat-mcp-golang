# Авторизация MCP-сервера Wordstat: pre-shared Bearer

- **Дата:** 2026-10-12
- **Статус:** дизайн согласован владельцем, ожидает ревью спеки
- **Версия контракта после внедрения:** `serverInfo.version = 1.3.0`
- **Репозиторий:** `yandex-wordstat-mcp-golang`

## 1. Цель и критерии успеха

Закрыть MCP-endpoint Bearer-токеном на уровне сервиса и убрать общий пароль `admin/123` из
цепочки доступа к MCP.

Критерии успеха:

1. Запрос к `/mcp` без заголовка `Authorization: Bearer <токен>` получает `401`.
2. Запрос с верным токеном проходит: `initialize`, `tools/list`, `tools/call` работают.
3. `/healthz` отвечает `200` без авторизации — балансировщик видит контейнер.
4. Если `WORDSTAT_MCP_TOKEN` не задан, `/mcp` отвечает `503`, а не открывает доступ.
5. Токен нигде не логируется и не попадает в git.
6. `make test` зелёный; покрытие не ниже текущего (83.7%).

## 2. Принятые решения

| # | Решение | Обоснование |
| --- | --- | --- |
| 1 | Авторизация — **статические Bearer-токены**, без authorization server | Владелец выбрал минимальный по объёму вариант: AS не нужен, потребитель один |
| 2 | Основной барьер — **сервис**, а не Angie | Один механизм вместо двух секретов; уходит общий `admin/123` |
| 3 | **Один** токен в переменной окружения | Минимум движущихся частей; ротация = замена значения + перезапуск |
| 4 | Пустой токен → **503 на `/mcp`** | Fail closed: ошибка конфигурации не открывает эндпоинт и не вызывает крашлуп |
| 5 | **PRM не отдаём**, AS нет | PRM по спеке обязан содержать `authorization_servers` хотя бы с одним AS — выдумывать его мы не станем (см. §3) |

## 3. Осознанные отклонения от спецификации MCP

Источник требований: [MCP 2025-06-18, Authorization](https://modelcontextprotocol.io/specification/2025-06-18/basic/authorization).

| Требование спеки | Что делаем | Обоснование и риск |
| --- | --- | --- |
| `MCP servers MUST implement OAuth 2.0 Protected Resource Metadata (RFC 9728)`; в документе `MUST` быть `authorization_servers` с хотя бы одним AS | PRM не реализуем; вместо этого `401` + `WWW-Authenticate: Bearer` | Честного AS нет. Метадата без AS вводила бы клиентов в заблуждение (discovery упирается в тупик). Риск: клиент, ожидающий OAuth-discovery, не сможет авторизоваться сам |
| Токен `MUST` валидироваться по audience (RFC 8707) | Неприменимо: токен предвыдан и не несёт клеймов | Привязка к ресурсу обеспечивается конфигурацией: секрет известен только сервису и владельцу, наружу не транслируется (token passthrough отсутствует) |
| Интерактивные клиенты сами проходят OAuth-flow | Токен выдаётся владельцем вне сервиса | Риск: для Claude/ChatGPT потребуется ручная настройка. Смягчение — шов `auth.TokenVerifier` (§6) для перехода на внешний AS или мини-AS без переделки транспорта |

Авторизация в MCP в целом **OPTIONAL**: спека требует соответствия, только если реализация
поддерживает авторизацию. Мы реализуем защиту ресурса вне OAuth-флоу и фиксируем это явно.

## 4. Конфигурация

- **`WORDSTAT_MCP_TOKEN`** — единственная новая переменная окружения.
  - локально: `.env` (в git не попадает, уже в `.gitignore`);
  - прод: `/opt/yandex-wordstat-mcp-golang/.env`, права `600 root:root`;
  - в `docker-compose.yml` пробрасывается как `${WORDSTAT_MCP_TOKEN:-}` — **без** `:?`.
    Это принципиально: `:?` заставляет `docker compose` отказаться запускать контейнер при
    пустом значении, и тогда выбранное поведение «стартовать и отвечать `503`» было бы
    недостижимо, а `/healthz` — недоступен. Переменные Wordstat (`WORDSTAT_API_KEY`,
    `WORDSTAT_FOLDER_ID`) сохраняют `:?`: без них сервис неработоспособен в принципе.
- Генерация: `openssl rand -hex 32` (не менее 32 байт энтропии).
- Сравнение — `subtle.ConstantTimeCompare`; значение токена не логируется.
- `serverInfo.version` → `1.3.0`.
- Скоупы не используются: `Scopes` в `auth.RequireBearerTokenOptions` не задаём, `403` при этом не возникает.

## 5. Поведение HTTP

| Ситуация | Ответ |
| --- | --- |
| Заголовка нет, схема не `Bearer`, значение пустое | `401`, `WWW-Authenticate: Bearer`, тело `no bearer token` (текст формирует SDK) |
| Токен не совпадает | `401`, `WWW-Authenticate: Bearer`, тело `invalid token` |
| Токен совпадает | Запрос обрабатывается; в контекст кладётся `auth.TokenInfo{UserID: "pre-shared"}`; в опциях верификатора `AllowMissingExpiration: true` |
| `WORDSTAT_MCP_TOKEN` пуст | `/mcp` → `503 auth not configured` (любой HTTP-метод); при старте одна строка `WARN` |
| `/healthz` (при любом значении токена) | `200 ok`, без авторизации |

Заголовок `WWW-Authenticate: Bearer` добавляем сами: `auth.RequireBearerToken` формирует его
только когда задан `ResourceMetadataURL` или непустые `Scopes`, а у нас нет ни того, ни
другого (PRM не отдаём, scopes не используем). Делается это обёрткой над
`http.ResponseWriter`, которая выставляет заголовок только при `WriteHeader(401)` — на
успешных ответах challenge-заголовка быть не должно.

`AllowMissingExpiration: true` обязателен: без него SDK отклоняет токен без непустого
`TokenInfo.Expiration` («token missing expiration», 401), а у предвыданного токена срока
годности нет — иначе `401` получали бы **все** запросы.

`UserID` заполняем осознанно: `mcp.StreamableHTTPHandler` привязывает MCP-сессию к
идентичности (`streamable.go:713` и `:569`), и это сохраняет защиту от перехвата сессии,
если позже появятся отдельные токены на потребителя.

## 6. Код

Новое — `internal/server/auth.go`:

- `Options{AuthToken string}` — конфигурация обвязки;
- `protectMCP(next http.Handler, token string) http.Handler` — единая точка:
  - `token == ""` → обработчик, отвечающий `503 auth not configured`;
  - иначе `auth.RequireBearerToken(verifyToken(token), opts)(next)`, где `opts` —
    `auth.RequireBearerTokenOptions{AllowMissingExpiration: true}`;
- `verifyToken(token string) auth.TokenVerifier` — сравнение `subtle.ConstantTimeCompare`,
  при несовпадении `fmt.Errorf("%w: invalid token", auth.ErrInvalidToken)` (SDK превратит
  это в `401`), при совпадении `&auth.TokenInfo{UserID: userIDPreShared}`;
- `challengeWriter` — обёртка `http.ResponseWriter`, добавляющая
  `WWW-Authenticate: Bearer` при `401` (см. §5);
- константа `userIDPreShared = "pre-shared"`.

Изменения:

- `internal/server/server.go`: `NewHandler`, `New`, `Run` получают `Options`; `protectMCP`
  навешивается **только** на маршрут `MCPPath`, `HealthPath` остаётся открытым;
- `cmd/server/main.go`: читает `WORDSTAT_MCP_TOKEN`, логирует `WARN` при пустом значении,
  передаёт `Options` в `server.Run`;
- `docker-compose.yml`: переменная `WORDSTAT_MCP_TOKEN`;
- Angie на сервере (вручную, с бэкапом и `angie -t`): внутри `location ^~ /wordstat-mcp/`
  добавить `auth_basic off;` и `auth_request off;` — так же, как уже сделано для `/health`;
  остальные локации домена не трогаем.

## 7. Тесты (`tests/server`, чёрный ящик)

1. Нет заголовка → `401`, есть `WWW-Authenticate: Bearer`, в нём **нет** `resource_metadata`.
2. Неверный токен → `401`.
3. `Authorization` с другой схемой или пустым значением → `401`.
4. Верный токен → `initialize`, `tools/list`, `tools/call` проходят по Streamable HTTP
   (тестовый клиент добавляет заголовок через `RoundTripper` в
   `StreamableClientTransport.HTTPClient`).
5. Пустой токен в конфиге → `/mcp` `503`, `/healthz` `200`.
6. `/healthz` без заголовка при заданном токене → `200`, и в ответе **нет**
   `WWW-Authenticate` (заголовок только у `401`).
7. Токен не появляется в логах (проверка через `slog` в буфер).
8. Мусорный/очень длинный заголовок → `401`, без паники.

Существующие тесты `tests/server`, вызывающие `NewHandler`, обновляются под новую сигнатуру.

## 8. Приёмка

- `make test` зелёный, покрытие не ниже 83.7%.
- Живая проверка после деплоя (по отдельному подтверждению владельца):
  без токена → `401`; с токеном `initialize` и `tools/call` работают; `curl -u admin:123`
  больше не даёт доступа к `/wordstat-mcp/`; `/healthz` → `200`.
- Ротация: заменить `WORDSTAT_MCP_TOKEN` в `.env` на сервере и выполнить
  `docker compose up -d`; конфиг Angie при ротации не меняется.

## 9. Вне области

Authorization server и всё, что с ним связано: `/authorize`, `/token`, DCR, JWKS, refresh
tokens, scopes, PRM. Изменения в других приложениях домена `dev.coresmart.tech`.
Шифрование токена в конфиге. Rate limiting и IP-allowlist.

## 10. Риски

- Токен лежит в env: пользователь с root на хосте увидит его через `docker inspect`.
  Принято владельцем при выборе варианта «один токен в env».
- Интерактивные клиенты не получат токен сами — нужна ручная настройка.
- После отключения Angie-auth единственная защита `/mcp` — сервисная мидлварь; поэтому
  fail-closed поведение (§5) и тесты на `401`/`503` обязательны.

## 11. План работ (для передачи в writing-plans)

1. `internal/server/auth.go` + `Options` и сигнатуры `NewHandler`/`New`/`Run`.
2. Тесты `tests/server/auth_test.go` + обновление существующих тестов сервера.
3. `cmd/server/main.go` — чтение переменной и `WARN`.
4. `docker-compose.yml` — переменная; версия `1.3.0`.
5. Правка Angie на сервере, деплой, живая проверка — по подтверждению владельца.
