# Immich iPad Photo Frame

Turn an old iPad into a digital photo frame that displays random photos from your self-hosted [Immich](https://immich.app) server.

Built for iPad 1 (iOS 5.1.1 Safari), but works on any browser.

## How It Works

A Go server proxies random photos from Immich and serves a minimal HTML slideshow. Each photo shows the date and location (if available) in the bottom-right corner.

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
source .env && export IMMICH_URL IMMICH_API_KEY SLIDESHOW_INTERVAL PORT
go run main.go
```

Open `http://localhost:3000` in a browser.

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `IMMICH_URL` | Immich server URL (e.g. `http://192.168.1.100:2283`) | *required* |
| `IMMICH_API_KEY` | Immich API key | *required* |
| `SLIDESHOW_INTERVAL` | Seconds between photos | `10` |
| `PORT` | Server port | `3000` |

Generate an API key in Immich under **User Settings > API Keys**.

## iPad Setup

1. Connect the iPad to the same network as the server
2. Open Safari and go to `http://<server-ip>:3000`
3. Add to Home Screen for full-screen mode (hides Safari toolbar)
