package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/mymcp"
	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
	"github.com/joho/godotenv"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("no .env file loaded: %v", err)
	}

	apiKey := os.Getenv("WORDSTAT_API_KEY")
	folderID := os.Getenv("WORDSTAT_FOLDER_ID")
	if apiKey == "" || folderID == "" {
		log.Fatal("WORDSTAT_API_KEY and WORDSTAT_FOLDER_ID must be set")
	}
	log.Print("Сервер запустился")

	client := wordstat.NewClient(apiKey, folderID)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "yandex-wordstat-mcp",
		Version: "1.0.0",
	}, nil)

	mymcp.RegisterTools(server, client)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
