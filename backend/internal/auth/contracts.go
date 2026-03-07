package auth

import (
	"context"

	"github.com/joaquinwaller/lastfmscrobblerweb/internal/lastfm"
)

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type LastFMClient interface {
	GetToken(ctx context.Context) (string, error)
	GetSession(ctx context.Context, token string) (lastfm.Session, error)
}

type UserRepository interface {
	UpsertByLastFM(ctx context.Context, username, sessionKey string) (User, error)
	GetLastFMSessionByID(ctx context.Context, userID string) (string, error)
}

type TokenSigner interface {
	Sign(userID, username string) (string, error)
}
