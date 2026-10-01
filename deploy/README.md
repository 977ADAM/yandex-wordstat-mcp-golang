# Деплой

## Прод: dev.coresmart.tech

| Что | Значение |
| --- | --- |
| Хост | `myserver` (77.83.119.18:65133), Ubuntu 24.04, x86_64 |
| Каталог | `/opt/yandex-wordstat-mcp-golang` |
| Контейнер | `yandex-wordstat-mcp-golang-wordstat-mcp-1` (образ `yandex-wordstat-mcp:local`) |
| Слушает | только `127.0.0.1:8080` |
| Endpoint для клиента | `https://dev.coresmart.tech/wordstat-mcp/mcp` |
| Health-check | `GET /wordstat-mcp/healthz` → `200 ok` |
| Авторизация | Angie: basic-auth (admin) **или** `X-API-Key` / `Authorization: Bearer <ключ>` из `/etc/angie/api_keys.map` |
| Секреты | `/opt/yandex-wordstat-mcp-golang/.env` (0600, вне git) |

TLS терминирует балансировщик выше по стеку; на самом хосте Angie слушает 80.

## Обновление

```bash
ssh myserver 'cd /opt/yandex-wordstat-mcp-golang && git pull && docker compose up -d --build'
```

## Проверка

```bash
# локально на сервере
ssh myserver 'curl -s -H "Host: dev.coresmart.tech" -H "X-API-Key: <ключ>" \
  http://127.0.0.1/wordstat-mcp/healthz'

# снаружи
curl -s -H "X-API-Key: <ключ>" https://dev.coresmart.tech/wordstat-mcp/healthz
```

## Правки Angie

Локация живёт внутри server-блока `dev.coresmart.tech` в
`/etc/angie/http.d/clustering.conf`; эталонный текст — [angie-wordstat-mcp.conf](angie-wordstat-mcp.conf).

Порядок: бэкап `clustering.conf.bak.<epoch>` → правка → `angie -t` → `angie -s reload`.
Откат: вернуть бэкап (или удалить локацию) и перезагрузить.

## Замечания

- Образ собирается прямо на сервере (`docker compose build`): хост x86_64, кросс-сборка не нужна.
- `restart: unless-stopped` — контейнер поднимается вместе с Docker.
- В контейнере `no .env file loaded` в логах — это норма: переменные приходят из compose, `.env` нужен только для локального `make run`.
- Сейчас на локали и на сервере используется один и тот же ключ Wordstat; для прода лучше отдельный service account.
