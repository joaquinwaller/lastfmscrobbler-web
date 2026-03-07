package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joaquinwaller/lastfmscrobblerweb/internal/auth"
	httprouter "github.com/joaquinwaller/lastfmscrobblerweb/internal/http"
	"github.com/joaquinwaller/lastfmscrobblerweb/internal/lastfm"
	"github.com/joaquinwaller/lastfmscrobblerweb/internal/scrobble"
	"github.com/joaquinwaller/lastfmscrobblerweb/internal/spotify"
	"github.com/joaquinwaller/lastfmscrobblerweb/internal/user"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	port := envOrDefault("PORT", "8080")
	baseURL := envOrDefault("BASE_URL", "http://localhost:"+port)
	frontendURL := os.Getenv("FRONTEND_URL")
	lastFMKey := os.Getenv("LASTFM_API_KEY")
	lastFMSecret := envOrDefault("LASTFM_API_SECRET", os.Getenv("LASTFM_SHARED_SECRET"))
	jwtSecret := os.Getenv("JWT_SECRET")
	jwtTTL := envDurationHours("JWT_TTL_HOURS", 24*365*5)
	databaseURL := os.Getenv("DATABASE_URL")
	spotifyClientID := os.Getenv("SPOTIFY_CLIENT_ID")
	spotifyClientSecret := os.Getenv("SPOTIFY_CLIENT_SECRET")
	spotifyMarket := envOrDefault("SPOTIFY_MARKET", "US")

	lastFMClient := lastfm.New(lastFMKey, lastFMSecret)
	signer := auth.NewSigner(jwtSecret, "lastfmscrobblerweb", jwtTTL)

	userRepo, closeDB, err := buildUserRepository(databaseURL)
	if err != nil {
		log.Fatalf("failed to initialize user repository: %v", err)
	}
	if closeDB != nil {
		defer closeDB()
	}

	authService := &auth.Service{
		APIKey:      lastFMKey,
		BaseURL:     baseURL,
		FrontendURL: frontendURL,
		LastFM:      lastFMClient,
		Users:       userRepo,
		Tokens:      signer,
	}
	authHandler := &auth.Handler{Service: authService, Signer: signer}
	spotifyClient := spotify.New(spotifyClientID, spotifyClientSecret)
	scrobbleService := &scrobble.Service{
		Spotify: spotifyClient,
		LastFM:  lastFMClient,
		Users:   userRepo,
		Market:  spotifyMarket,
	}
	scrobbleHandler := &scrobble.Handler{
		Service: scrobbleService,
		Tokens:  signer,
	}

	router := httprouter.NewRouter(authHandler, scrobbleHandler, frontendURL)

	addr := ":" + port
	log.Printf("Server running on %s", addr)
	log.Fatal(http.ListenAndServe(addr, router))
}

func buildUserRepository(databaseURL string) (auth.UserRepository, func(), error) {
	if databaseURL == "" {
		log.Print("DATABASE_URL not set, using in-memory user repository")
		return user.NewRepository(), nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return nil, nil, err
	}

	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close(ctx)
		return nil, nil, err
	}

	repo := user.NewPostgresRepository(conn)
	if err := repo.InitSchema(ctx); err != nil {
		_ = conn.Close(ctx)
		return nil, nil, err
	}

	log.Print("Using PostgreSQL user repository")
	return repo, func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = conn.Close(closeCtx)
	}, nil
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envDurationHours(key string, fallbackHours int) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return time.Duration(fallbackHours) * time.Hour
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		log.Printf("invalid %s=%q, using fallback %dh", key, raw, fallbackHours)
		return time.Duration(fallbackHours) * time.Hour
	}
	return time.Duration(n) * time.Hour
}
