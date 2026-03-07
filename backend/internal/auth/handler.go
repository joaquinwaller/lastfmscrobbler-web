package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type Handler struct {
	Service *Service
	Signer  *Signer
}

func (h *Handler) StartLastFM(w http.ResponseWriter, r *http.Request) {
	url, err := h.Service.StartLastFM(r.Context())
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (h *Handler) StartLastFMJSON(w http.ResponseWriter, r *http.Request) {
	if h.Service == nil || h.Service.LastFM == nil {
		h.writeError(w, http.StatusInternalServerError, "lastfm client not configured")
		return
	}

	token, err := h.Service.LastFM.GetToken(r.Context())
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	authURL, err := h.Service.BuildAuthURL(token)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]string{
		"token":    token,
		"auth_url": authURL,
	})
}

func (h *Handler) PollLastFM(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		h.writeError(w, http.StatusBadRequest, "missing token")
		return
	}

	result, err := h.Service.CompleteLastFM(r.Context(), token)
	if err != nil {
		if err.Error() == "LastfmTokenNotAuthorized" || err.Error() == "lastfm session data incomplete" {
			h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
			return
		}
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CallbackLastFM(w http.ResponseWriter, r *http.Request) {
	if apiErr := r.URL.Query().Get("error"); apiErr != "" {
		h.writeError(w, http.StatusBadRequest, apiErr)
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		h.writeError(w, http.StatusBadRequest, "missing token")
		return
	}

	result, err := h.Service.CompleteLastFM(r.Context(), token)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if h.Service.FrontendURL != "" {
		redirectURL, err := buildFrontendCallbackURL(h.Service.FrontendURL, result)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	h.writeJSON(w, http.StatusOK, result)
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	if h.Signer == nil {
		h.writeError(w, http.StatusInternalServerError, "token signer not configured")
		return
	}

	token, err := bearerToken(r)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	claims, err := h.Signer.ParseAllowExpired(token)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	newToken, err := h.Signer.Sign(claims.Subject, claims.Username)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"token": newToken,
		"user": map[string]string{
			"id":       claims.Subject,
			"username": claims.Username,
		},
	})
}

func buildFrontendCallbackURL(frontendURL string, result CompleteAuthResult) (string, error) {
	base := strings.TrimRight(frontendURL, "/")
	u, err := url.Parse(base + "/auth/callback")
	if err != nil {
		return "", err
	}

	fragment := url.Values{}
	fragment.Set("token", result.Token)
	fragment.Set("user_id", result.User.ID)
	fragment.Set("username", result.User.Username)
	u.Fragment = fragment.Encode()

	return u.String(), nil
}

func bearerToken(r *http.Request) (string, error) {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if authz == "" {
		return "", errors.New("missing authorization header")
	}
	const bearer = "Bearer "
	if !strings.HasPrefix(authz, bearer) {
		return "", errors.New("invalid authorization header")
	}
	token := strings.TrimSpace(strings.TrimPrefix(authz, bearer))
	if token == "" {
		return "", errors.New("missing bearer token")
	}
	return token, nil
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, map[string]string{"error": message})
}
