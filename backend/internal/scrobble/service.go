package scrobble

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/joaquinwaller/lastfmscrobblerweb/internal/lastfm"
	"github.com/joaquinwaller/lastfmscrobblerweb/internal/spotify"
)

type Service struct {
	Spotify *spotify.Client
	LastFM  *lastfm.Client
	Users   UserSessionRepository
	Market  string
}

type PreviewRequest struct {
	URL string `json:"url"`
}

type SearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
	Type  string `json:"type"`
}

type StartRequest struct {
	URL    string `json:"url"`
	Amount int    `json:"amount"`
}

type StartResult struct {
	Requested int             `json:"requested"`
	Sent      int             `json:"sent"`
	Errors    []string        `json:"errors,omitempty"`
	Preview   spotify.Preview `json:"preview"`
}

func (s *Service) Preview(ctx context.Context, req PreviewRequest) (spotify.Preview, error) {
	if s.Spotify == nil {
		return spotify.Preview{}, errors.New("spotify client not configured")
	}
	url := strings.TrimSpace(req.URL)
	if url == "" {
		return spotify.Preview{}, errors.New("url is required")
	}

	preview, tracks, err := s.Spotify.ResolveByURL(ctx, url, s.market())
	if err != nil {
		return spotify.Preview{}, err
	}
	if len(tracks) == 0 {
		return spotify.Preview{}, errors.New("no valid tracks found")
	}
	return preview, nil
}

func (s *Service) Search(ctx context.Context, req SearchRequest) ([]spotify.SearchItem, error) {
	if s.Spotify == nil {
		return nil, errors.New("spotify client not configured")
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, errors.New("query is required")
	}
	limit := req.Limit
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	return s.Spotify.Search(ctx, query, s.market(), limit, req.Type)
}

func (s *Service) Start(ctx context.Context, userID string, req StartRequest) (StartResult, error) {
	if s.Spotify == nil {
		return StartResult{}, errors.New("spotify client not configured")
	}
	if s.LastFM == nil {
		return StartResult{}, errors.New("lastfm client not configured")
	}
	if s.Users == nil {
		return StartResult{}, errors.New("session store not configured")
	}
	if userID == "" {
		return StartResult{}, errors.New("user id is required")
	}

	sessionKey, err := s.Users.GetLastFMSessionByID(ctx, userID)
	if err != nil {
		return StartResult{}, err
	}
	if sessionKey == "" {
		return StartResult{}, errors.New("lastfm session not found")
	}

	url := strings.TrimSpace(req.URL)
	if url == "" {
		return StartResult{}, errors.New("url is required")
	}
	if req.Amount < 0 {
		return StartResult{}, errors.New("amount must be >= 0")
	}
	if req.Amount > 3000 {
		return StartResult{}, errors.New("amount must be <= 3000")
	}

	preview, unique, err := s.Spotify.ResolveByURL(ctx, url, s.market())
	if err != nil {
		return StartResult{}, err
	}
	if len(unique) == 0 {
		return StartResult{}, errors.New("no valid tracks found")
	}

	baseTracks := prepareTracks(preview.Type, unique, req.Amount)
	tracks := fakeTimestamps(baseTracks)

	result := StartResult{
		Requested: len(tracks),
		Preview:   preview,
	}

	errorsSet := map[string]struct{}{}
	for _, chunk := range chunkTracks(tracks, 49) {
		sent, scrobbleErr := s.LastFM.Scrobble(ctx, sessionKey, chunk)
		if scrobbleErr == nil {
			result.Sent += sent
			continue
		}

		msg := scrobbleErr.Error()
		if _, seen := errorsSet[msg]; !seen {
			errorsSet[msg] = struct{}{}
			result.Errors = append(result.Errors, msg)
		}

		if msg == "LastfmRateLimitExceeded" || msg == "LastfmInvalidSessionKey" {
			break
		}
	}

	return result, nil
}

func (s *Service) market() string {
	if strings.TrimSpace(s.Market) == "" {
		return "US"
	}
	return s.Market
}

func prepareTracks(itemType string, unique []spotify.Track, amount int) []spotify.Track {
	if len(unique) == 0 {
		return nil
	}

	if amount <= 0 {
		out := make([]spotify.Track, len(unique))
		copy(out, unique)
		return out
	}

	if itemType == "track" {
		out := make([]spotify.Track, amount)
		for i := range out {
			out[i] = unique[0]
		}
		return out
	}

	if amount <= len(unique) {
		out := make([]spotify.Track, amount)
		copy(out, unique[:amount])
		return out
	}

	if amount > len(unique) {
		out := make([]spotify.Track, amount)
		for i := range out {
			out[i] = unique[i%len(unique)]
		}
		return out
	}

	return nil
}

func fakeTimestamps(tracks []spotify.Track) []lastfm.ScrobbleTrack {
	now := time.Now().Unix()
	offset := int64(0)

	out := make([]lastfm.ScrobbleTrack, len(tracks))
	for i, t := range tracks {
		durationSec := float64(t.DurationMS) / 1000.0
		min := durationSec / 2.0
		if min < 30 {
			min = 30
		}
		if min > 240 {
			min = 240
		}
		offset += int64(min)

		out[i] = lastfm.ScrobbleTrack{
			Name:      t.Name,
			Artist:    t.Artist,
			Album:     t.Album,
			Timestamp: now - offset,
		}
	}
	return out
}

func chunkTracks(tracks []lastfm.ScrobbleTrack, size int) [][]lastfm.ScrobbleTrack {
	if size <= 0 {
		size = 49
	}
	chunks := make([][]lastfm.ScrobbleTrack, 0, (len(tracks)+size-1)/size)
	for i := 0; i < len(tracks); i += size {
		end := i + size
		if end > len(tracks) {
			end = len(tracks)
		}
		chunks = append(chunks, tracks[i:end])
	}
	return chunks
}
