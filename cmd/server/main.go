// yandex-wordstat-mcp-golang/cmd/server/main.go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/mcp"
	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
)

func main() {
	apiKey := os.Getenv("WORDSTAT_API_KEY")
	folderID := os.Getenv("WORDSTAT_FOLDER_ID")
	if apiKey == "" || folderID == "" {
		log.Fatal("WORDSTAT_API_KEY and WORDSTAT_FOLDER_ID must be set")
	}

	client := wordstat.NewClient(apiKey, folderID)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "yandex-wordstat-mcp",
		Version: "1.0.0",
	}, nil)

	mcp.RegisterTools(server, client)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server error: %v", err)
	}
}


