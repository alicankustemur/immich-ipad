package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.tmpl.Execute(w, map[string]int{
		"Interval": s.cfg.SlideshowInterval,
	})
}

func (s *Server) handleRandom(w http.ResponseWriter, r *http.Request) {
	p := s.cache.next()
	if p == nil {
		http.Error(w, "Loading photos...", http.StatusServiceUnavailable)
		return
	}
	if !p.cityDone {
		p.City = s.fetchCity(p.ID)
		p.cityDone = true
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	json.NewEncoder(w).Encode(p)
}

func (s *Server) handlePhoto(w http.ResponseWriter, r *http.Request) {
	assetID := r.URL.Query().Get("id")
	if assetID == "" {
		http.Error(w, "Missing id", http.StatusBadRequest)
		return
	}

	thumbnailURL := fmt.Sprintf("%s/api/assets/%s/thumbnail?size=preview", s.cfg.ImmichURL, assetID)
	req, err := http.NewRequest("GET", thumbnailURL, nil)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("x-api-key", s.cfg.ImmichAPIKey)

	resp, err := s.client.Do(req)
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
}
