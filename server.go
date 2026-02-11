package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"text/template"
)

type Server struct {
	cfg    Config
	client *http.Client
	cache  *PhotoCache
	tmpl   *template.Template
}

func (s *Server) routes() {
	http.HandleFunc("/", s.handleIndex)
	http.HandleFunc("/random", s.handleRandom)
	http.HandleFunc("/photo", s.handlePhoto)
}

func (s *Server) fetchCity(assetID string) string {
	req, err := http.NewRequest("GET", s.cfg.ImmichURL+"/api/assets/"+assetID, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("x-api-key", s.cfg.ImmichAPIKey)

	resp, err := s.client.Do(req)
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
