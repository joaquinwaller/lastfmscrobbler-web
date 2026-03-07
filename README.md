# LastFmScrobbler

Bulk scrobbling web app for Last.fm with Spotify URL preview/search support.

## Live App

- App: https://lastfmscrobbler.lat
- API health check: https://api.lastfmscrobbler.lat/health

## Stack

- Frontend: React + Vite
- Backend: Go
- Database: PostgreSQL
- Reverse proxy / TLS: Caddy
- Runtime: Docker Compose

## Features

- Last.fm authentication
- Spotify URL preview
- Spotify search
- Bulk scrobbling workflow
- PostgreSQL-backed user storage

## Run with Docker

Requirements:
- Docker
- Docker Compose

From the project root:

```bash
docker compose up --build
```

Default services:
- Frontend: https://lastfmscrobbler.lat
- API: https://api.lastfmscrobbler.lat
- Health check: https://api.lastfmscrobbler.lat/health

This repository is currently configured for the deployed domain setup above. If you want to run it on localhost instead, update the domain-related values in:

- `docker-compose.yml`
- `backend/.env`
- `frontend/.env.example`

For a fresh backend environment, start from:

```bash
backend/.env.example
```

## Environment

Backend runtime values are loaded from:

```bash
backend/.env
```

Frontend API base URL example:

```bash
frontend/.env.example
```

## Last.fm Callback

Use this callback URL in your Last.fm API settings:

```text
https://api.lastfmscrobbler.lat/auth/lastfm/callback
```

## Stop

```bash
docker compose down
```

Remove containers and database volume:

```bash
docker compose down -v
```
