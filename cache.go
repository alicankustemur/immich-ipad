package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

type PhotoCache struct {
	mu sync.Mutex
	// maxPages holds the effective page count per configured device model.
	maxPages map[string]int
	queue    []PhotoInfo
	shown    map[string]bool
	client   *http.Client
	cfg      Config
}

// totalPages returns the combined page count across all models. Caller must hold c.mu.
func (c *PhotoCache) totalPages() int {
	total := 0
	for _, n := range c.maxPages {
		total += n
	}
	return total
}

// probe reports whether a page still returns assets for a model. An API failure
// is returned as an error rather than "no assets" — treating a failed request as
// an empty page would make the search below converge on a bogus page count.
func (c *PhotoCache) probe(model string, page int) (bool, error) {
	_, raw, err := c.fetchPage(model, page, 1)
	if err != nil {
		return false, err
	}
	return raw > 0, nil
}

// maxPageFor finds the last page that still returns assets for a model. prev is
// the previously known count (0 if unknown): when set, the search gallops out
// from there, which costs a handful of requests instead of the ~17 a full binary
// search over the whole library needs. Returns 0 only if page 1 is genuinely empty.
func (c *PhotoCache) maxPageFor(model string, prev, upper int) (int, error) {
	// Bracket the boundary as (low, high]: low has assets, high does not.
	low, high := 0, upper+1

	if prev > 0 && prev <= upper {
		ok, err := c.probe(model, prev)
		if err != nil {
			return 0, err
		}
		if ok {
			low = prev
			for step := 1; ; step *= 2 {
				next := prev + step
				if next > upper {
					break
				}
				ok, err := c.probe(model, next)
				if err != nil {
					return 0, err
				}
				if !ok {
					high = next
					break
				}
				low = next
			}
		} else {
			high = prev
			for step := 1; ; step *= 2 {
				next := prev - step
				if next < 1 {
					break
				}
				ok, err := c.probe(model, next)
				if err != nil {
					return 0, err
				}
				if ok {
					low = next
					break
				}
				high = next
			}
		}
	}

	for low+1 < high {
		mid := low + (high-low)/2
		ok, err := c.probe(model, mid)
		if err != nil {
			return 0, err
		}
		if ok {
			low = mid
		} else {
			high = mid
		}
	}
	return low, nil
}

// refreshTotal rediscovers the page count per device model. It reports whether
// any model has a usable count, so the caller knows to keep retrying.
func (c *PhotoCache) refreshTotal() bool {
	// First get upper bound from statistics API
	req, err := http.NewRequest("GET", c.cfg.ImmichURL+"/api/assets/statistics", nil)
	if err != nil {
		log.Printf("Statistics request error: %v", err)
		return false
	}
	req.Header.Set("x-api-key", c.cfg.ImmichAPIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		log.Printf("Statistics API error: %v", err)
		return false
	}
	defer resp.Body.Close()

	var stats struct {
		Images int `json:"images"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		log.Printf("Statistics decode error: %v", err)
		return false
	}

	if stats.Images == 0 {
		return false
	}

	for _, model := range c.cfg.DeviceModels {
		c.mu.Lock()
		prev := c.maxPages[model]
		c.mu.Unlock()

		n, err := c.maxPageFor(model, prev, stats.Images)
		if err != nil {
			// Keep whatever we knew before: a transient Immich outage must not
			// drop a model out of the rotation.
			log.Printf("Page count probe for %q failed, keeping %d: %v", model, prev, err)
			continue
		}

		c.mu.Lock()
		if n != prev {
			log.Printf("Updating maxPage for %q: %d -> %d (total images: %d)", model, prev, n, stats.Images)
			c.maxPages[model] = n
		}
		c.mu.Unlock()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalPages() > 0
}

// startRefreshLoop refreshes the page counts every hour, retrying quickly until
// the first success. Immich is often not reachable yet when this container
// starts; without the fast retry the frame would stay blank for a full hour.
func (c *PhotoCache) startRefreshLoop() {
	go func() {
		for !c.refreshTotal() {
			time.Sleep(1 * time.Minute)
		}
		for range time.NewTicker(1 * time.Hour).C {
			c.refreshTotal()
		}
	}()
}

// pickModel chooses a device model at random, weighted by how many photos each
// one has, so every photo across all models is equally likely to be picked.
// Returns the model and its page count, or ("", 0) if nothing is available yet.
// Caller must hold c.mu.
func (c *PhotoCache) pickModel() (string, int) {
	total := c.totalPages()
	if total == 0 {
		return "", 0
	}
	r := rand.Intn(total)
	for _, model := range c.cfg.DeviceModels {
		n := c.maxPages[model]
		if r < n {
			return model, n
		}
		r -= n
	}
	return "", 0
}

// fillQueue fetches 1 photo from a random page of a random device model
func (c *PhotoCache) fillQueue() {
	if c.totalPages() == 0 {
		log.Printf("Page counts not yet initialized, waiting for statistics refresh")
		return
	}
	for retries := 0; retries < 10; retries++ {
		model, maxPage := c.pickModel()
		if maxPage == 0 {
			return
		}
		page := rand.Intn(maxPage) + 1
		photos, _, err := c.fetchPage(model, page, 1)
		if err != nil {
			log.Printf("Fetch page %d of %q failed: %v", page, model, err)
			continue
		}
		if len(photos) == 0 {
			continue
		}
		p := photos[0]
		if !c.shown[p.ID] {
			c.queue = append(c.queue, p)
			log.Printf("Fetched page %d of %q (shown: %d, maxPage: %d)", page, model, len(c.shown), maxPage)
			return
		}
	}
}

// fetchPage returns the photos on a page for one device model, along with the
// number of assets the API returned before screenshots were filtered out. That
// raw count is what tells a page past the end of the results (0 assets) apart
// from a page that only held screenshots. A non-nil error means the count is
// unknown, which callers must not confuse with a count of zero.
func (c *PhotoCache) fetchPage(model string, page, pageSize int) ([]PhotoInfo, int, error) {
	searchBody := map[string]interface{}{
		"type":       "IMAGE",
		"page":       page,
		"size":       pageSize,
		"model":      model,
		"visibility": "timeline",
	}

	bodyBytes, err := json.Marshal(searchBody)
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequest("POST", c.cfg.ImmichURL+"/api/search/metadata", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("x-api-key", c.cfg.ImmichAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("search API status %d: %s", resp.StatusCode, string(body))
	}

	var result searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, 0, err
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

	return photos, len(result.Assets.Items), nil
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
	if len(c.shown) >= c.totalPages() {
		log.Printf("All %d photos shown, resetting cycle", len(c.shown))
		c.shown = make(map[string]bool)
	}

	return &p
}
