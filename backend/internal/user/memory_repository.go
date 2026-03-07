package user

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"

	"github.com/joaquinwaller/lastfmscrobblerweb/internal/auth"
)

type record struct {
	ID               string
	Username         string
	LastFMSessionKey string
}

type Repository struct {
	mu     sync.Mutex
	byName map[string]record
	byID   map[string]record
}

func NewRepository() *Repository {
	return &Repository{
		byName: make(map[string]record),
		byID:   make(map[string]record),
	}
}

func (r *Repository) UpsertByLastFM(_ context.Context, username, sessionKey string) (auth.User, error) {
	if username == "" {
		return auth.User{}, errors.New("username is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.byName[username]
	if ok {
		existing.LastFMSessionKey = sessionKey
		r.byName[username] = existing
		r.byID[existing.ID] = existing
		return auth.User{
			ID:       existing.ID,
			Username: existing.Username,
		}, nil
	}

	id, err := newID()
	if err != nil {
		return auth.User{}, err
	}

	r.byName[username] = record{
		ID:               id,
		Username:         username,
		LastFMSessionKey: sessionKey,
	}
	r.byID[id] = r.byName[username]

	return auth.User{
		ID:       id,
		Username: username,
	}, nil
}

func (r *Repository) GetLastFMSessionByID(_ context.Context, userID string) (string, error) {
	if userID == "" {
		return "", errors.New("user id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.byID[userID]
	if !ok {
		return "", errors.New("user not found")
	}
	if existing.LastFMSessionKey == "" {
		return "", errors.New("lastfm session key not found")
	}
	return existing.LastFMSessionKey, nil
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
