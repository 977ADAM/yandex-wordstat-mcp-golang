package mymcp

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Типы ниже описывают структурированный результат инструментов: SDK выводит из
// них outputSchema и заполняет structuredContent. Текстовое представление
// (format.go) остаётся для человека и для клиентов без поддержки structured
// content.
//
// Числа: API отдаёт int64 строкой, а double — числом, поэтому при конвертации
// count/totalCount разбираются в int64 (см. parseCount), а share и
// affinityIndex остаются float64.
//
// Ошибки: при сбое инструмент возвращает isError = true и тот же тип выхода, но
// с заполненным ToolStatus (поля данных при этом нулевые). Потребитель отличает
// «нет данных» (HasData = false, Code пустой) от сбоя (Code заполнен).

// ToolStatus — конверт ошибки инструмента. Встраивается в каждый выходной тип:
// на успехе поля пустые и в JSON не попадают.
type ToolStatus struct {
	Code      string `json:"code,omitempty" jsonschema:"стабильный код ошибки: invalid_argument, quota_exceeded, upstream_unavailable, internal"`
	Message   string `json:"message,omitempty" jsonschema:"текст ошибки от API или валидатора"`
	Retryable *bool  `json:"retryable,omitempty" jsonschema:"true, если запрос имеет смысл повторить"`
}

// Стабильные коды ошибок инструментов.
const (
	CodeInvalidArgument     = "invalid_argument"
	CodeQuotaExceeded       = "quota_exceeded"
	CodeUpstreamUnavailable = "upstream_unavailable"
	CodeInternal            = "internal"
)

// TopRequestsOutput — результат инструмента top_requests.
type TopRequestsOutput struct {
	ToolStatus

	Phrase       string        `json:"phrase" jsonschema:"поисковая фраза"`
	HasData      bool          `json:"hasData" jsonschema:"false — Wordstat не знает такой фразы, спроса нет; это не ошибка, а валидный ответ"`
	TotalCount   int64         `json:"totalCount" jsonschema:"общее число запросов, содержащих все слова фразы, за 30 дней"`
	Requests     []PhraseCount `json:"requests" jsonschema:"популярные запросы по убыванию частотности; это подмножества totalCount, суммировать их нельзя"`
	Associations []PhraseCount `json:"associations" jsonschema:"похожие запросы; не подмножества totalCount"`
	Regions      []string      `json:"regions" jsonschema:"фактический фильтр по geo ID (пусто — вся Россия)"`
	Devices      []string      `json:"devices" jsonschema:"фактический фильтр по устройствам (пусто — все устройства)"`
}

// PhraseCount — фраза и её частотность.
type PhraseCount struct {
	Phrase string `json:"phrase" jsonschema:"формулировка запроса"`
	Count  int64  `json:"count" jsonschema:"число запросов за последние 30 дней"`
}

// DynamicsOutput — результат инструмента dynamics.
type DynamicsOutput struct {
	ToolStatus

	Phrase   string          `json:"phrase" jsonschema:"поисковая фраза"`
	Period   string          `json:"period" jsonschema:"фактическая детализация: PERIOD_DAILY, PERIOD_WEEKLY или PERIOD_MONTHLY"`
	FromDate string          `json:"fromDate" jsonschema:"фактическое начало периода, RFC3339"`
	ToDate   string          `json:"toDate" jsonschema:"фактический конец периода, RFC3339"`
	Points   []DynamicsPoint `json:"points" jsonschema:"точки временного ряда"`
	Regions  []string        `json:"regions" jsonschema:"фактический фильтр по geo ID (пусто — вся Россия)"`
	Devices  []string        `json:"devices" jsonschema:"фактический фильтр по устройствам (пусто — все устройства)"`
}

// DynamicsPoint — одна точка временного ряда.
type DynamicsPoint struct {
	Date  string  `json:"date" jsonschema:"дата точки, RFC3339"`
	Count int64   `json:"count" jsonschema:"число запросов за период"`
	Share float64 `json:"share" jsonschema:"доля запроса от всех запросов к Яндексу"`
}

