package http

import (
	"net/url"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/joaquinwaller/lastfmscrobblerweb/internal/auth"
	"github.com/joaquinwaller/lastfmscrobblerweb/internal/scrobble"
)

func NewRouter(authHandler *auth.Handler, scrobbleHandler *scrobble.Handler, frontendURL string) http.Handler {
	r := chi.NewRouter()
	r.Use(cors(frontendURL))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	r.Get("/auth/lastfm/start", authHandler.StartLastFM)
	r.Get("/auth/lastfm/start.json", authHandler.StartLastFMJSON)
	r.Get("/auth/lastfm/poll", authHandler.PollLastFM)
	r.Get("/auth/lastfm/callback", authHandler.CallbackLastFM)
	r.Post("/auth/refresh", authHandler.Refresh)
	r.Post("/scrobble/preview", scrobbleHandler.Preview)
	r.Post("/scrobble/search", scrobbleHandler.Search)
	r.Post("/scrobble/start", scrobbleHandler.Start)

	return r
}

func cors(frontendURL string) func(http.Handler) http.Handler {
	allowedOrigins := buildAllowedOrigins(frontendURL)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (len(allowedOrigins) == 0 || allowedOrigins[origin]) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func buildAllowedOrigins(frontendURL string) map[string]bool {
	trimmed := strings.TrimRight(strings.TrimSpace(frontendURL), "/")
	if trimmed == "" {
		return nil
	}

	allowed := map[string]bool{trimmed: true}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return allowed
	}

	host := parsed.Hostname()
	if host == "" || host == "localhost" {
		return allowed
	}

	counterpartHost := host
	if strings.HasPrefix(host, "www.") {
		counterpartHost = strings.TrimPrefix(host, "www.")
	} else {
		counterpartHost = "www." + host
	}

	counterpart := parsed.Scheme + "://" + counterpartHost
	if port := parsed.Port(); port != "" {
		counterpart += ":" + port
	}

	allowed[counterpart] = true
	return allowed
}
