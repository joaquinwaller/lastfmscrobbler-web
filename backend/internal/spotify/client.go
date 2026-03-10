package spotify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Track struct {
	Name       string `json:"name"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	Image      string `json:"image,omitempty"`
	DurationMS int    `json:"duration_ms"`
}

type Preview struct {
	Type      string  `json:"type"`
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Subtitle  string  `json:"subtitle"`
	Image     string  `json:"image,omitempty"`
	Total     int     `json:"total"`
	Sample    []Track `json:"sample"`
	Market    string  `json:"market"`
	SourceURL string  `json:"source_url"`
}

type SearchItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle"`
	Image     string `json:"image,omitempty"`
	SourceURL string `json:"source_url"`
}

type Client struct {
	ClientID     string
	ClientSecret string
	HTTP         *http.Client
	Now          func() time.Time

	accessToken string
	tokenExpiry time.Time
	mu          sync.RWMutex

	previewCache map[string]previewCacheEntry
	searchCache  map[string]searchCacheEntry
}

type previewCacheEntry struct {
	preview   Preview
	tracks    []Track
	expiresAt time.Time
}

type searchCacheEntry struct {
	items     []SearchItem
	expiresAt time.Time
}

const (
	previewCacheTTL = 2 * time.Minute
	searchCacheTTL  = 30 * time.Second
)

func New(clientID, clientSecret string) *Client {
	return &Client{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		HTTP: &http.Client{
			Timeout: 20 * time.Second,
		},
		Now:          time.Now,
		previewCache: make(map[string]previewCacheEntry),
		searchCache:  make(map[string]searchCacheEntry),
	}
}

func ParseSpotifyURL(raw string) (string, string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", errors.New("invalid spotify url")
	}
	if !strings.Contains(u.Host, "spotify.com") {
		return "", "", errors.New("invalid spotify url")
	}

	cleanPath := strings.TrimPrefix(u.Path, "/")
	if strings.HasPrefix(cleanPath, "intl-") {
		parts := strings.SplitN(cleanPath, "/", 2)
		if len(parts) == 2 {
			cleanPath = parts[1]
		}
	}

	parts := strings.Split(cleanPath, "/")
	for i, part := range parts {
		switch part {
		case "track", "album", "playlist", "artist":
			if i+1 < len(parts) {
				id := strings.TrimSpace(parts[i+1])
				if id != "" {
					return part, id, nil
				}
			}
		}
	}

	return "", "", errors.New("invalid spotify url")
}

func (c *Client) ResolveByURL(ctx context.Context, rawURL, market string) (Preview, []Track, error) {
	itemType, id, err := ParseSpotifyURL(rawURL)
	if err != nil {
		return Preview{}, nil, err
	}
	return c.ResolveByTypeAndID(ctx, itemType, id, market, rawURL)
}

func (c *Client) ResolveByTypeAndID(ctx context.Context, itemType, id, market, sourceURL string) (Preview, []Track, error) {
	if market == "" {
		market = "US"
	}
	cacheKey := previewCacheKey(itemType, id, market)
	if preview, tracks, ok := c.getCachedPreview(cacheKey); ok {
		preview.SourceURL = sourceURL
		return preview, tracks, nil
	}
	if err := c.refreshToken(ctx); err != nil {
		return Preview{}, nil, err
	}

	var (
		preview Preview
		tracks  []Track
		err     error
	)
	switch itemType {
	case "track":
		var t Track
		t, err = c.getTrack(ctx, id)
		if err != nil {
			return Preview{}, nil, err
		}
		preview = Preview{
			Type:      itemType,
			ID:        id,
			Title:     t.Name,
			Subtitle:  t.Artist,
			Image:     t.Image,
			Total:     1,
			Sample:    []Track{t},
			Market:    market,
			SourceURL: sourceURL,
		}
		tracks = []Track{t}
	case "album":
		preview, tracks, err = c.resolveAlbum(ctx, id, market, sourceURL)
	case "playlist":
		preview, tracks, err = c.resolvePlaylist(ctx, id, market, sourceURL)
	case "artist":
		preview, tracks, err = c.resolveArtist(ctx, id, market, sourceURL)
	default:
		return Preview{}, nil, errors.New("unsupported spotify type")
	}
	if err != nil {
		return Preview{}, nil, err
	}
	c.setCachedPreview(cacheKey, preview, tracks)
	preview.SourceURL = sourceURL
	return preview, tracks, nil
}