// RegionsOutput — результат инструмента regions.
type RegionsOutput struct {
	ToolStatus

	Phrase  string        `json:"phrase" jsonschema:"поисковая фраза"`
	Region  string        `json:"region" jsonschema:"фактическая группировка: REGION_ALL, REGION_CITIES или REGION_REGIONS"`
	Regions []RegionCount `json:"regions" jsonschema:"распределение по регионам за 30 дней"`
}

// RegionCount — статистика по одному региону.
type RegionCount struct {
	RegionID      string  `json:"regionId" jsonschema:"ID региона (название — в list_regions)"`
	Count         int64   `json:"count" jsonschema:"число запросов в регионе за 30 дней"`
	Share         float64 `json:"share" jsonschema:"доля запроса от всех запросов к Яндексу в регионе"`
	AffinityIndex float64 `json:"affinityIndex" jsonschema:"индекс интереса: доля в регионе к доле по стране"`
}

// RegionsTreeOutput — результат инструмента list_regions.
//
// Регионы отдаются плоским списком с уровнем вложенности: инференс JSON-схемы
// в SDK не умеет рекурсивные типы (AddTool паникует с «cycle detected»), а
// плоский список с depth/parentId удобнее для машинной обработки. Текстовый
// вывод при этом остаётся деревом с отступами (см. formatRegionTree).
type RegionsTreeOutput struct {
	ToolStatus

	Count   int           `json:"count" jsonschema:"сколько всего регионов в справочнике"`
	Regions []RegionEntry `json:"regions" jsonschema:"регионы в порядке обхода дерева"`
}

// RegionEntry — один регион в плоском списке.
type RegionEntry struct {
	ID       string `json:"id" jsonschema:"ID региона"`
	Name     string `json:"name" jsonschema:"название региона"`
	Depth    int    `json:"depth" jsonschema:"уровень вложенности: 0 — корень"`
	ParentID string `json:"parentId,omitempty" jsonschema:"ID родительского региона"`
}

// describeError превращает ошибку клиента в структурированный конверт.
func describeError(err error) ToolStatus {
	status := ToolStatus{Message: err.Error(), Retryable: boolPtr(false)}

	switch {
	case errors.Is(err, wordstat.ErrInvalidArgument):
		status.Code = CodeInvalidArgument
	case errors.Is(err, wordstat.ErrQuotaExceeded):
		status.Code, status.Retryable = CodeQuotaExceeded, boolPtr(true)
	case errors.Is(err, wordstat.ErrUnavailable):
		status.Code, status.Retryable = CodeUpstreamUnavailable, boolPtr(true)
	default:
		status.Code = CodeInternal
	}

	return status
}

// boolPtr возвращает указатель на bool: с omitempty обычный false исчез бы из
// JSON, а потребителю важно видеть retryable = false.
func boolPtr(v bool) *bool { return &v }

