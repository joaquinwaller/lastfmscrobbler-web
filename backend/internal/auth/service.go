package auth

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

type Service struct {
	APIKey      string
	BaseURL     string
	FrontendURL string
	LastFM      LastFMClient
	Users       UserRepository
	Tokens      TokenSigner
}

func (s *Service) BuildAuthURL(token string) (string, error) {
	u, err := url.Parse("https://www.last.fm/api/auth")
	if err != nil {
		return "", err
	}

	q := u.Query()
	q.Set("api_key", s.APIKey)
	q.Set("token", token)
	if s.BaseURL != "" {
		q.Set("cb", strings.TrimRight(s.BaseURL, "/")+"/auth/lastfm/callback")
	}
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func (s *Service) StartLastFM(ctx context.Context) (string, error) {
	if s.LastFM == nil {
		return "", errors.New("lastfm client not configured")
	}

	token, err := s.LastFM.GetToken(ctx)
	if err != nil {
		return "", err
	}

	return s.BuildAuthURL(token)
}

type CompleteAuthResult struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

func (s *Service) CompleteLastFM(ctx context.Context, token string) (CompleteAuthResult, error) {
	if token == "" {
		return CompleteAuthResult{}, errors.New("token is required")
	}
	if s.LastFM == nil {
		return CompleteAuthResult{}, errors.New("lastfm client not configured")
	}
	if s.Users == nil {
		return CompleteAuthResult{}, errors.New("user repository not configured")
	}
	if s.Tokens == nil {
		return CompleteAuthResult{}, errors.New("token signer not configured")
	}

	session, err := s.LastFM.GetSession(ctx, token)
	if err != nil {
		return CompleteAuthResult{}, err
	}

	user, err := s.Users.UpsertByLastFM(ctx, session.Name, session.Key)
	if err != nil {
		return CompleteAuthResult{}, err
	}

	jwt, err := s.Tokens.Sign(user.ID, user.Username)
	if err != nil {
		return CompleteAuthResult{}, err
	}

	return CompleteAuthResult{
		Token: jwt,
		User:  user,
	}, nil
}