func (c *Client) Search(ctx context.Context, query, market string, limit int, itemType string) ([]SearchItem, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("query is required")
	}
	if err := c.refreshToken(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	if market == "" {
		market = "US"
	}
	cacheKey := searchCacheKey(query, market, limit, itemType)
	if items, ok := c.getCachedSearch(cacheKey); ok {
		return items, nil
	}
	searchType, allowedTypes, err := normalizeSearchType(itemType)
	if err != nil {
		return nil, err
	}

	q := url.Values{}
	q.Set("q", query)
	q.Set("type", searchType)
	q.Set("limit", fmt.Sprint(limit))
	q.Set("market", market)

	var out struct {
		Tracks struct {
			Items []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
				Album struct {
					Name   string `json:"name"`
					Images []struct {
						URL string `json:"url"`
					} `json:"images"`
				} `json:"album"`
			} `json:"items"`
		} `json:"tracks"`
		Albums struct {
			Items []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
				Images []struct {
					URL string `json:"url"`
				} `json:"images"`
			} `json:"items"`
		} `json:"albums"`
		Playlists struct {
			Items []struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Owner struct {
					DisplayName string `json:"display_name"`
					ID          string `json:"id"`
				} `json:"owner"`
				Images []struct {
					URL string `json:"url"`
				} `json:"images"`
			} `json:"items"`
		} `json:"playlists"`
		Artists struct {
			Items []struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Images []struct {
					URL string `json:"url"`
				} `json:"images"`
			} `json:"items"`
		} `json:"artists"`
	}

	if err := c.getAPI(ctx, "/search", q, &out); err != nil {
		return nil, err
	}

	items := make([]SearchItem, 0, limit*len(allowedTypes))
	for _, t := range allowedTypes {
		switch t {
		case "track":
			for _, track := range out.Tracks.Items {
				if track.ID == "" || strings.TrimSpace(track.Name) == "" {
					continue
				}
				artist := ""
				if len(track.Artists) > 0 {
					artist = track.Artists[0].Name
				}
				image := ""
				if len(track.Album.Images) > 0 {
					image = track.Album.Images[0].URL
				}
				items = append(items, SearchItem{
					Type:      "track",
					ID:        track.ID,
					Title:     track.Name,
					Subtitle:  artist,
					Image:     image,
					SourceURL: "https://open.spotify.com/track/" + track.ID,
				})
			}
		case "album":
			for _, album := range out.Albums.Items {
				if album.ID == "" || strings.TrimSpace(album.Name) == "" {
					continue
				}
				artist := ""
				if len(album.Artists) > 0 {
					artist = album.Artists[0].Name
				}
				image := ""
				if len(album.Images) > 0 {
					image = album.Images[0].URL
				}
				items = append(items, SearchItem{
					Type:      "album",
					ID:        album.ID,
					Title:     album.Name,
					Subtitle:  artist,
					Image:     image,
					SourceURL: "https://open.spotify.com/album/" + album.ID,
				})
			}
		case "playlist":
			for _, playlist := range out.Playlists.Items {
				if playlist.ID == "" || strings.TrimSpace(playlist.Name) == "" {
					continue
				}
				owner := playlist.Owner.DisplayName
				if owner == "" {
					owner = playlist.Owner.ID
				}
				image := ""
				if len(playlist.Images) > 0 {
					image = playlist.Images[0].URL
				}
				items = append(items, SearchItem{
					Type:      "playlist",
					ID:        playlist.ID,
					Title:     playlist.Name,
					Subtitle:  owner,
					Image:     image,
					SourceURL: "https://open.spotify.com/playlist/" + playlist.ID,
				})
			}
		case "artist":
			for _, artist := range out.Artists.Items {
				if artist.ID == "" || strings.TrimSpace(artist.Name) == "" {
					continue
				}
				image := ""
				if len(artist.Images) > 0 {
					image = artist.Images[0].URL
				}
				items = append(items, SearchItem{
					Type:      "artist",
					ID:        artist.ID,
					Title:     artist.Name,
					Subtitle:  "Artist",
					Image:     image,
					SourceURL: "https://open.spotify.com/artist/" + artist.ID,
				})
			}
		}
	}

	c.setCachedSearch(cacheKey, items)
	return items, nil
}

