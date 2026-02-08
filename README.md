# Immich iPad Photo Frame

Turn an old iPad into a digital photo frame that displays random photos from your self-hosted [Immich](https://immich.app) server.

Built for iPad 1 (iOS 5.1.1 Safari), but works on any browser.

## How It Works

A Go server proxies photos from Immich and serves a minimal HTML slideshow. Photos are preloaded in batches of 5 for smooth, flicker-free transitions. Each photo displays for a configurable interval with the date and location shown in the bottom-right corner.

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
source .env && export IMMICH_URL IMMICH_API_KEY ALBUM_ID SLIDESHOW_INTERVAL PORT
go run main.go
```

Open `http://localhost:3000` in a browser.

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `IMMICH_URL` | Immich server URL (e.g. `http://192.168.1.100:2283`) | *required* |
| `IMMICH_API_KEY` | Immich API key | *required* |
| `ALBUM_ID` | Immich album ID to show photos from (leave empty for all photos) | |
| `SLIDESHOW_INTERVAL` | Seconds between photos | `10` |
| `PORT` | Server port | `3000` |

Generate an API key in Immich under **User Settings > API Keys**.

To find an album ID, open the album in Immich and copy the UUID from the URL:
`http://your-immich/albums/<album-id>`

## iPad Setup

1. Connect the iPad to the same network as the server
2. Open Safari and go to `http://<server-ip>:3000`
3. Add to Home Screen for full-screen mode (hides Safari toolbar)
