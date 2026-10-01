package mymcp

import (
	"fmt"
	"strings"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
)

// formatTopRequests рендерит топ запросов и ассоциации.
func formatTopRequests(phrase string, res *wordstat.TopRequestsResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Фраза: %s\nВсего показов: %s\n\nПопулярные запросы:\n", phrase, res.TotalCount)
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
	if len(res.Results) == 0 {
		b.WriteString("(пусто)\n")
	}
	for _, p := range res.Results {
		fmt.Fprintf(&b, "%s: count=%s share=%g\n", p.Date, p.Count, p.Share)
	}
	return b.String()
}

// formatRegions рендерит распределение по регионам.
func formatRegions(phrase string, res *wordstat.RegionsResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Регионы для «%s»:\n", phrase)
	if len(res.Results) == 0 {
		b.WriteString("(пусто)\n")
	}
	for _, r := range res.Results {
		fmt.Fprintf(&b, "regionId=%s count=%s share=%g affinity=%g\n", r.RegionID, r.Count, r.Share, r.AffinityIndex)
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