func normalizeSearchType(itemType string) (string, []string, error) {
	switch strings.ToLower(strings.TrimSpace(itemType)) {
	case "", "all":
		return "track,album,playlist,artist", []string{"track", "album", "playlist", "artist"}, nil
	case "track":
		return "track", []string{"track"}, nil
	case "album":
		return "album", []string{"album"}, nil
	case "playlist":
		return "playlist", []string{"playlist"}, nil
	case "artist":
		return "artist", []string{"artist"}, nil
	default:
		return "", nil, errors.New("invalid search type")
	}
}

func (c *Client) resolveAlbum(ctx context.Context, id, market, sourceURL string) (Preview, []Track, error) {
	album, err := c.getAlbum(ctx, id, market)
	if err != nil {
		return Preview{}, nil, err
	}

	tracks, err := c.getTracksFromAlbum(ctx, id, album.Name, album.Image, market)
	if err != nil {
		return Preview{}, nil, err
	}

	preview := Preview{
		Type:      "album",
		ID:        id,
		Title:     album.Name,
		Subtitle:  strings.Join(album.Artists, ", "),
		Image:     album.Image,
		Total:     len(tracks),
		Sample:    sampleTracks(tracks, 12),
		Market:    market,
		SourceURL: sourceURL,
	}
	return preview, tracks, nil
}

func (c *Client) resolvePlaylist(ctx context.Context, id, market, sourceURL string) (Preview, []Track, error) {
	playlist, err := c.getPlaylist(ctx, id, market)
	if err != nil {
		return Preview{}, nil, err
	}

	tracks, err := c.getTracksFromPlaylist(ctx, id, market)
	if err != nil {
		return Preview{}, nil, err
	}

	preview := Preview{
		Type:      "playlist",
		ID:        id,
		Title:     playlist.Name,
		Subtitle:  playlist.Owner,
		Image:     playlist.Image,
		Total:     len(tracks),
		Sample:    sampleTracks(tracks, 12),
		Market:    market,
		SourceURL: sourceURL,
	}
	return preview, tracks, nil
}

func (c *Client) resolveArtist(ctx context.Context, id, market, sourceURL string) (Preview, []Track, error) {
	artistName, image, err := c.getArtist(ctx, id)
	if err != nil {
		return Preview{}, nil, err
	}

	tracks, err := c.getRandomTracksFromArtist(ctx, id, market)
	if err != nil {
		return Preview{}, nil, err
	}

	preview := Preview{
		Type:      "artist",
		ID:        id,
		Title:     artistName,
		Subtitle:  "Random tracks where this artist is the main artist",
		Image:     image,
		Total:     len(tracks),
		Sample:    sampleTracks(tracks, 12),
		Market:    market,
		SourceURL: sourceURL,
	}
	return preview, tracks, nil
}

