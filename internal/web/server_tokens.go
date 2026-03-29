package web

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s *Server) handleAPITokens(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		s.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !s.config.CanManageTokens(claims) || s.auth == nil || s.auth.TokenService() == nil || !s.auth.TokenService().Enabled() {
		s.writeError(w, http.StatusForbidden, "user is not allowed to manage api tokens")
		return
	}
	subject := SubjectFromClaims(claims)
	if subject == "" {
		s.writeError(w, http.StatusForbidden, "authenticated subject is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		tokens, err := s.auth.TokenService().List(r.Context(), subject)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, tokens)
	case http.MethodPost:
		var req struct {
			Label    string   `json:"label"`
			Scopes   []string `json:"scopes"`
			TTLHours int      `json:"ttlHours"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		tokenValue, tokenView, err := s.auth.TokenService().Create(r.Context(), subject, claims, req.Label, req.Scopes, req.TTLHours)
		if err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "limit") {
				status = http.StatusConflict
			}
			s.writeError(w, status, err.Error())
			return
		}
		s.writeJSON(w, http.StatusCreated, map[string]interface{}{
			"token": tokenValue,
			"item":  tokenView,
		})
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleRevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		s.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !s.config.CanManageTokens(claims) || s.auth == nil || s.auth.TokenService() == nil || !s.auth.TokenService().Enabled() {
		s.writeError(w, http.StatusForbidden, "user is not allowed to manage api tokens")
		return
	}
	subject := SubjectFromClaims(claims)
	if subject == "" {
		s.writeError(w, http.StatusForbidden, "authenticated subject is required")
		return
	}
	tokenID := strings.TrimSpace(r.PathValue("tokenId"))
	if tokenID == "" {
		s.writeError(w, http.StatusBadRequest, "token id is required")
		return
	}
	if err := s.auth.TokenService().Revoke(r.Context(), subject, tokenID); err != nil {
		if err == ErrAPITokenNotFound {
			s.writeError(w, http.StatusNotFound, err.Error())
			return
		}
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
