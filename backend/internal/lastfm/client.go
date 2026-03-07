package lastfm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	APIKey    string
	APISecret string
	HTTP      *http.Client
}

type Session struct {
	Name string
	Key  string
}

type ScrobbleTrack struct {
	Name      string
	Artist    string
	Album     string
	Timestamp int64
}

func New(apiKey, apiSecret string) *Client {
	return &Client{
		APIKey:    apiKey,
		APISecret: apiSecret,
		HTTP: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) GetToken(ctx context.Context) (string, error) {
	u, err := url.Parse("https://ws.audioscrobbler.com/2.0/")
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("method", "auth.getToken")
	q.Set("api_key", c.APIKey)
	q.Set("format", "json")

	// api_sig requerido
	apiSig := BuildAPISig(map[string]string{
		"api_key": c.APIKey,
		"method":  "auth.getToken",
	}, c.APISecret)

	q.Set("api_sig", apiSig)

	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("lastfm getToken failed: status %d", resp.StatusCode)
	}

	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Token == "" {
		return "", errors.New("lastfm token empty")
	}
	return out.Token, nil
}

func (c *Client) GetSession(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, errors.New("token is required")
	}

	u, err := url.Parse("https://ws.audioscrobbler.com/2.0/")
	if err != nil {
		return Session{}, err
	}

	q := u.Query()
	q.Set("method", "auth.getSession")
	q.Set("api_key", c.APIKey)
	q.Set("token", token)
	q.Set("format", "json")

	apiSig := BuildAPISig(map[string]string{
		"api_key": c.APIKey,
		"method":  "auth.getSession",
		"token":   token,
	}, c.APISecret)
	q.Set("api_sig", apiSig)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Session{}, err
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Session{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return Session{}, err
	}

	var out struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
		Session struct {
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"session"`
	}
	_ = json.Unmarshal(body, &out)

	if resp.StatusCode != 200 {
		switch out.Error {
		case 14:
			return Session{}, errors.New("LastfmTokenNotAuthorized")
		case 11, 16:
			return Session{}, errors.New("LastfmServiceUnavailable")
		default:
			msg := strings.TrimSpace(string(body))
			if msg == "" {
				msg = out.Message
			}
			return Session{}, fmt.Errorf("lastfm getSession failed: status %d: %s", resp.StatusCode, msg)
		}
	}

	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return Session{}, err
		}
	}
	if out.Error != 0 {
		switch out.Error {
		case 14:
			return Session{}, errors.New("LastfmTokenNotAuthorized")
		case 11, 16:
			return Session{}, errors.New("LastfmServiceUnavailable")
		default:
			return Session{}, fmt.Errorf("lastfm error %d: %s", out.Error, out.Message)
		}
	}
	if out.Session.Name == "" || out.Session.Key == "" {
		return Session{}, errors.New("lastfm session data incomplete")
	}

	return Session{
		Name: out.Session.Name,
		Key:  out.Session.Key,
	}, nil
}

func (c *Client) Scrobble(ctx context.Context, sessionKey string, tracks []ScrobbleTrack) (int, error) {
	if sessionKey == "" {
		return 0, errors.New("session key is required")
	}
	if len(tracks) == 0 {
		return 0, nil
	}

	params := map[string]string{
		"api_key": c.APIKey,
		"method":  "track.scrobble",
		"sk":      sessionKey,
	}

	for i, track := range tracks {
		if strings.TrimSpace(track.Name) == "" || strings.TrimSpace(track.Artist) == "" {
			continue
		}

		params[fmt.Sprintf("track[%d]", i)] = track.Name
		params[fmt.Sprintf("artist[%d]", i)] = track.Artist
		if track.Album != "" {
			params[fmt.Sprintf("album[%d]", i)] = track.Album
		}
		ts := track.Timestamp
		if ts <= 0 {
			ts = time.Now().Unix()
		}
		params[fmt.Sprintf("timestamp[%d]", i)] = strconv.FormatInt(ts, 10)
	}

	params["format"] = "json"
	params["api_sig"] = BuildAPISig(params, c.APISecret)

	form := url.Values{}
	for key, value := range params {
		form.Set(key, value)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://ws.audioscrobbler.com/2.0/", strings.NewReader(form.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var out struct {
			Error   int    `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &out) == nil {
			switch out.Error {
			case 9:
				return 0, errors.New("LastfmInvalidSessionKey")
			case 29:
				return 0, errors.New("LastfmRateLimitExceeded")
			}
		}
		return 0, fmt.Errorf("lastfm scrobble failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return len(tracks), nil
}