func (c *Client) refreshToken(ctx context.Context) error {
	c.mu.RLock()
	accessToken := c.accessToken
	tokenExpiry := c.tokenExpiry
	c.mu.RUnlock()
	if accessToken != "" && c.Now().Before(tokenExpiry.Add(-30*time.Second)) {
		return nil
	}

	if c.ClientID == "" || c.ClientSecret == "" {
		return errors.New("spotify credentials not configured")
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://accounts.spotify.com/api/token", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	basic := base64.StdEncoding.EncodeToString([]byte(c.ClientID + ":" + c.ClientSecret))
	req.Header.Set("Authorization", "Basic "+basic)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return spotifyRateLimitError(resp)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("spotify token failed: status %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if out.AccessToken == "" {
		return errors.New("spotify token empty")
	}

	c.mu.Lock()
	c.accessToken = out.AccessToken
	c.tokenExpiry = c.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	c.mu.Unlock()
	return nil
}

func (c *Client) getTrack(ctx context.Context, id string) (Track, error) {
	var out struct {
		Name       string `json:"name"`
		DurationMS int    `json:"duration_ms"`
		Artists    []struct {
			Name string `json:"name"`
		} `json:"artists"`
		Album struct {
			Name   string `json:"name"`
			Images []struct {
				URL string `json:"url"`
			} `json:"images"`
		} `json:"album"`
	}

	if err := c.getAPI(ctx, "/tracks/"+id, nil, &out); err != nil {
		return Track{}, err
	}
	if out.DurationMS <= 30_000 {
		return Track{}, errors.New("track too short to scrobble")
	}
	artist := ""
	if len(out.Artists) > 0 {
		artist = out.Artists[0].Name
	}
	image := ""
	if len(out.Album.Images) > 0 {
		image = out.Album.Images[0].URL
	}
	return Track{
		Name:       out.Name,
		Artist:     artist,
		DurationMS: out.DurationMS,
		Album:      out.Album.Name,
		Image:      image,
	}, nil
}

type albumMeta struct {
	Name    string
	Artists []string
	Image   string
}

func (c *Client) getAlbum(ctx context.Context, id, market string) (albumMeta, error) {
	var out struct {
		Name    string `json:"name"`
		Artists []struct {
			Name string `json:"name"`
		} `json:"artists"`
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
	}
	q := url.Values{}
	if market != "" {
		q.Set("market", market)
	}
	if err := c.getAPI(ctx, "/albums/"+id, q, &out); err != nil {
		return albumMeta{}, err
	}
	artists := make([]string, 0, len(out.Artists))
	for _, a := range out.Artists {
		if a.Name != "" {
			artists = append(artists, a.Name)
		}
	}
	image := ""
	if len(out.Images) > 0 {
		image = out.Images[0].URL
	}
	return albumMeta{Name: out.Name, Artists: artists, Image: image}, nil
}

func (c *Client) getTracksFromAlbum(ctx context.Context, id, albumName, albumImage, market string) ([]Track, error) {
	tracks := make([]Track, 0)
	limit := 50
	offset := 0
	total := 1

	for offset < total {
		q := url.Values{}
		q.Set("limit", fmt.Sprint(limit))
		q.Set("offset", fmt.Sprint(offset))
		if market != "" {
			q.Set("market", market)
		}

		var out struct {
			Total int `json:"total"`
			Items []struct {
				Name       string `json:"name"`
				DurationMS int    `json:"duration_ms"`
				Artists    []struct {
					Name string `json:"name"`
					ID   string `json:"id"`
				} `json:"artists"`
			} `json:"items"`
		}

		if err := c.getAPI(ctx, "/albums/"+id+"/tracks", q, &out); err != nil {
			return nil, err
		}
		total = out.Total

		for _, t := range out.Items {
			if t.DurationMS <= 30_000 {
				continue
			}
			artist := ""
			if len(t.Artists) > 0 {
				artist = t.Artists[0].Name
			}
			tracks = append(tracks, Track{
				Name:       t.Name,
				Artist:     artist,
				DurationMS: t.DurationMS,
				Album:      albumName,
				Image:      albumImage,
			})
		}
		offset += limit
	}

	return tracks, nil
}

type playlistMeta struct {
	Name  string
	Owner string
	Image string
}

func (c *Client) getPlaylist(ctx context.Context, id, market string) (playlistMeta, error) {
	var out struct {
		Name   string `json:"name"`
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
		Owner struct {
			DisplayName string `json:"display_name"`
			ID          string `json:"id"`
		} `json:"owner"`
	}
	q := url.Values{}
	q.Set("fields", "name,images,owner(display_name,id)")
	if market != "" {
		q.Set("market", market)
	}
	if err := c.getAPI(ctx, "/playlists/"+id, q, &out); err != nil {
		return playlistMeta{}, err
	}
	owner := out.Owner.DisplayName
	if owner == "" {
		owner = out.Owner.ID
	}
	image := ""
	if len(out.Images) > 0 {
		image = out.Images[0].URL
	}
	return playlistMeta{Name: out.Name, Owner: owner, Image: image}, nil
}

func (c *Client) getTracksFromPlaylist(ctx context.Context, id, market string) ([]Track, error) {
	tracks := make([]Track, 0)
	limit := 100
	offset := 0
	total := 1

	for offset < total {
		q := url.Values{}
		q.Set("limit", fmt.Sprint(limit))
		q.Set("offset", fmt.Sprint(offset))
		q.Set("fields", "total,items(track(name,duration_ms,artists(name,id),album(name,images)))")
		if market != "" {
			q.Set("market", market)
		}

		var out struct {
			Total int `json:"total"`
			Items []struct {
				Track *struct {
					Name       string `json:"name"`
					DurationMS int    `json:"duration_ms"`
					Artists    []struct {
						Name string `json:"name"`
						ID   string `json:"id"`
					} `json:"artists"`
					Album struct {
						Name   string `json:"name"`
						Images []struct {
							URL string `json:"url"`
						} `json:"images"`
					} `json:"album"`
				} `json:"track"`
			} `json:"items"`
		}

		if err := c.getAPI(ctx, "/playlists/"+id+"/tracks", q, &out); err != nil {
			return nil, err
		}
		total = out.Total

		for _, item := range out.Items {
			t := item.Track
			if t == nil || t.DurationMS <= 30_000 {
				continue
			}
			artist := ""
			if len(t.Artists) > 0 {
				artist = t.Artists[0].Name
			}
			image := ""
			if len(t.Album.Images) > 0 {
				image = t.Album.Images[0].URL
			}
			tracks = append(tracks, Track{
				Name:       t.Name,
				Artist:     artist,
				DurationMS: t.DurationMS,
				Album:      t.Album.Name,
				Image:      image,
			})
		}

		offset += limit
	}

	return tracks, nil
}

func (c *Client) getArtist(ctx context.Context, id string) (string, string, error) {
	var out struct {
		Name   string `json:"name"`
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
	}
	if err := c.getAPI(ctx, "/artists/"+id, nil, &out); err != nil {
		return "", "", err
	}
	image := ""
	if len(out.Images) > 0 {
		image = out.Images[0].URL
	}
	return out.Name, image, nil
}

func (c *Client) getRandomTracksFromArtist(ctx context.Context, artistID, market string) ([]Track, error) {
	type album struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
	}

	albums := make([]album, 0)
	limit := 50
	offset := 0
	total := 1

	for offset < total {
		q := url.Values{}
		q.Set("include_groups", "album,single,compilation")
		q.Set("limit", fmt.Sprint(limit))
		q.Set("offset", fmt.Sprint(offset))
		if market != "" {
			q.Set("market", market)
		}

		var out struct {
			Total int     `json:"total"`
			Items []album `json:"items"`
		}
		if err := c.getAPI(ctx, "/artists/"+artistID+"/albums", q, &out); err != nil {
			return nil, err
		}
		albums = append(albums, out.Items...)
		total = out.Total
		offset += limit
	}

	tracks := make([]Track, 0)
	for _, alb := range albums {
		q := url.Values{}
		q.Set("limit", "50")
		q.Set("offset", "0")
		if market != "" {
			q.Set("market", market)
		}

		var out struct {
			Total int `json:"total"`
			Items []struct {
				Name       string `json:"name"`
				DurationMS int    `json:"duration_ms"`
				Artists    []struct {
					Name string `json:"name"`
					ID   string `json:"id"`
				} `json:"artists"`
			} `json:"items"`
		}

		if err := c.getAPI(ctx, "/albums/"+alb.ID+"/tracks", q, &out); err != nil {
			return nil, err
		}

		for _, t := range out.Items {
			if t.DurationMS <= 30_000 {
				continue
			}
			if len(t.Artists) == 0 || t.Artists[0].ID != artistID {
				continue
			}
			image := ""
			if len(alb.Images) > 0 {
				image = alb.Images[0].URL
			}
			tracks = append(tracks, Track{
				Name:       t.Name,
				Artist:     t.Artists[0].Name,
				DurationMS: t.DurationMS,
				Album:      alb.Name,
				Image:      image,
			})
		}
	}

	rand.Shuffle(len(tracks), func(i, j int) {
		tracks[i], tracks[j] = tracks[j], tracks[i]
	})

	return tracks, nil
}