// errorCall собирает результат сбоя: isError + текст ошибки.
func errorCall(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

// failedTopRequests — выход top_requests при сбое.
func failedTopRequests(err error) TopRequestsOutput {
	return TopRequestsOutput{
		ToolStatus:   describeError(err),
		Requests:     []PhraseCount{},
		Associations: []PhraseCount{},
		Regions:      []string{},
		Devices:      []string{},
	}
}

// failedDynamics — выход dynamics при сбое.
func failedDynamics(err error) DynamicsOutput {
	return DynamicsOutput{
		ToolStatus: describeError(err),
		Points:     []DynamicsPoint{},
		Regions:    []string{},
		Devices:    []string{},
	}
}

// failedRegions — выход regions при сбое.
func failedRegions(err error) RegionsOutput {
	return RegionsOutput{
		ToolStatus: describeError(err),
		Regions:    []RegionCount{},
	}
}

// failedRegionsTree — выход list_regions при сбое.
func failedRegionsTree(err error) RegionsTreeOutput {
	return RegionsTreeOutput{
		ToolStatus: describeError(err),
		Regions:    []RegionEntry{},
	}
}

// topRequestsOutput конвертирует ответ API в структурированный результат.
func topRequestsOutput(phrase string, res *wordstat.TopRequestsResponse) (TopRequestsOutput, error) {
	total, err := parseCount("totalCount", res.TotalCount)
	if err != nil {
		return TopRequestsOutput{}, err
	}

	requests, err := phraseCounts("requests", res.Results)
	if err != nil {
		return TopRequestsOutput{}, err
	}
	associations, err := phraseCounts("associations", res.Associations)
	if err != nil {
		return TopRequestsOutput{}, err
	}

	return TopRequestsOutput{
		Phrase:       phrase,
		HasData:      total > 0,
		TotalCount:   total,
		Requests:     requests,
		Associations: associations,
		Regions:      nonNilStrings(res.Regions),
		Devices:      nonNilStrings(res.Devices),
	}, nil
}

// phraseCounts конвертирует список «фраза → count» в структурированный вид.
func phraseCounts(field string, stats []wordstat.PhraseStat) ([]PhraseCount, error) {
	out := make([]PhraseCount, 0, len(stats))
	for _, s := range stats {
		count, err := parseCount(field+".count", s.Count)
		if err != nil {
			return nil, err
		}
		out = append(out, PhraseCount{Phrase: s.Phrase, Count: count})
	}
	return out, nil
}

// dynamicsOutput конвертирует ответ API в структурированный результат.
func dynamicsOutput(phrase string, res *wordstat.DynamicsResponse) (DynamicsOutput, error) {
	points := make([]DynamicsPoint, 0, len(res.Results))
	for _, p := range res.Results {
		count, err := parseCount("results.count", p.Count)
		if err != nil {
			return DynamicsOutput{}, err
		}
		points = append(points, DynamicsPoint{Date: p.Date, Count: count, Share: p.Share})
	}

	return DynamicsOutput{
		Phrase:   phrase,
		Period:   res.Period,
		FromDate: res.FromDate,
		ToDate:   res.ToDate,
		Points:   points,
		Regions:  nonNilStrings(res.Regions),
		Devices:  nonNilStrings(res.Devices),
	}, nil
}

// regionsOutput конвертирует ответ API в структурированный результат.
func regionsOutput(phrase string, res *wordstat.RegionsResponse) (RegionsOutput, error) {
	regions := make([]RegionCount, 0, len(res.Results))
	for _, r := range res.Results {
		count, err := parseCount("results.count", r.Count)
		if err != nil {
			return RegionsOutput{}, err
		}
		regions = append(regions, RegionCount{
			RegionID:      r.RegionID,
			Count:         count,
			Share:         r.Share,
			AffinityIndex: r.AffinityIndex,
		})
	}

	return RegionsOutput{Phrase: phrase, Region: res.Region, Regions: regions}, nil
}

// regionsTreeOutput разворачивает дерево регионов в плоский список.
func regionsTreeOutput(res *wordstat.RegionsTreeResponse) RegionsTreeOutput {
	regions := []RegionEntry{}

	var walk func(nodes []wordstat.RegionNode, depth int, parentID string)
	walk = func(nodes []wordstat.RegionNode, depth int, parentID string) {
		for _, n := range nodes {
			regions = append(regions, RegionEntry{ID: n.ID, Name: n.Name, Depth: depth, ParentID: parentID})
			walk(n.Children, depth+1, n.ID)
		}
	}
	walk(res.Regions, 0, "")

	return RegionsTreeOutput{Count: len(regions), Regions: regions}
}

// nonNilStrings возвращает пустой слайс вместо nil, чтобы в JSON был [], а не null.
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// parseCount превращает count из API в число. protobuf int64 сериализуется
// строкой, а нулевые значения proto3 JSON по умолчанию опускает — пустая
// строка означает 0.
func parseCount(field, raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("unexpected %s from Wordstat API: %q", field, raw)
	}
	return n, nil
}
