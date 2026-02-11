package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"
)

//go:embed templates/index.html
var templateFS embed.FS

type Config struct {
	ImmichURL         string
	ImmichAPIKey      string
	DeviceModel       string
	SlideshowInterval int
	Port              string
}

type PhotoInfo struct {
	ID       string `json:"id"`
	Date     string `json:"date"`
	City     string `json:"city"`
	Index    int    `json:"index"`
	Total    int    `json:"total"`
	cityDone bool
}

type PhotoCache struct {
	mu       sync.Mutex
	queue    []PhotoInfo     // ready to show
	shown    map[string]bool // already shown IDs
	maxPage  int
	client   *http.Client
	cfg      Config
}

func loadConfig() Config {
	interval := 15
	if v := os.Getenv("SLIDESHOW_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			interval = n
		}
	}

	port := "3000"
	if v := os.Getenv("PORT"); v != "" {
		port = v
	}

	deviceModel := os.Getenv("DEVICE_MODEL")
	if deviceModel == "" {
		deviceModel = "iPhone 14 Pro"
	}

	return Config{
		ImmichURL:         os.Getenv("IMMICH_URL"),
		ImmichAPIKey:      os.Getenv("IMMICH_API_KEY"),
		DeviceModel:       deviceModel,
		SlideshowInterval: interval,
		Port:              port,
	}
}

func main() {
	cfg := loadConfig()

	if cfg.ImmichURL == "" || cfg.ImmichAPIKey == "" {
		log.Fatal("IMMICH_URL and IMMICH_API_KEY environment variables are required")
	}

	tmpl, err := template.ParseFS(templateFS, "templates/index.html")
	if err != nil {
		log.Fatalf("Failed to parse template: %v", err)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	cache := &PhotoCache{
		shown:   make(map[string]bool),
		maxPage: 85000,
		client:  client,
		cfg:     cfg,
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, map[string]int{
			"Interval": cfg.SlideshowInterval,
		})
	})

	http.HandleFunc("/random", func(w http.ResponseWriter, r *http.Request) {
		p := cache.next()
		if p == nil {
			http.Error(w, "Loading photos...", http.StatusServiceUnavailable)
			return
		}
		if !p.cityDone {
			p.City = fetchCity(client, cfg, p.ID)
			p.cityDone = true
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		json.NewEncoder(w).Encode(p)
	})

	http.HandleFunc("/photo", func(w http.ResponseWriter, r *http.Request) {
		assetID := r.URL.Query().Get("id")
		if assetID == "" {
			http.Error(w, "Missing id", http.StatusBadRequest)
			return
		}

		thumbnailURL := fmt.Sprintf("%s/api/assets/%s/thumbnail?size=preview", cfg.ImmichURL, assetID)
		req, err := http.NewRequest("GET", thumbnailURL, nil)
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		req.Header.Set("x-api-key", cfg.ImmichAPIKey)

		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "Failed to fetch photo", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			http.Error(w, "Failed to fetch photo", http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		io.Copy(w, resp.Body)
	})

	addr := ":" + cfg.Port
	log.Printf("Immich iPad Photo Frame server starting on %s", addr)
	log.Printf("Immich URL: %s", cfg.ImmichURL)
	log.Printf("Device model: %s", cfg.DeviceModel)
	log.Printf("Slideshow interval: %ds", cfg.SlideshowInterval)
	log.Fatal(http.ListenAndServe(addr, nil))
}

// Search API types

type searchAsset struct {
	ID               string `json:"id"`
	FileCreatedAt    string `json:"fileCreatedAt"`
	OriginalFileName string `json:"originalFileName"`
}

type searchResponse struct {
	Assets struct {
		Items    []searchAsset `json:"items"`
		NextPage string       `json:"nextPage"`
	} `json:"assets"`
}

// fillQueue fetches 1 photo from a random page
func (c *PhotoCache) fillQueue() {
	for retries := 0; retries < 10; retries++ {
		page := rand.Intn(c.maxPage) + 1
		photos, _ := c.fetchPage(c.client, c.cfg, page, 1)
		if len(photos) == 0 {
			continue
		}
		p := photos[0]
		if !c.shown[p.ID] {
			c.queue = append(c.queue, p)
			log.Printf("Fetched page %d (shown: %d)", page, len(c.shown))
			return
		}
	}
}

func (c *PhotoCache) fetchPage(client *http.Client, cfg Config, page, pageSize int) ([]PhotoInfo, bool) {
	searchBody := map[string]interface{}{
		"type":  "IMAGE",
		"page":  page,
		"size":  pageSize,
		"model": cfg.DeviceModel,
	}

	bodyBytes, err := json.Marshal(searchBody)
	if err != nil {
		return nil, false
	}

	req, err := http.NewRequest("POST", cfg.ImmichURL+"/api/search/metadata", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, false
	}
	req.Header.Set("x-api-key", cfg.ImmichAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
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

	// Reset shown set when all photos have been shown (~84k)
	if len(c.shown) >= c.maxPage*10 {
		log.Printf("All %d photos shown, resetting cycle", len(c.shown))
		c.shown = make(map[string]bool)
	}

	return &p
}

func fetchCity(client *http.Client, cfg Config, assetID string) string {
	req, err := http.NewRequest("GET", cfg.ImmichURL+"/api/assets/"+assetID, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("x-api-key", cfg.ImmichAPIKey)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("City fetch error for %s: %v", assetID, err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var asset struct {
		ExifInfo struct {
			City    string `json:"city"`
			State   string `json:"state"`
			Country string `json:"country"`
		} `json:"exifInfo"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&asset); err != nil {
		return ""
	}

	parts := []string{}
	if asset.ExifInfo.City != "" {
		parts = append(parts, asset.ExifInfo.City)
	}
	if asset.ExifInfo.Country != "" {
		parts = append(parts, asset.ExifInfo.Country)
	}
	return strings.Join(parts, ", ")
}

var turkishMonths = []string{
	"Ocak", "Şubat", "Mart", "Nisan", "Mayıs", "Haziran",
	"Temmuz", "Ağustos", "Eylül", "Ekim", "Kasım", "Aralık",
}

func formatDate(isoDate string) string {
	t, err := time.Parse(time.RFC3339Nano, isoDate)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05.000Z", isoDate)
		if err != nil {
			return ""
		}
	}
	return fmt.Sprintf("%d %s %d", t.Day(), turkishMonths[t.Month()-1], t.Year())
}
