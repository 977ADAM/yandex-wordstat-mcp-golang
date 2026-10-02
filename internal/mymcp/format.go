package mymcp

import (
	"fmt"
	"strings"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
)

// formatTopRequests рендерит топ запросов и ассоциации.
func formatTopRequests(phrase string, res *wordstat.TopRequestsResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Фраза: %s\n", phrase)
	b.WriteString(formatScope(res.Regions, res.Devices))
	fmt.Fprintf(&b, "Всего показов: %s\n\nПопулярные запросы:\n", res.TotalCount)
	if len(res.Results) == 0 {
		b.WriteString("(пусто)\n")
	}
	for _, p := range res.Results {
		fmt.Fprintf(&b, "- %s: %s\n", p.Phrase, p.Count)
	}
	if len(res.Associations) > 0 {
		b.WriteString("\nПохожие запросы:\n")
		for _, p := range res.Associations {
			fmt.Fprintf(&b, "- %s: %s\n", p.Phrase, p.Count)
		}
	}
	return b.String()
}

// formatDynamics рендерит временной ряд.
func formatDynamics(phrase string, res *wordstat.DynamicsResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Динамика для «%s»:\n", phrase)
	b.WriteString(formatScope(res.Regions, res.Devices))
	if len(res.Results) == 0 {
		b.WriteString("(пусто)\n")
	}
	for _, p := range res.Results {
		fmt.Fprintf(&b, "%s: count=%s share=%g\n", p.Date, p.Count, p.Share)
	}
	return b.String()
}

// formatScope рендерит фактический фильтр запроса: по нему видно, что именно
// ушло в API, включая подставленные по умолчанию значения.
func formatScope(regions, devices []string) string {
	if len(regions) == 0 && len(devices) == 0 {
		return ""
	}

	parts := make([]string, 0, 2)
	if len(regions) > 0 {
		parts = append(parts, "регионы: "+strings.Join(regions, ", "))
	}
	if len(devices) > 0 {
		parts = append(parts, "устройства: "+strings.Join(devices, ", "))
	}
	return "Фильтр — " + strings.Join(parts, "; ") + "\n"
}

// formatRegions рендерит распределение по регионам. names — справочник
// id → название (может быть nil).
func formatRegions(phrase string, res *wordstat.RegionsResponse, names map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Регионы для «%s»:\n", phrase)
	if len(res.Results) == 0 {
		b.WriteString("(пусто)\n")
	}
	for _, r := range res.Results {
		name := ""
		if n := names[r.RegionID]; n != "" {
			name = " (" + n + ")"
		}
		fmt.Fprintf(&b, "regionId=%s%s count=%s share=%g affinity=%g\n",
			r.RegionID, name, r.Count, r.Share, r.AffinityIndex)
	}
	return b.String()
}

// formatRegionTree рендерит дерево регионов с отступами по уровню.
func formatRegionTree(res *wordstat.RegionsTreeResponse) string {
	var b strings.Builder
	b.WriteString("Регионы:\n")

	var walk func(nodes []wordstat.RegionNode, depth int)
	walk = func(nodes []wordstat.RegionNode, depth int) {
		for _, n := range nodes {
			fmt.Fprintf(&b, "%*s%s (id=%s)\n", depth*2, "", n.Name, n.ID)
			walk(n.Children, depth+1)
		}
	}
	walk(res.Regions, 0)

	return b.String()
}
