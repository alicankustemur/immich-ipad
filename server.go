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
	http.HandleFunc("/map", s.handleMap)
	http.HandleFunc("/weather", s.handleWeather)
	http.HandleFunc("/weather-icon/", s.handleWeatherIcon)
}

type locationInfo struct {
	City string
	Lat  float64
	Lon  float64
}

func (s *Server) fetchLocation(assetID string) locationInfo {
	req, err := http.NewRequest("GET", s.cfg.ImmichURL+"/api/assets/"+assetID, nil)
	if err != nil {
		return locationInfo{}
	}
	req.Header.Set("x-api-key", s.cfg.ImmichAPIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("Location fetch error for %s: %v", assetID, err)
		return locationInfo{}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return locationInfo{}
	}

	var asset struct {
		ExifInfo struct {
			City      string   `json:"city"`
			State     string   `json:"state"`
			Country   string   `json:"country"`
			Latitude  *float64 `json:"latitude"`
			Longitude *float64 `json:"longitude"`
		} `json:"exifInfo"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&asset); err != nil {
		return locationInfo{}
	}

	parts := []string{}
	if asset.ExifInfo.City != "" {
		parts = append(parts, asset.ExifInfo.City)
	}
	if asset.ExifInfo.Country != "" {
		parts = append(parts, asset.ExifInfo.Country)
	}

	loc := locationInfo{City: strings.Join(parts, ", ")}
	if asset.ExifInfo.Latitude != nil && asset.ExifInfo.Longitude != nil {
		loc.Lat = *asset.ExifInfo.Latitude
		loc.Lon = *asset.ExifInfo.Longitude
	}
	return loc
}
