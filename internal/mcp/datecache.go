package mcp

import (
	"sync"
	"time"
)

// DateCache provides cached listing of available dates.
// This is useful because listing dates requires scanning storage.
type DateCache struct {
	mu      sync.RWMutex
	sites   map[string][]string // siteID -> list of dates
	updated map[string]time.Time
	ttl     time.Duration
}

// NewDateCache creates a new date cache with default 5-minute TTL.
func NewDateCache() *DateCache {
	return &DateCache{
		sites:   make(map[string][]string),
		updated: make(map[string]time.Time),
		ttl:     5 * time.Minute,
	}
}

// Get returns cached dates for a site if not expired.
func (c *DateCache) Get(siteID string) ([]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	dates, ok := c.sites[siteID]
	if !ok {
		return nil, false
	}

	updated, ok := c.updated[siteID]
	if !ok || time.Since(updated) > c.ttl {
		return nil, false
	}

	return dates, true
}

// Set stores dates for a site.
func (c *DateCache) Set(siteID string, dates []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.sites[siteID] = dates
	c.updated[siteID] = time.Now()
}

// Invalidate clears the cache for a site.
func (c *DateCache) Invalidate(siteID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.sites, siteID)
	delete(c.updated, siteID)
}
