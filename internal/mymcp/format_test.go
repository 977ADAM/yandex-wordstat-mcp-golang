package mymcp

import (
	"testing"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
)

func TestFormatTopRequests(t *testing.T) {
	tests := []struct {
		name string
		res  *wordstat.TopRequestsResponse
		want string
	}{
		{
			name: "с результатами и ассоциациями",
			res: &wordstat.TopRequestsResponse{
				TotalCount: "21500",
				Results: []wordstat.PhraseStat{
					{Phrase: "чат боты для бизнеса", Count: "1100"},
					{Phrase: "чат бот для бизнеса макс", Count: "145"},
				},
				Associations: []wordstat.PhraseStat{
					{Phrase: "чатбот", Count: "7371"},
				},
			},
			want: "Фраза: чат бот для бизнеса\n" +
				"Всего показов: 21500\n" +
				"\n" +
				"Популярные запросы:\n" +
				"- чат боты для бизнеса: 1100\n" +
				"- чат бот для бизнеса макс: 145\n" +
				"\n" +
				"Похожие запросы:\n" +
				"- чатбот: 7371\n",
		},
		{
			name: "пустой ответ без ассоциаций",
			res:  &wordstat.TopRequestsResponse{TotalCount: "0"},
			want: "Фраза: узкая ниша\n" +
				"Всего показов: 0\n" +
				"\n" +
				"Популярные запросы:\n" +
				"(пусто)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			phrase := "чат бот для бизнеса"
			if len(tt.res.Results) == 0 {
				phrase = "узкая ниша"
			}
			if got := formatTopRequests(phrase, tt.res); got != tt.want {
				t.Errorf("formatTopRequests() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestFormatDynamics(t *testing.T) {
	res := &wordstat.DynamicsResponse{
		Results: []wordstat.DynamicsPoint{
			{Date: "2026-01-31T00:00:00Z", Count: "1050", Share: 8.7e-06},
			{Date: "2026-02-28T00:00:00Z", Count: "1180", Share: 0.5},
		},
	}

	want := "Динамика для «яндекс»:\n" +
		"2026-01-31T00:00:00Z: count=1050 share=8.7e-06\n" +
		"2026-02-28T00:00:00Z: count=1180 share=0.5\n"

	if got := formatDynamics("яндекс", res); got != want {
		t.Errorf("formatDynamics() =\n%q\nwant\n%q", got, want)
	}

	if got, want := formatDynamics("яндекс", &wordstat.DynamicsResponse{}), "Динамика для «яндекс»:\n(пусто)\n"; got != want {
		t.Errorf("formatDynamics(empty) = %q, want %q", got, want)
	}
}

func TestFormatRegions(t *testing.T) {
	res := &wordstat.RegionsResponse{
		Results: []wordstat.RegionStat{
			{RegionID: "213", Count: "235", Share: 0.0000109, AffinityIndex: 120.4},
			{RegionID: "2", Count: "2330855", Share: 0.5818950367758946, AffinityIndex: 123.75},
		},
	}

	want := "Регионы для «яндекс»:\n" +
		"regionId=213 count=235 share=1.09e-05 affinity=120.4\n" +
		"regionId=2 count=2330855 share=0.5818950367758946 affinity=123.75\n"

	if got := formatRegions("яндекс", res); got != want {
		t.Errorf("formatRegions() =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatRegionTree(t *testing.T) {
	res := &wordstat.RegionsTreeResponse{
		Regions: []wordstat.RegionNode{
			{
				ID:   "225",
				Name: "Россия",
				Children: []wordstat.RegionNode{
					{ID: "213", Name: "Москва"},
					{ID: "2", Name: "Санкт-Петербург"},
				},
			},
			{ID: "187", Name: "Украина"},
		},
	}

	want := "Регионы:\n" +
		"Россия (id=225)\n" +
		"  Москва (id=213)\n" +
		"  Санкт-Петербург (id=2)\n" +
		"Украина (id=187)\n"

	if got := formatRegionTree(res); got != want {
		t.Errorf("formatRegionTree() =\n%q\nwant\n%q", got, want)
	}
}
