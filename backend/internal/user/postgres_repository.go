package user

import (
	"context"
	"errors"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/joaquinwaller/lastfmscrobblerweb/internal/auth"
)

type PostgresRepository struct {
	mu   sync.Mutex
	conn *pgx.Conn
}

func NewPostgresRepository(conn *pgx.Conn) *PostgresRepository {
	return &PostgresRepository{conn: conn}
}

func (r *PostgresRepository) InitSchema(ctx context.Context) error {
	if r.conn == nil {
		return errors.New("postgres connection is required")
	}

	const query = `
CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY,
	username TEXT NOT NULL UNIQUE,
	lastfm_session_key TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`

	r.mu.Lock()
	defer r.mu.Unlock()

	_, err := r.conn.Exec(ctx, query)
	return err
}

func (r *PostgresRepository) UpsertByLastFM(ctx context.Context, username, sessionKey string) (auth.User, error) {
	if username == "" {
		return auth.User{}, errors.New("username is required")
	}
	if sessionKey == "" {
		return auth.User{}, errors.New("session key is required")
	}
	if r.conn == nil {
		return auth.User{}, errors.New("postgres connection is required")
	}

	id, err := newID()
	if err != nil {
		return auth.User{}, err
	}

	const query = `
INSERT INTO users (id, username, lastfm_session_key)
VALUES ($1, $2, $3)
ON CONFLICT (username)
DO UPDATE SET
	lastfm_session_key = EXCLUDED.lastfm_session_key,
	updated_at = NOW()
RETURNING id, username;`

	var user auth.User
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.conn.QueryRow(ctx, query, id, username, sessionKey).Scan(&user.ID, &user.Username); err != nil {
		return auth.User{}, err
	}

	return user, nil
}

func (r *PostgresRepository) GetLastFMSessionByID(ctx context.Context, userID string) (string, error) {
	if userID == "" {
		return "", errors.New("user id is required")
	}
	if r.conn == nil {
		return "", errors.New("postgres connection is required")
	}

	const query = `
SELECT lastfm_session_key
FROM users
WHERE id = $1;`

	r.mu.Lock()
	defer r.mu.Unlock()

	var sessionKey string
	if err := r.conn.QueryRow(ctx, query, userID).Scan(&sessionKey); err != nil {
		return "", err
	}
	if sessionKey == "" {
		return "", errors.New("lastfm session key not found")
	}
	return sessionKey, nil
}
