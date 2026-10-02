package wordstat

import (
	"encoding/json"
	"sync"
	"time"
)

// DefaultCacheTTL — время жизни записи кэша в процессе. Семантика Wordstat
// меняется раз в месяц, но данные обновляются чаще, поэтому сутки — компромисс
// между свежестью и экономией квоты.
const DefaultCacheTTL = 24 * time.Hour

// maxCacheEntries ограничивает размер кэша, чтобы долгоживущий процесс не рос
// бесконечно.
const maxCacheEntries = 256

type cacheEntry struct {
	value   any
	expires time.Time
}

// cache — простой кэш ответов с TTL и вытеснением по времени истечения.
type cache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]cacheEntry
}

func newCache(ttl time.Duration) *cache {
	return &cache{ttl: ttl, entries: make(map[string]cacheEntry)}
}

// get возвращает значение, если запись есть и не истекла. ttl <= 0 отключает кэш.
func (c *cache) get(key string, now time.Time) (any, bool) {
	if c == nil || c.ttl <= 0 || key == "" {
		return nil, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if !now.Before(entry.expires) {
		delete(c.entries, key)
		return nil, false
	}
	return entry.value, true
}

// put сохраняет значение, освобождая место при переполнении.
func (c *cache) put(key string, value any, now time.Time) {
	if c == nil || c.ttl <= 0 || key == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= maxCacheEntries {
		c.evict(now)
	}
	c.entries[key] = cacheEntry{value: value, expires: now.Add(c.ttl)}
}

// evict удаляет истёкшие записи, а если места всё ещё нет — самую раннюю по
// истечению.
func (c *cache) evict(now time.Time) {
	var oldestKey string
	var oldestExpiry time.Time

	for key, entry := range c.entries {
		if !now.Before(entry.expires) {
			delete(c.entries, key)
			continue
		}
		if oldestKey == "" || entry.expires.Before(oldestExpiry) {
			oldestKey, oldestExpiry = key, entry.expires
		}
	}

	if len(c.entries) >= maxCacheEntries && oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

// cacheKey строит ключ из имени метода и параметров запроса: encoding/json
// сортирует ключи map, поэтому ключ детерминирован.
func cacheKey(method string, payload map[string]any) string {
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return method + ":" + string(data)
}

// cachedResponse — ответ, который умеет отдать свою копию с отметкой о кэше.
// Копия глубокая по слайсам: вызывающий код не должен портить закэшированный
// объект и наоборот.
type cachedResponse interface {
	cloneWithCacheHit(hit bool) cachedResponse
}

func (r *TopRequestsResponse) cloneWithCacheHit(hit bool) cachedResponse {
	clone := *r
	clone.CacheHit = hit
	clone.Results = clonePhraseStats(r.Results)
	clone.Associations = clonePhraseStats(r.Associations)
	clone.Regions = cloneStrings(r.Regions)
	clone.Devices = cloneStrings(r.Devices)
	return &clone
}

func (r *DynamicsResponse) cloneWithCacheHit(hit bool) cachedResponse {
	clone := *r
	clone.CacheHit = hit
	clone.Results = append([]DynamicsPoint(nil), r.Results...)
	clone.Regions = cloneStrings(r.Regions)
	clone.Devices = cloneStrings(r.Devices)
	return &clone
}

func (r *RegionsResponse) cloneWithCacheHit(hit bool) cachedResponse {
	clone := *r
	clone.CacheHit = hit
	clone.Results = append([]RegionStat(nil), r.Results...)
	return &clone
}

func (r *RegionsTreeResponse) cloneWithCacheHit(hit bool) cachedResponse {
	clone := *r
	clone.CacheHit = hit
	clone.Regions = cloneRegionNodes(r.Regions)
	return &clone
}

// cloneStrings копирует слайс строк, сохраняя nil.
func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

// clonePhraseStats копирует слайс «фраза → count», сохраняя nil.
func clonePhraseStats(stats []PhraseStat) []PhraseStat {
	if stats == nil {
		return nil
	}
	return append([]PhraseStat(nil), stats...)
}

// cloneRegionNodes рекурсивно копирует дерево регионов.
func cloneRegionNodes(nodes []RegionNode) []RegionNode {
	if nodes == nil {
		return nil
	}
	out := make([]RegionNode, len(nodes))
	for i, node := range nodes {
		out[i] = node
		out[i].Children = cloneRegionNodes(node.Children)
	}
	return out
}

// cached отдаёт копию закэшированного ответа с CacheHit = true.
func (c *Client) cached(key string) (cachedResponse, bool) {
	value, ok := c.cache.get(key, c.now())
	if !ok {
		return nil, false
	}
	response, ok := value.(cachedResponse)
	if !ok {
		return nil, false
	}
	return response.cloneWithCacheHit(true), true
}

// store кладёт в кэш копию ответа с CacheHit = false.
func (c *Client) store(key string, value cachedResponse) {
	if key == "" {
		return
	}
	c.cache.put(key, value.cloneWithCacheHit(false), c.now())
}
