// Package server собирает HTTP-обвязку MCP-сервера Wordstat:
// Streamable HTTP транспорт, health-check и graceful shutdown.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// DefaultAddr — адрес прослушивания по умолчанию.
	DefaultAddr = ":8080"
	// MCPPath — endpoint Streamable HTTP транспорта.
	MCPPath = "/mcp"
	// HealthPath — простой health-check для проб и балансировщика.
	HealthPath = "/healthz"

	// SessionTimeout — время жизни простаивающей MCP-сессии.
	SessionTimeout = 30 * time.Minute
	// ShutdownTimeout — сколько ждём завершения текущих запросов при остановке.
	ShutdownTimeout = 10 * time.Second
)

// NewHandler собирает маршруты: MCP по MCPPath, health-check по HealthPath.
func NewHandler(mcpServer *mcp.Server, logger *slog.Logger) http.Handler {
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return mcpServer },
		&mcp.StreamableHTTPOptions{
			Logger:         logger,
			SessionTimeout: SessionTimeout,
		},
	)

	mux := http.NewServeMux()
	mux.Handle(MCPPath, mcpHandler)
	mux.HandleFunc(HealthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	return mux
}

// New создаёт http.Server для MCP-сервера.
//
// ReadTimeout/WriteTimeout намеренно не заданы: standalone SSE-поток и
// стриминговые ответы живут дольше одного запроса, и общий WriteTimeout
// обрывал бы их.
func New(addr string, mcpServer *mcp.Server, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           NewHandler(mcpServer, logger),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// Run поднимает сервер и работает до отмены ctx, после чего мягко
// останавливается, давая текущим запросам завершиться.
func Run(ctx context.Context, addr string, mcpServer *mcp.Server, logger *slog.Logger) error {
	httpServer := New(addr, mcpServer, logger)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("Сервер запустился", "addr", addr, "mcp", MCPPath, "health", HealthPath)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("останавливаю сервер")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	return httpServer.Shutdown(shutdownCtx)
}
