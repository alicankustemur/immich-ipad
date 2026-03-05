package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.tmpl.Execute(w, map[string]interface{}{
		"Interval": s.cfg.SlideshowInterval,
		"ShowMap":  s.cfg.ShowMap,
	})
}

func (s *Server) handleRandom(w http.ResponseWriter, r *http.Request) {
	p := s.cache.next()
	if p == nil {
		http.Error(w, "Loading photos...", http.StatusServiceUnavailable)
		return
	}
	if !p.cityDone {
		loc := s.fetchLocation(p.ID)
		p.City = loc.City
		p.Lat = loc.Lat
		p.Lon = loc.Lon
		p.cityDone = true
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	json.NewEncoder(w).Encode(p)
}

func (s *Server) handleMap(w http.ResponseWriter, r *http.Request) {
	latStr := r.URL.Query().Get("lat")
	lonStr := r.URL.Query().Get("lon")
	if latStr == "" || lonStr == "" {
		http.Error(w, "Missing lat/lon", http.StatusBadRequest)
		return
	}

	lat, err1 := strconv.ParseFloat(latStr, 64)
	lon, err2 := strconv.ParseFloat(lonStr, 64)
	if err1 != nil || err2 != nil {
		http.Error(w, "Invalid lat/lon", http.StatusBadRequest)
		return
	}

	zoom := 14
	if z := r.URL.Query().Get("zoom"); z != "" {
		if zv, err := strconv.Atoi(z); err == nil && zv >= 1 && zv <= 18 {
			zoom = zv
		}
	}
	tileX, tileY := latLonToTile(lat, lon, zoom)
	px, py := latLonToPixel(lat, lon, zoom)

	// Fetch 3x3 grid of tiles and stitch into 768x768 image
	grid := image.NewRGBA(image.Rect(0, 0, 768, 768))
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			tx := tileX + dx
			ty := tileY + dy
			tileURL := fmt.Sprintf("https://tile.openstreetmap.org/%d/%d/%d.png", zoom, tx, ty)
			req, err := http.NewRequest("GET", tileURL, nil)
			if err != nil {
				continue
			}
			req.Header.Set("User-Agent", "immich-ipad/1.0")
			resp, err := s.client.Do(req)
			if err != nil {
				continue
			}
			tileImg, err := png.Decode(resp.Body)
			resp.Body.Close()
			if err != nil {
				continue
			}
			destX := (dx + 1) * 256
			destY := (dy + 1) * 256
			draw.Draw(grid, image.Rect(destX, destY, destX+256, destY+256), tileImg, image.Point{}, draw.Src)
		}
	}

	// Pin position in the stitched 768x768 image
	centerX := 256 + px
	centerY := 256 + py

	// Crop 256x256 centered on the pin
	cropX := centerX - 128
	cropY := centerY - 128
	if cropX < 0 {
		cropX = 0
	}
	if cropY < 0 {
		cropY = 0
	}
	if cropX+256 > 768 {
		cropX = 768 - 256
	}
	if cropY+256 > 768 {
		cropY = 768 - 256
	}

	cropped := image.NewRGBA(image.Rect(0, 0, 256, 256))
	draw.Draw(cropped, cropped.Bounds(), grid, image.Point{X: cropX, Y: cropY}, draw.Src)

	// Draw pin at center of cropped image
	pinX := centerX - cropX
	pinY := centerY - cropY
	drawMarker(cropped, pinX, pinY)

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	var buf bytes.Buffer
	png.Encode(&buf, cropped)
	w.Write(buf.Bytes())
}

func latLonToTile(lat, lon float64, zoom int) (int, int) {
	n := math.Pow(2, float64(zoom))
	x := int((lon + 180.0) / 360.0 * n)
	latRad := lat * math.Pi / 180.0
	y := int((1.0 - math.Log(math.Tan(latRad)+1.0/math.Cos(latRad))/math.Pi) / 2.0 * n)
	return x, y
}

func latLonToPixel(lat, lon float64, zoom int) (int, int) {
	n := math.Pow(2, float64(zoom))
	xf := (lon + 180.0) / 360.0 * n
	latRad := lat * math.Pi / 180.0
	yf := (1.0 - math.Log(math.Tan(latRad)+1.0/math.Cos(latRad))/math.Pi) / 2.0 * n
	// Pixel within the 256x256 tile
	px := int((xf - math.Floor(xf)) * 256)
	py := int((yf - math.Floor(yf)) * 256)
	return px, py
}

