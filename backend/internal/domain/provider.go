package domain

import "time"

// ProviderStats is what the status screen shows about an external provider
// and the cache in front of it.
type ProviderStats struct {
	Requests24h   int
	Errors24h     int
	LastSuccessAt *time.Time
	LastErrorAt   *time.Time
	CacheEntries  int64
	CacheHits     int64
}
