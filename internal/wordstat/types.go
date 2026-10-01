// Package wordstat — тонкий клиент Yandex Cloud Search API v2 (сервис Wordstat).
//
// Поля структур повторяют yandex/cloud/searchapi/v2/wordstat_service.proto:
// https://github.com/yandex-cloud/cloudapi/blob/master/yandex/cloud/searchapi/v2/wordstat_service.proto
//
// Внимание к типам: protobuf int64 сериализуется в JSON строкой (count, totalCount),
// а double (share, affinityIndex) — обычным числом.
package wordstat

// TopRequestsResponse — ответ метода POST /v2/wordstat/topRequests (GetTop).
type TopRequestsResponse struct {
	// TotalCount — общее число запросов, содержащих все ключевые слова (int64 → строка).
	TotalCount string `json:"totalCount"`
	// Results — популярные запросы.
	Results []PhraseStat `json:"results"`
	// Associations — похожие запросы; на узких нишах приходит пустым.
	Associations []PhraseStat `json:"associations"`
}

// PhraseStat — пара «фраза → число запросов».
type PhraseStat struct {
	Phrase string `json:"phrase"`
	Count  string `json:"count"` // int64 → строка
}

// DynamicsResponse — ответ метода POST /v2/wordstat/dynamics (GetDynamics).
type DynamicsResponse struct {
	// Results — точки временного ряда.
	Results []DynamicsPoint `json:"results"`
}

// DynamicsPoint — одна точка динамики.
type DynamicsPoint struct {
	Date  string  `json:"date"`  // google.protobuf.Timestamp → RFC3339
	Count string  `json:"count"` // int64 → строка
	Share float64 `json:"share"` // double → число
}

// RegionsResponse — ответ метода POST /v2/wordstat/regions (GetRegionsDistribution).
type RegionsResponse struct {
	// Results — распределение по регионам.
	Results []RegionStat `json:"results"`
}

// RegionStat — статистика по одному региону.
type RegionStat struct {
	RegionID      string  `json:"region"` // ID региона приходит в поле region
	Count         string  `json:"count"`  // int64 → строка
	Share         float64 `json:"share"`
	AffinityIndex float64 `json:"affinityIndex"`
}

// RegionsTreeResponse — ответ метода POST /v2/wordstat/getRegionsTree (GetRegionsTree).
type RegionsTreeResponse struct {
	Regions []RegionNode `json:"regions"`
}

// RegionNode — узел дерева регионов.
type RegionNode struct {
	ID       string       `json:"id"`
	Name     string       `json:"label"` // название региона приходит в поле label
	Children []RegionNode `json:"children,omitempty"`
}