var pinImage image.Image

func loadPinImage() {
	img, err := png.Decode(bytes.NewReader(pinPNG))
	if err != nil {
		log.Fatalf("Failed to decode pin.png: %v", err)
	}
	// Scale pin to 24px wide, maintain aspect ratio
	bounds := img.Bounds()
	targetW := 24
	targetH := targetW * bounds.Dy() / bounds.Dx()
	scaled := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	for sy := 0; sy < targetH; sy++ {
		for sx := 0; sx < targetW; sx++ {
			srcX := bounds.Min.X + sx*bounds.Dx()/targetW
			srcY := bounds.Min.Y + sy*bounds.Dy()/targetH
			scaled.Set(sx, sy, img.At(srcX, srcY))
		}
	}
	pinImage = scaled
}

func drawMarker(img *image.RGBA, px, py int) {
	if pinImage == nil {
		return
	}
	pb := pinImage.Bounds()
	// Position pin so the bottom-center of the pin is at the location
	offsetX := px - pb.Dx()/2
	offsetY := py - pb.Dy()
	destRect := image.Rect(offsetX, offsetY, offsetX+pb.Dx(), offsetY+pb.Dy())
	draw.DrawMask(img, destRect, pinImage, pb.Min, pinImage, pb.Min, draw.Over)
}

func (s *Server) handleWeather(w http.ResponseWriter, r *http.Request) {
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%s&longitude=%s&current_weather=true",
		s.cfg.WeatherLat, s.cfg.WeatherLon,
	)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	resp, err := s.client.Do(req)
	if err != nil {
		http.Error(w, "Failed to fetch weather", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	var data struct {
		CurrentWeather struct {
			Temperature float64 `json:"temperature"`
			WeatherCode int     `json:"weathercode"`
			IsDay       int     `json:"is_day"`
		} `json:"current_weather"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		http.Error(w, "Failed to parse weather", http.StatusInternalServerError)
		return
	}

	isDay := data.CurrentWeather.IsDay == 1
	icon := weatherIcon(data.CurrentWeather.WeatherCode, isDay)
	result := map[string]interface{}{
		"temp": fmt.Sprintf("%d", int(data.CurrentWeather.Temperature)),
		"icon": "/weather-icon/" + icon,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=900")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleWeatherIcon(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[len("/weather-icon/"):]
	data, err := weatherFS.ReadFile("templates/weather/" + name)
	if err != nil {
		http.Error(w, "Icon not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

func weatherIcon(code int, isDay bool) string {
	switch {
	case code == 0:
		if isDay {
			return "sunny.png"
		}
		return "clear_night.png"
	case code == 1:
		if isDay {
			return "mostly_sunny.png"
		}
		return "mostly_clear_night.png"
	case code == 2:
		if isDay {
			return "partly_cloudy.png"
		}
		return "partly_cloudy_night.png"
	case code == 3:
		if isDay {
			return "mostly_cloudy_day.png"
		}
		return "mostly_cloudy_night.png"
	case code == 45 || code == 48:
		return "haze_fog_dust_smoke.png"
	case code >= 51 && code <= 55:
		return "drizzle.png"
	case code >= 56 && code <= 57:
		return "sleet_hail.png"
	case code >= 61 && code <= 63:
		return "showers_rain.png"
	case code == 65:
		return "heavy_rain.png"
	case code >= 66 && code <= 67:
		return "wintry_mix_rain_snow.png"
	case code >= 71 && code <= 75:
		return "heavy_snow.png"
	case code == 77:
		return "flurries.png"
	case code >= 80 && code <= 82:
		if isDay {
			return "scattered_showers_day.png"
		}
		return "scattered_showers_night.png"
	case code >= 85 && code <= 86:
		return "snow_showers_snow.png"
	case code == 95:
		if isDay {
			return "isolated_scattered_tstorms_day.png"
		}
		return "isolated_scattered_tstorms_night.png"
	case code == 96 || code == 99:
		return "strong_tstorms.png"
	default:
		return "cloudy.png"
	}
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
