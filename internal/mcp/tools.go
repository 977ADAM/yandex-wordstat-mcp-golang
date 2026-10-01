package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
)

func RegisterTools(server *mcp.Server, client *wordstat.Client) {

	// ─── top_requests ──────────────────────────────────────
	type TopArgs struct {
		Phrase     string `json:"phrase" jsonschema:"required,Поисковая фраза (например, 'купить кофемашину')"`
		NumPhrases int    `json:"numPhrases,omitempty" jsonschema:"Количество записей (1..2000), по умолчанию 2000"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "top_requests",
		Description: "Популярные и связанные запросы по фразе за последние 30 дней с частотностью.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args TopArgs) (*mcp.CallToolResult, any, error) {
		res, err := client.TopRequests(ctx, args.Phrase, args.NumPhrases)
		if err != nil {
			return nil, nil, err
		}
		out := fmt.Sprintf("Фраза: %s\nВсего показов: %s\n\nПопулярные запросы:\n", args.Phrase, res.TotalCount)
		for _, p := range res.TopRequests {
			out += fmt.Sprintf("- %s: %s\n", p.Phrase, p.Count)
		}
		if len(res.Associations) > 0 {
			out += "\nПохожие запросы:\n"
			for _, p := range res.Associations {
				out += fmt.Sprintf("- %s: %s\n", p.Phrase, p.Count)
			}
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out}}}, nil, nil
	})

	// ─── dynamics ──────────────────────────────────────────
	type DynArgs struct {
		Phrase   string `json:"phrase" jsonschema:"required,Поисковая фраза"`
		Period   string `json:"period,omitempty" jsonschema:"Детализация: daily, weekly, monthly"`
		FromDate string `json:"fromDate,omitempty" jsonschema:"Начало (RFC3339)"`
		ToDate   string `json:"toDate,omitempty" jsonschema:"Конец (RFC3339)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "dynamics",
		Description: "Динамика частотности запроса во времени.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args DynArgs) (*mcp.CallToolResult, any, error) {
		res, err := client.Dynamics(ctx, args.Phrase, args.Period, args.FromDate, args.ToDate)
		if err != nil {
			return nil, nil, err
		}
		out := fmt.Sprintf("Динамика для «%s»:\n", args.Phrase)
		for _, p := range res.Series {
			out += fmt.Sprintf("%s: count=%s share=%s\n", p.Date, p.Count, p.Share)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out}}}, nil, nil
	})

	// ─── regions ───────────────────────────────────────────
	type RegArgs struct {
		Phrase     string `json:"phrase" jsonschema:"required,Поисковая фраза"`
		RegionMode string `json:"regionMode,omitempty" jsonschema:"Группировка: all, cities, regions"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "regions",
		Description: "Распределение спроса по регионам.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args RegArgs) (*mcp.CallToolResult, any, error) {
		res, err := client.Regions(ctx, args.Phrase, args.RegionMode)
		if err != nil {
			return nil, nil, err
		}
		out := fmt.Sprintf("Регионы для «%s»:\n", args.Phrase)
		for _, r := range res.Regions {
			out += fmt.Sprintf("regionId=%s count=%s share=%s affinity=%s\n",
				r.RegionID, r.Count, r.Share, r.AffinityIndex)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out}}}, nil, nil
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
		out := "Регионы:\n"
		var walk func(nodes []wordstat.RegionNode, depth int)
		walk = func(nodes []wordstat.RegionNode, depth int) {
			for _, n := range nodes {
				out += fmt.Sprintf("%*s%s (id=%s)\n", depth*2, "", n.Name, n.ID)
				walk(n.Children, depth+1)
			}
		}
		walk(res.Regions, 0)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out}}}, nil, nil
	})
}