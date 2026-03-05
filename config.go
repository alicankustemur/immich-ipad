package main

import (
	"os"
	"strconv"
)

type Config struct {
	ImmichURL         string
	ImmichAPIKey      string
	DeviceModel       string
	SlideshowInterval int
	Port              string
	ShowMap           bool
	WeatherLat        string
	WeatherLon        string
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

	showMap := os.Getenv("SHOW_MAP") == "true"

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
		DeviceModel:       deviceModel,
		SlideshowInterval: interval,
		Port:              port,
		ShowMap:           showMap,
		WeatherLat:        weatherLat,
		WeatherLon:        weatherLon,
	}
}
