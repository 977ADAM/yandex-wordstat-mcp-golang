package mymcp

import (
	"context"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// WordstatClient — то, что инструментам нужно от клиента Wordstat.
// Интерфейс (а не конкретный тип) позволяет подменять клиент в тестах.
type WordstatClient interface {
	TopRequests(ctx context.Context, params wordstat.TopParams) (*wordstat.TopRequestsResponse, error)
	Dynamics(ctx context.Context, params wordstat.DynamicsParams) (*wordstat.DynamicsResponse, error)
	Regions(ctx context.Context, phrase, regionMode string) (*wordstat.RegionsResponse, error)
	RegionsTree(ctx context.Context) (*wordstat.RegionsTreeResponse, error)
}

// RegisterTools регистрирует четыре инструмента Wordstat на MCP-сервере.
//
// Каждый инструмент возвращает и текст (для человека и клиентов без поддержки
// structured content), и типизированный результат: SDK выводит из него
// outputSchema и заполняет structuredContent. При сбое ответ содержит
// isError = true и структурированный конверт ошибки (ToolStatus), так что
// потребитель отличает сбой от «спроса нет» (HasData = false).
func RegisterTools(server *mcp.Server, client WordstatClient) {

	// ─── top_requests ──────────────────────────────────────
	type TopArgs struct {
		Phrase     string   `json:"phrase" jsonschema:"Поисковая фраза (например, 'купить кофемашину')"`
		NumPhrases int      `json:"numPhrases,omitempty" jsonschema:"Сколько фраз вернуть в requests (1..2000), по умолчанию 20. На число associations не влияет"`
		Regions    []string `json:"regions,omitempty" jsonschema:"Geo ID Яндекса (до 100): '213' — Москва, '1' — Москва и область, '225' — Россия. Пусто — вся Россия"`
		Devices    []string `json:"devices,omitempty" jsonschema:"Устройства (до 3): all, desktop, phone, tablet. Пусто — все устройства"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name: "top_requests",
		Description: "Популярные и связанные запросы по фразе за последние 30 дней. " +
			"requests — подмножества totalCount (суммировать их нельзя), associations — нет. " +
			"hasData=false означает, что спроса нет; это валидный ответ, а не ошибка.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args TopArgs) (*mcp.CallToolResult, TopRequestsOutput, error) {
		res, err := client.TopRequests(ctx, wordstat.TopParams{
			Phrase:     args.Phrase,
			NumPhrases: args.NumPhrases,
			Regions:    args.Regions,
			Devices:    args.Devices,
		})
		if err != nil {
			return errorCall(err), failedTopRequests(err), nil
		}

		out, err := topRequestsOutput(args.Phrase, res)
		if err != nil {
			return errorCall(err), failedTopRequests(err), nil
		}
		return textResult(formatTopRequests(args.Phrase, res)), out, nil
	})

	// ─── dynamics ──────────────────────────────────────────
	type DynArgs struct {
		Phrase   string   `json:"phrase" jsonschema:"Поисковая фраза"`
		Period   string   `json:"period,omitempty" jsonschema:"Детализация: daily, weekly, monthly (по умолчанию monthly)"`
		FromDate string   `json:"fromDate,omitempty" jsonschema:"Начало периода, RFC3339 или YYYY-MM-DD. Для monthly — первый день месяца, для weekly — понедельник. По умолчанию — 12 периодов назад"`
		ToDate   string   `json:"toDate,omitempty" jsonschema:"Конец периода, RFC3339 или YYYY-MM-DD. Для monthly — последний день месяца, для weekly — воскресенье. По умолчанию — последний завершённый период"`
		Regions  []string `json:"regions,omitempty" jsonschema:"Geo ID Яндекса (до 100): '213' — Москва, '1' — Москва и область, '225' — Россия. Пусто — вся Россия"`
		Devices  []string `json:"devices,omitempty" jsonschema:"Устройства (до 3): all, desktop, phone, tablet. Пусто — все устройства"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "dynamics",
		Description: "Динамика частотности запроса во времени (день/неделя/месяц) с фактическим окном в ответе.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args DynArgs) (*mcp.CallToolResult, DynamicsOutput, error) {
		res, err := client.Dynamics(ctx, wordstat.DynamicsParams{
			Phrase:   args.Phrase,
			Period:   args.Period,
			FromDate: args.FromDate,
			ToDate:   args.ToDate,
			Regions:  args.Regions,
			Devices:  args.Devices,
		})
		if err != nil {
			return errorCall(err), failedDynamics(err), nil
		}

		out, err := dynamicsOutput(args.Phrase, res)
		if err != nil {
			return errorCall(err), failedDynamics(err), nil
		}
		return textResult(formatDynamics(args.Phrase, res)), out, nil
	})

	// ─── regions ───────────────────────────────────────────
	type RegArgs struct {
		Phrase       string `json:"phrase" jsonschema:"Поисковая фраза"`
		RegionMode   string `json:"regionMode,omitempty" jsonschema:"Группировка: all, cities, regions (по умолчанию all)"`
		IncludeNames bool   `json:"includeNames,omitempty" jsonschema:"Добавить названия регионов (join со справочником list_regions, он кэшируется)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name: "regions",
		Description: "Распределение спроса по регионам за последние 30 дней (с индексом интереса), " +
			"отсортировано по убыванию count. includeNames=true добавляет названия регионов " +
			"(отдельный вызов справочника, дальше из кэша).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args RegArgs) (*mcp.CallToolResult, RegionsOutput, error) {
		res, err := client.Regions(ctx, args.Phrase, args.RegionMode)
		if err != nil {
			return errorCall(err), failedRegions(err), nil
		}

		var names map[string]string
		if args.IncludeNames {
			tree, err := client.RegionsTree(ctx)
			if err != nil {
				return errorCall(err), failedRegions(err), nil
			}
			names = regionNames(tree)
		}

		out, err := regionsOutput(args.Phrase, res, names)
		if err != nil {
			return errorCall(err), failedRegions(err), nil
		}
		return textResult(formatRegions(args.Phrase, res, names)), out, nil
	})

	// ─── list_regions ──────────────────────────────────────
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_regions",
		Description: "Справочник регионов (id → название) плоским списком с уровнем вложенности.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, RegionsTreeOutput, error) {
		res, err := client.RegionsTree(ctx)
		if err != nil {
			return errorCall(err), failedRegionsTree(err), nil
		}
		return textResult(formatRegionTree(res)), regionsTreeOutput(res), nil
	})
}

// textResult оборачивает текст в результат вызова инструмента.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}
