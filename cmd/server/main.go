package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/mymcp"
	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
	"github.com/joho/godotenv"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// defaultAddr — адрес прослушивания, переопределяется WORDSTAT_HTTP_ADDR.
	defaultAddr = ":8080"
	// mcpPath — endpoint Streamable HTTP транспорта.
	mcpPath = "/mcp"
	// healthPath — простой health-check для проб/балансировщика.
	healthPath = "/healthz"

	sessionTimeout  = 30 * time.Minute
	shutdownTimeout = 10 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	if err := godotenv.Load(); err != nil {
		slog.Info("no .env file loaded", "err", err)
	}

	apiKey := os.Getenv("WORDSTAT_API_KEY")
	folderID := os.Getenv("WORDSTAT_FOLDER_ID")
	if apiKey == "" || folderID == "" {
		return errors.New("WORDSTAT_API_KEY and WORDSTAT_FOLDER_ID must be set")
	}

	addr := os.Getenv("WORDSTAT_HTTP_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "yandex-wordstat-mcp",
		Version: "1.0.0",
	}, nil)

	mymcp.RegisterTools(server, wordstat.NewClient(apiKey, folderID))

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           newHandler(server, slog.Default()),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// ReadTimeout/WriteTimeout намеренно не заданы: standalone SSE-поток
		// и стриминговые ответы живут дольше одного запроса.
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("Сервер запустился", "addr", addr, "mcp", mcpPath, "health", healthPath)
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
		slog.Info("останавливаю сервер")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	return httpServer.Shutdown(shutdownCtx)
}

// newHandler собирает маршруты: MCP по mcpPath, health-check по healthPath.
func newHandler(server *mcp.Server, logger *slog.Logger) http.Handler {
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			Logger:         logger,
			SessionTimeout: sessionTimeout,
		},
	)

	mux := http.NewServeMux()
	mux.Handle(mcpPath, mcpHandler)
	mux.HandleFunc(healthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	return mux
}
