package scrobble

import "context"

type UserSessionRepository interface {
	GetLastFMSessionByID(ctx context.Context, userID string) (string, error)
}
