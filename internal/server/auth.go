package server

import (
	"context"
	"crypto/subtle"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// Options — конфигурация HTTP-обвязки MCP-сервера.
type Options struct {
	// AuthToken — предвыданный Bearer-токен, которым защищён MCPPath.
	// Пустое значение переводит /mcp в fail-closed режим: 503 на любой запрос.
	AuthToken string
}

// userIDPreShared — идентичность владельца предвыданного токена. Заполняем
// осознанно: транспорт привязывает MCP-сессию к идентичности и это сохраняет
// защиту от перехвата сессии, если появятся отдельные токены на потребителя.
const userIDPreShared = "pre-shared"

// protectMCP закрывает MCP-обработчик Bearer-авторизацией.
//
// Пустой токен — ошибка конфигурации, а не открытый доступ: отвечаем 503,
// чтобы неверная настройка не выставляла эндпоинт наружу.
//
// 401 от auth.RequireBearerToken проходит через challengeWriter: SDK добавляет
// WWW-Authenticate только при заданных ResourceMetadataURL или Scopes, а у нас
// их нет, поэтому заголовок выставляет обёртка.
func protectMCP(next http.Handler, token string) http.Handler {
	if token == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "auth not configured", http.StatusServiceUnavailable)
		})
	}

	protected := auth.RequireBearerToken(verifyToken(token), &auth.RequireBearerTokenOptions{
		AllowMissingExpiration: true,
	})(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protected.ServeHTTP(&challengeWriter{ResponseWriter: w}, r)
	})
}

// verifyToken сравнивает предъявленный токен с настроенным за постоянное время.
func verifyToken(token string) auth.TokenVerifier {
	return func(_ context.Context, presented string, _ *http.Request) (*auth.TokenInfo, error) {
		if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
			// Сентинел возвращаем как есть: %w-обёртка дала бы тело
			// «invalid token: invalid token», а любая другая ошибка — 500.
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{UserID: userIDPreShared}, nil
	}
}

// challengeWriter добавляет WWW-Authenticate: Bearer к ответам 401 и
// пробрасывает возможности нижележащего writer'а.
//
// Unwrap и Flush обязательны: SDK флашит SSE через http.NewResponseController,
// без них контроллер не доберётся до настоящего writer'а, и поток Streamable
// HTTP залипнет до заполнения буфера.
type challengeWriter struct {
	http.ResponseWriter
}

func (w *challengeWriter) WriteHeader(code int) {
	if code == http.StatusUnauthorized && w.Header().Get("WWW-Authenticate") == "" {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *challengeWriter) Write(b []byte) (int, error) {
	return w.ResponseWriter.Write(b)
}

// Unwrap отдаёт вложенный writer для http.NewResponseController.
func (w *challengeWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Flush делегирует флаш нижележащему writer'у, если тот его поддерживает.
func (w *challengeWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
