package wordstat

// TopRequestsResponse — ответ метода /v2/wordstat/topRequests
type TopRequestsResponse struct {
	TotalCount   string       `json:"totalCount"`   // приходит строкой
	TopRequests  []PhraseStat `json:"topRequests"`
	Associations []PhraseStat `json:"associations"`
}

type PhraseStat struct {
	Phrase string `json:"phrase"`
	Count  string `json:"count"` // приходит строкой
}

// DynamicsResponse — ответ метода /v2/wordstat/dynamics
type DynamicsResponse struct {
	Series []DynamicsPoint `json:"series"`
}

type DynamicsPoint struct {
	Date  string `json:"date"`
	Count string `json:"count"`
	Share string `json:"share"`
}

// RegionsResponse — ответ метода /v2/wordstat/regions
type RegionsResponse struct {
	Regions []RegionStat `json:"regions"`
}

type RegionStat struct {
	RegionID      string `json:"regionId"`
	Count         string `json:"count"`
	Share         string `json:"share"`
	AffinityIndex string `json:"affinityIndex"`
}

// RegionsTreeResponse — ответ метода /v2/wordstat/getRegionsTree
type RegionsTreeResponse struct {
	Regions []RegionNode `json:"regions"`
}

type RegionNode struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Children []RegionNode `json:"children,omitempty"`
}