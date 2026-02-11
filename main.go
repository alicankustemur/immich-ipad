package main

import (
	"embed"
	"log"
	"net/http"
	"text/template"
	"time"
)

//go:embed templates/index.html
var templateFS embed.FS

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

	s := &Server{
		cfg:    cfg,
		client: client,
		cache: &PhotoCache{
			shown:   make(map[string]bool),
			maxPage: 85000,
			client:  client,
			cfg:     cfg,
		},
		tmpl: tmpl,
	}

	s.routes()

	addr := ":" + cfg.Port
	log.Printf("Immich iPad Photo Frame server starting on %s", addr)
	log.Printf("Immich URL: %s", cfg.ImmichURL)
	log.Printf("Device model: %s", cfg.DeviceModel)
	log.Printf("Slideshow interval: %ds", cfg.SlideshowInterval)
	log.Fatal(http.ListenAndServe(addr, nil))
}
