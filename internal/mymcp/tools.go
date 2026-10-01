package mymcp

import (
	"context"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// WordstatClient — то, что инструментам нужно от клиента Wordstat.
// Интерфейс (а не конкретный тип) позволяет подменять клиент в тестах.
type WordstatClient interface {
	TopRequests(ctx context.Context, phrase string, numPhrases int) (*wordstat.TopRequestsResponse, error)
	Dynamics(ctx context.Context, phrase, period, fromDate, toDate string) (*wordstat.DynamicsResponse, error)
	Regions(ctx context.Context, phrase, regionMode string) (*wordstat.RegionsResponse, error)
	RegionsTree(ctx context.Context) (*wordstat.RegionsTreeResponse, error)
}

// RegisterTools регистрирует четыре инструмента Wordstat на MCP-сервере.
func RegisterTools(server *mcp.Server, client WordstatClient) {

	// ─── top_requests ──────────────────────────────────────
	type TopArgs struct {
		Phrase     string `json:"phrase" jsonschema:"Поисковая фраза (например, 'купить кофемашину')"`
		NumPhrases int    `json:"numPhrases,omitempty" jsonschema:"Сколько фраз вернуть (1..2000), по умолчанию 20"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "top_requests",
		Description: "Популярные и связанные запросы по фразе за последние 30 дней с частотностью.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args TopArgs) (*mcp.CallToolResult, any, error) {
		res, err := client.TopRequests(ctx, args.Phrase, args.NumPhrases)
		if err != nil {
			return nil, nil, err
		}
		return textResult(formatTopRequests(args.Phrase, res)), nil, nil
	})

	// ─── dynamics ──────────────────────────────────────────
	type DynArgs struct {
		Phrase   string `json:"phrase" jsonschema:"Поисковая фраза"`
		Period   string `json:"period,omitempty" jsonschema:"Детализация: daily, weekly, monthly (по умолчанию monthly)"`
		FromDate string `json:"fromDate,omitempty" jsonschema:"Начало периода, RFC3339 или YYYY-MM-DD. Для monthly — первый день месяца, для weekly — понедельник. По умолчанию — 12 периодов назад"`
		ToDate   string `json:"toDate,omitempty" jsonschema:"Конец периода, RFC3339 или YYYY-MM-DD. Для monthly — последний день месяца, для weekly — воскресенье. По умолчанию — последний завершённый период"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "dynamics",
		Description: "Динамика частотности запроса во времени (день/неделя/месяц).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args DynArgs) (*mcp.CallToolResult, any, error) {
		res, err := client.Dynamics(ctx, args.Phrase, args.Period, args.FromDate, args.ToDate)
		if err != nil {
			return nil, nil, err
		}
		return textResult(formatDynamics(args.Phrase, res)), nil, nil
	})

	// ─── regions ───────────────────────────────────────────
	type RegArgs struct {
		Phrase     string `json:"phrase" jsonschema:"Поисковая фраза"`
		RegionMode string `json:"regionMode,omitempty" jsonschema:"Группировка: all, cities, regions (по умолчанию all)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "regions",
		Description: "Распределение спроса по регионам за последние 30 дней (с индексом интереса).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args RegArgs) (*mcp.CallToolResult, any, error) {
		res, err := client.Regions(ctx, args.Phrase, args.RegionMode)
		if err != nil {
			return nil, nil, err
		}
		return textResult(formatRegions(args.Phrase, res)), nil, nil
	})

	// ─── list_regions ──────────────────────────────────────
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_regions",
		Description: "Справочник регионов (id → название).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		res, err := client.RegionsTree(ctx)
		if err != nil {
			return nil, nil, err
		}
		return textResult(formatRegionTree(res)), nil, nil
	})
}

// textResult оборачивает текст в результат вызова инструмента.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}