func (c *Client) getAPI(ctx context.Context, path string, q url.Values, out any) error {
	if q == nil {
		q = url.Values{}
	}
	u := "https://api.spotify.com/v1" + path
	if encoded := q.Encode(); encoded != "" {
		u += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	c.mu.RLock()
	accessToken := c.accessToken
	c.mu.RUnlock()
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return spotifyRateLimitError(resp)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("spotify api failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) getCachedPreview(key string) (Preview, []Track, bool) {
	c.mu.RLock()
	entry, ok := c.previewCache[key]
	c.mu.RUnlock()
	if !ok || c.Now().After(entry.expiresAt) {
		if ok {
			c.mu.Lock()
			delete(c.previewCache, key)
			c.mu.Unlock()
		}
		return Preview{}, nil, false
	}

	preview := entry.preview
	tracks := append([]Track(nil), entry.tracks...)
	return preview, tracks, true
}

func (c *Client) setCachedPreview(key string, preview Preview, tracks []Track) {
	c.mu.Lock()
	c.previewCache[key] = previewCacheEntry{
		preview:   preview,
		tracks:    append([]Track(nil), tracks...),
		expiresAt: c.Now().Add(previewCacheTTL),
	}
	c.mu.Unlock()
}

func (c *Client) getCachedSearch(key string) ([]SearchItem, bool) {
	c.mu.RLock()
	entry, ok := c.searchCache[key]
	c.mu.RUnlock()
	if !ok || c.Now().After(entry.expiresAt) {
		if ok {
			c.mu.Lock()
			delete(c.searchCache, key)
			c.mu.Unlock()
		}
		return nil, false
	}

	return append([]SearchItem(nil), entry.items...), true
}

func (c *Client) setCachedSearch(key string, items []SearchItem) {
	c.mu.Lock()
	c.searchCache[key] = searchCacheEntry{
		items:     append([]SearchItem(nil), items...),
		expiresAt: c.Now().Add(searchCacheTTL),
	}
	c.mu.Unlock()
}

func previewCacheKey(itemType, id, market string) string {
	return strings.ToLower(strings.TrimSpace(itemType)) + ":" + strings.TrimSpace(id) + ":" + strings.ToUpper(strings.TrimSpace(market))
}

func searchCacheKey(query, market string, limit int, itemType string) string {
	return strings.ToLower(strings.TrimSpace(query)) + ":" + strings.ToUpper(strings.TrimSpace(market)) + ":" + fmt.Sprint(limit) + ":" + strings.ToLower(strings.TrimSpace(itemType))
}

func spotifyRateLimitError(resp *http.Response) error {
	retryAfter := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if retryAfter != "" {
		return fmt.Errorf("spotify rate limit reached; wait %s seconds and try again", retryAfter)
	}
	return errors.New("spotify rate limit reached; wait a minute and try again")
}

func sampleTracks(tracks []Track, max int) []Track {
	if len(tracks) <= max {
		return append([]Track(nil), tracks...)
	}
	return append([]Track(nil), tracks[:max]...)
}
