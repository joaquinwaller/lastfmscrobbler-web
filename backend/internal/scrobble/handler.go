package scrobble

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/joaquinwaller/lastfmscrobblerweb/internal/auth"
)

type Handler struct {
	Service *Service
	Tokens  *auth.Signer
}

func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	var req PreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	preview, err := h.Service.Preview(r.Context(), req)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{"preview": preview})
}

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	items, err := h.Service.Search(r.Context(), req)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authenticate(r)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	var req StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.Service.Start(r.Context(), claims.Subject, req)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, result)
}

func (h *Handler) authenticate(r *http.Request) (auth.Claims, error) {
	if h.Tokens == nil {
		return auth.Claims{}, errors.New("token verifier not configured")
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if authz == "" {
		return auth.Claims{}, errors.New("missing authorization header")
	}
	const bearer = "Bearer "
	if !strings.HasPrefix(authz, bearer) {
		return auth.Claims{}, errors.New("invalid authorization header")
	}
	token := strings.TrimSpace(strings.TrimPrefix(authz, bearer))
	if token == "" {
		return auth.Claims{}, errors.New("missing bearer token")
	}
	return h.Tokens.Parse(token)
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, map[string]string{"error": message})
}
