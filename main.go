package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"text/template"
	"time"
)

//go:embed templates/index.html
var templateFS embed.FS

type Config struct {
	ImmichURL         string
	ImmichAPIKey      string
	AlbumID           string
	SlideshowInterval int
	Port              string
}

type PhotoInfo struct {
	ID    string `json:"id"`
	Date  string `json:"date"`
	City  string `json:"city"`
	Index int    `json:"index"`
	Total int    `json:"total"`
}

type AlbumCache struct {
	mu     sync.Mutex
	photos []PhotoInfo
	index  int
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

	return Config{
		ImmichURL:         os.Getenv("IMMICH_URL"),
		ImmichAPIKey:      os.Getenv("IMMICH_API_KEY"),
		AlbumID:           os.Getenv("ALBUM_ID"),
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
	cache := &AlbumCache{}

	if cfg.AlbumID != "" {
		for {
			if err := cache.refresh(client, cfg); err != nil {
				log.Printf("Waiting for Immich... (%v)", err)
				time.Sleep(5 * time.Second)
				continue
			}
			log.Printf("Loaded %d photos from album", len(cache.photos))
			break
		}
		go func() {
			for {
				time.Sleep(5 * time.Minute)
				if err := cache.refresh(client, cfg); err != nil {
					log.Printf("Error refreshing album cache: %v", err)
				} else {
					log.Printf("Refreshed album cache: %d photos", len(cache.photos))
				}
			}
		}()
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
		info, err := getRandomPhoto(client, cfg, cache)
		if err != nil {
			log.Printf("Error getting random photo info: %v", err)
			http.Error(w, "Failed to get random photo", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		json.NewEncoder(w).Encode(info)
	})

	http.HandleFunc("/batch", func(w http.ResponseWriter, r *http.Request) {
		count := 2
		if v := r.URL.Query().Get("count"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 20 {
				count = n
			}
		}
		var batch []PhotoInfo
		for i := 0; i < count; i++ {
			info, err := getRandomPhoto(client, cfg, cache)
			if err != nil {
				log.Printf("Error getting photo %d/%d for batch: %v", i+1, count, err)
				continue
			}
			batch = append(batch, *info)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		json.NewEncoder(w).Encode(batch)
	})

	http.HandleFunc("/photo", func(w http.ResponseWriter, r *http.Request) {
		assetID := r.URL.Query().Get("id")
		if assetID == "" {
			info, err := getRandomPhoto(client, cfg, cache)
			if err != nil {
				log.Printf("Error getting random photo: %v", err)
				http.Error(w, "Failed to get random photo", http.StatusBadGateway)
				return
			}
			assetID = info.ID
		}

		thumbnailURL := fmt.Sprintf("%s/api/assets/%s/thumbnail?size=preview", cfg.ImmichURL, assetID)
		req, err := http.NewRequest("GET", thumbnailURL, nil)
		if err != nil {
			log.Printf("Error creating thumbnail request: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		req.Header.Set("x-api-key", cfg.ImmichAPIKey)

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Error fetching thumbnail: %v", err)
			http.Error(w, "Failed to fetch photo", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Printf("Immich thumbnail returned status %d for asset %s", resp.StatusCode, assetID)
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
	log.Printf("Slideshow interval: %ds", cfg.SlideshowInterval)
	log.Fatal(http.ListenAndServe(addr, nil))
}

// Album cache: stores only lightweight PhotoInfo, shuffles in place

type albumAsset struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	FileCreatedAt string   `json:"fileCreatedAt"`
	ExifInfo      exifInfo `json:"exifInfo"`
}

type exifInfo struct {
	City    string `json:"city"`
	State   string `json:"state"`
	Country string `json:"country"`
}

type albumResponse struct {
	Assets []albumAsset `json:"assets"`
}

func (c *AlbumCache) refresh(client *http.Client, cfg Config) error {
	url := fmt.Sprintf("%s/api/albums/%s", cfg.ImmichURL, cfg.AlbumID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("x-api-key", cfg.ImmichAPIKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("calling Immich API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Immich API returned status %d: %s", resp.StatusCode, string(body))
	}

	var album albumResponse
	if err := json.NewDecoder(resp.Body).Decode(&album); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	// Convert to lightweight PhotoInfo immediately, discard heavy Asset data
	var photos []PhotoInfo
	idx := 1
	for _, a := range album.Assets {
		if a.Type == "IMAGE" {
			photos = append(photos, PhotoInfo{
				ID:    a.ID,
				Date:  formatDate(a.FileCreatedAt),
				City:  buildLocation(a.ExifInfo),
				Index: idx,
			})
			idx++
		}
	}
	// album goes out of scope here — GC reclaims the ~1.2MB JSON data

	total := len(photos)
	for i := range photos {
		photos[i].Total = total
	}

	// Shuffle in place
	rand.Shuffle(len(photos), func(i, j int) {
		photos[i], photos[j] = photos[j], photos[i]
	})

	c.mu.Lock()
	c.photos = photos
	c.index = 0
	c.mu.Unlock()
	log.Printf("Shuffled %d photos for new cycle", total)
	return nil
}

func (c *AlbumCache) next() *PhotoInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.photos) == 0 {
		return nil
	}
	if c.index >= len(c.photos) {
		// All photos shown — reshuffle
		rand.Shuffle(len(c.photos), func(i, j int) {
			c.photos[i], c.photos[j] = c.photos[j], c.photos[i]
		})
		c.index = 0
		log.Printf("Reshuffled %d photos for new cycle", len(c.photos))
	}
	p := &c.photos[c.index]
	c.index++
	return p
}

func getRandomPhoto(client *http.Client, cfg Config, cache *AlbumCache) (*PhotoInfo, error) {
	if cfg.AlbumID != "" {
		p := cache.next()
		if p == nil {
			return nil, fmt.Errorf("album cache is empty")
		}
		return p, nil
	}

	for attempts := 0; attempts < 5; attempts++ {
		url := fmt.Sprintf("%s/api/assets/random?count=1", cfg.ImmichURL)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("x-api-key", cfg.ImmichAPIKey)

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("calling Immich API: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("Immich API returned status %d: %s", resp.StatusCode, string(body))
		}

		var assets []albumAsset
		if err := json.NewDecoder(resp.Body).Decode(&assets); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}

		if len(assets) == 0 {
			continue
		}

		if assets[0].Type == "IMAGE" {
			a := assets[0]
			return &PhotoInfo{
				ID:   a.ID,
				Date: formatDate(a.FileCreatedAt),
				City: buildLocation(a.ExifInfo),
			}, nil
		}

		log.Printf("Skipping non-image asset (type=%s), retrying...", assets[0].Type)
	}

	return nil, fmt.Errorf("could not find a photo after 5 attempts")
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

func buildLocation(exif exifInfo) string {
	parts := []string{}
	if exif.City != "" {
		parts = append(parts, exif.City)
	}
	if exif.State != "" {
		parts = append(parts, exif.State)
	}
	if exif.Country != "" {
		parts = append(parts, exif.Country)
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}
