package main

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ImmichURL         string
	ImmichAPIKey      string
	DeviceModels      []string
	SlideshowInterval int
	Port              string
	ShowMap           bool
	ShowWeather       bool
	WeatherLat        string
	WeatherLon        string
}

// parseDeviceModels splits a comma-separated DEVICE_MODEL value into individual
// camera models, dropping empty entries and surrounding whitespace/quotes.
func parseDeviceModels(v string) []string {
	var models []string
	for _, m := range strings.Split(v, ",") {
		m = strings.TrimSpace(m)
		m = strings.Trim(m, `"'`)
		m = strings.TrimSpace(m)
		if m != "" {
			models = append(models, m)
		}
	}
	return models
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

	// DEVICE_MODELS is the current name; DEVICE_MODEL is kept as a fallback.
	deviceModels := parseDeviceModels(os.Getenv("DEVICE_MODELS"))
	if len(deviceModels) == 0 {
		deviceModels = parseDeviceModels(os.Getenv("DEVICE_MODEL"))
	}
	if len(deviceModels) == 0 {
		deviceModels = []string{"iPhone 14 Pro", "iPhone XS"}
	}

	showMap := os.Getenv("SHOW_MAP") == "true"
	showWeather := os.Getenv("SHOW_WEATHER") != "false"

	weatherLat := os.Getenv("WEATHER_LAT")
	if weatherLat == "" {
		weatherLat = "40.9337"
	}
	weatherLon := os.Getenv("WEATHER_LON")
	if weatherLon == "" {
		weatherLon = "29.1297"
	}

	return Config{
		ImmichURL:         os.Getenv("IMMICH_URL"),
		ImmichAPIKey:      os.Getenv("IMMICH_API_KEY"),
		DeviceModels:      deviceModels,
		SlideshowInterval: interval,
		Port:              port,
		ShowMap:           showMap,
		ShowWeather:       showWeather,
		WeatherLat:        weatherLat,
		WeatherLon:        weatherLon,
	}
}
