package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

type PhotoCache struct {
	mu      sync.Mutex
	queue   []PhotoInfo
	shown   map[string]bool
	maxPage int
	client  *http.Client
	cfg     Config
}

// refreshTotal discovers the effective max page by binary searching the filtered search API
func (c *PhotoCache) refreshTotal() {
	// First get upper bound from statistics API
	req, err := http.NewRequest("GET", c.cfg.ImmichURL+"/api/assets/statistics", nil)
	if err != nil {
		log.Printf("Statistics request error: %v", err)
		return
	}
	req.Header.Set("x-api-key", c.cfg.ImmichAPIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		log.Printf("Statistics API error: %v", err)
		return
	}
	defer resp.Body.Close()

	var stats struct {
		Images int `json:"images"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		log.Printf("Statistics decode error: %v", err)
		return
	}

	if stats.Images == 0 {
		return
	}

	// Binary search to find the last page that returns results with our filters
	low, high := 1, stats.Images
	for low < high {
		mid := (low + high + 1) / 2
		photos, _ := c.fetchPage(mid, 1)
		if len(photos) > 0 {
			low = mid
		} else {
			high = mid - 1
		}
	}

	c.mu.Lock()
	if low != c.maxPage {
		log.Printf("Updating maxPage: %d -> %d (total images: %d)", c.maxPage, low, stats.Images)
		c.maxPage = low
	}
	c.mu.Unlock()
}

// startRefreshLoop refreshes the total photo count every hour
func (c *PhotoCache) startRefreshLoop() {
	c.refreshTotal()
	go func() {
		for range time.NewTicker(1 * time.Hour).C {
			c.refreshTotal()
		}
	}()
}

// fillQueue fetches 1 photo from a random page
func (c *PhotoCache) fillQueue() {
	if c.maxPage == 0 {
		log.Printf("maxPage not yet initialized, waiting for statistics refresh")
		return
	}
	for retries := 0; retries < 10; retries++ {
		page := rand.Intn(c.maxPage) + 1
		photos, _ := c.fetchPage(page, 1)
		if len(photos) == 0 {
			continue
		}
		p := photos[0]
		if !c.shown[p.ID] {
			c.queue = append(c.queue, p)
			log.Printf("Fetched page %d (shown: %d, maxPage: %d)", page, len(c.shown), c.maxPage)
			return
		}
	}
}

func (c *PhotoCache) fetchPage(page, pageSize int) ([]PhotoInfo, bool) {
	searchBody := map[string]interface{}{
		"type":       "IMAGE",
		"page":       page,
		"size":       pageSize,
		"model":      c.cfg.DeviceModel,
		"visibility": "timeline",
	}

	bodyBytes, err := json.Marshal(searchBody)
	if err != nil {
		return nil, false
	}

	req, err := http.NewRequest("POST", c.cfg.ImmichURL+"/api/search/metadata", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, false
	}
	req.Header.Set("x-api-key", c.cfg.ImmichAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		log.Printf("Search API error: %v", err)
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Search API status %d: %s", resp.StatusCode, string(body))
		return nil, false
	}

	var result searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Search API decode error: %v", err)
		return nil, false
	}

	if len(result.Assets.Items) == 0 {
		return []PhotoInfo{}, false
	}

	var photos []PhotoInfo
	for _, a := range result.Assets.Items {
		if strings.Contains(strings.ToLower(a.OriginalFileName), "screenshot") {
			continue
		}
		photos = append(photos, PhotoInfo{
			ID:   a.ID,
			Date: formatDate(a.FileCreatedAt),
		})
	}

	hasMore := result.Assets.NextPage != "" && len(result.Assets.Items) >= pageSize
	return photos, hasMore
}

func (c *PhotoCache) next() *PhotoInfo {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.queue) == 0 {
		c.fillQueue()
	}
	if len(c.queue) == 0 {
		return nil
	}

	p := c.queue[0]
	c.queue = c.queue[1:]
	c.shown[p.ID] = true

	// Reset shown set when all photos have been shown
	if len(c.shown) >= c.maxPage {
		log.Printf("All %d photos shown, resetting cycle", len(c.shown))
		c.shown = make(map[string]bool)
	}

	return &p
}
