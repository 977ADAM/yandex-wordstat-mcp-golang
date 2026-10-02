package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/mymcp"
	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/server"
	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
	"github.com/joho/godotenv"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
		addr = server.DefaultAddr
	}

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "yandex-wordstat-mcp",
		Version: "1.1.0",
	}, nil)

	mymcp.RegisterTools(mcpServer, wordstat.NewClient(apiKey, folderID))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return server.Run(ctx, addr, mcpServer, slog.Default())
}
