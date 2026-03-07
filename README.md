# Immich iPad Photo Frame

Turn an old iPad into a digital photo frame that displays random photos from your self-hosted [Immich](https://immich.app) server.

Built for iPad 1 (iOS 5.1.1 Safari), but works on any browser.

## Features

- Truly random photo selection across your entire library — picks from random pages for diverse years and locations
- Dynamic photo count — automatically discovers total photos via Immich API and refreshes every hour to include newly uploaded photos
- No repeats until all photos have been shown
- Lazy city/country fetching from EXIF data, cached per photo
- Photo info overlay (Turkish date, location) with fade-in effect
- Optional weather display and map overlay
- Device model filtering — show only photos from a specific camera (e.g. iPhone 14 Pro)
- Screenshots automatically excluded
- Minimal server load — 1 search API call per photo cycle
- Resilient client — survives server restarts, power outages, and network drops with automatic recovery (retries every slideshow interval, watchdog timer, manual XHR timeout for iPad 1 compatibility)
- Connects to Immich via Docker network for direct container communication

## Quick Start

### With Docker

```bash
cp .env.example .env
# Edit .env with your values
docker compose up --build
```

### Without Docker

```bash
cp .env.example .env
# Edit .env with your values
set -a && source .env && set +a
go run .
```

Open `http://localhost:3000` in a browser.

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `IMMICH_URL` | Immich server URL (e.g. `http://192.168.1.100:2283`) | *required* |
| `IMMICH_API_KEY` | Immich API key | *required* |
| `DEVICE_MODEL` | Camera model to filter by | `iPhone 14 Pro` |
| `SLIDESHOW_INTERVAL` | Seconds between photos | `15` |
| `PORT` | Server port | `3000` |
| `SHOW_WEATHER` | Show weather overlay | `true` |
| `SHOW_MAP` | Show map overlay | `false` |
| `WEATHER_LAT` | Weather location latitude | `40.9337` |
| `WEATHER_LON` | Weather location longitude | `29.1297` |

Generate an API key in Immich under **User Settings > API Keys**.

## Project Structure

```
main.go        — entry point, template embed
server.go      — Server struct, routes, city lookup
handlers.go    — HTTP handlers (index, random, photo)
cache.go       — PhotoCache, random page fetching
config.go      — environment config loading
format.go      — PhotoInfo type, Turkish date formatting
immich.go      — Immich API types
templates/
  index.html   — slideshow UI (iPad 1 compatible)
```

## iPad Setup

1. Connect the iPad to the same network as the server
2. Open Safari and go to `http://<server-ip>:3000`
3. Add to Home Screen for full-screen mode (hides Safari toolbar)
