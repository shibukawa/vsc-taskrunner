package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vsc-taskrunner/internal/uiconfig"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type contextKey string

const (
	claimsContextKey  contextKey = "runtask.auth.claims"
	defaultSessionTTL            = 24 * time.Hour
)

// SessionClaims is stored in the signed session cookie.
type SessionClaims struct {
	Claims map[string]interface{} `json:"claims"`
	Expiry int64                  `json:"exp"`
}

// Authenticator handles OIDC login/logout and session verification.
type Authenticator struct {
	enabled      bool
	oidcProvider *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	oauth2Config oauth2.Config
	config       *uiconfig.UIConfig

	sessionCookie string
	stateCookie   string
	nonceCookie   string
	pkceCookie    string
	signerKey     []byte
}

// NewAuthenticator constructs an OIDC authenticator from UI config.
func NewAuthenticator(cfg *uiconfig.UIConfig) (*Authenticator, error) {
	a := &Authenticator{
		enabled:       !cfg.Auth.NoAuth,
		config:        cfg,
		sessionCookie: "runtask_session",
		stateCookie:   "runtask_oidc_state",
		nonceCookie:   "runtask_oidc_nonce",
		pkceCookie:    "runtask_oidc_pkce_verifier",
	}
	if !a.enabled {
		return a, nil
	}

	if cfg.Auth.OIDCIssuer == "" || cfg.Auth.OIDCClientID == "" {
		return nil, fmt.Errorf("oidcIssuer and oidcClientID are required when auth.noAuth=false")
	}

	provider, err := oidc.NewProvider(context.Background(), cfg.Auth.OIDCIssuer)
	if err != nil {
		return nil, fmt.Errorf("initialize OIDC provider: %w", err)
	}

	redirectURL := strings.TrimRight(cfg.PublicBaseURL(), "/") + "/auth/callback"
	a.oauth2Config = oauth2.Config{
		ClientID:     cfg.Auth.OIDCClientID,
		ClientSecret: cfg.Auth.OIDCClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:   provider.Endpoint().AuthURL,
			TokenURL:  provider.Endpoint().TokenURL,
			AuthStyle: oauth2.AuthStyleInParams,
		},
		RedirectURL: redirectURL,
		Scopes:      []string{oidc.ScopeOpenID, "profile", "email"},
	}
	a.oidcProvider = provider
	a.verifier = provider.Verifier(&oidc.Config{ClientID: cfg.Auth.OIDCClientID})

	if cfg.Auth.SessionSecret != "" {
		a.signerKey = []byte(cfg.Auth.SessionSecret)
	} else {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate session secret: %w", err)
		}
		a.signerKey = key
	}

	return a, nil
}

// Enabled reports whether authentication is active.
func (a *Authenticator) Enabled() bool {
	return a != nil && a.enabled
}

// RequireAuth validates session cookie and injects claims into context.
func (a *Authenticator) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	if !a.Enabled() {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		claims, err := a.sessionClaimsFromRequest(r)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":     "unauthorized",
				"loginPath": "/auth/login",
			})
			return
		}
		if !a.config.MatchUser(claims) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "user is not allowed by auth.allowUsers"})
			return
		}
		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next(w, r.WithContext(ctx))
	}
}

// HandleLogin redirects user to the OIDC provider authorization endpoint.
func (a *Authenticator) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if !a.Enabled() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	state, err := randomToken(24)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	nonce, err := randomToken(24)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pkceVerifier := oauth2.GenerateVerifier()

	a.setShortCookie(w, a.stateCookie, state)
	a.setShortCookie(w, a.nonceCookie, nonce)
	a.setShortCookie(w, a.pkceCookie, pkceVerifier)

	oauthCfg := a.oauth2ConfigForRequest(r)
	authURL := oauthCfg.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(pkceVerifier))
	http.Redirect(w, r, authURL, http.StatusFound)
}

// HandleCallback validates OIDC callback, creates session cookie and redirects.
func (a *Authenticator) HandleCallback(w http.ResponseWriter, r *http.Request) {
	if !a.Enabled() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		http.Error(w, "missing state or code", http.StatusBadRequest)
		return
	}

	stateCookie, err := r.Cookie(a.stateCookie)
	if err != nil || stateCookie.Value != state {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	nonceCookie, err := r.Cookie(a.nonceCookie)
	if err != nil {
		http.Error(w, "missing nonce", http.StatusBadRequest)
		return
	}
	pkceCookie, err := r.Cookie(a.pkceCookie)
	if err != nil {
		http.Error(w, "missing pkce verifier", http.StatusBadRequest)
		return
	}

	oauthCfg := a.oauth2ConfigForRequest(r)
	token, err := oauthCfg.Exchange(r.Context(), code, oauth2.VerifierOption(pkceCookie.Value))
	if err != nil {
		http.Error(w, fmt.Sprintf("exchange token: %v", err), http.StatusUnauthorized)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		http.Error(w, "id_token missing in token response", http.StatusUnauthorized)
		return
	}

	idToken, err := a.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, fmt.Sprintf("verify id_token: %v", err), http.StatusUnauthorized)
		return
	}

	if nonceCookie.Value != "" {
		var tokenClaims struct {
			Nonce string `json:"nonce"`
		}
		if err := idToken.Claims(&tokenClaims); err == nil && tokenClaims.Nonce != "" && tokenClaims.Nonce != nonceCookie.Value {
			http.Error(w, "invalid nonce", http.StatusUnauthorized)
			return
		}
	}

	claims := map[string]interface{}{}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "parse claims", http.StatusUnauthorized)
		return
	}

	if !a.config.MatchUser(claims) {
		http.Error(w, "user is not allowed by auth.allowUsers", http.StatusForbidden)
		return
	}

	payload := SessionClaims{
		Claims: claims,
		Expiry: time.Now().Add(defaultSessionTTL).Unix(),
	}

	encoded, err := a.signSession(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     a.sessionCookie,
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(payload.Expiry, 0),
	})
	a.clearCookie(w, a.stateCookie)
	a.clearCookie(w, a.nonceCookie)

	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *Authenticator) oauth2ConfigForRequest(r *http.Request) oauth2.Config {
	cfg := a.oauth2Config
	cfg.RedirectURL = a.redirectURLForRequest(r)
	return cfg
}

func (a *Authenticator) redirectURLForRequest(r *http.Request) string {
	return strings.TrimRight(a.requestBaseURL(r), "/") + "/auth/callback"
}

func (a *Authenticator) requestBaseURL(r *http.Request) string {
	fallback := a.config.PublicBaseURL()
	if r == nil {
		return fallback
	}

	scheme := forwardedHeaderValue(r, "X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	host := forwardedHeaderValue(r, "X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	if host == "" {
		return fallback
	}

	prefix := strings.TrimRight(forwardedHeaderValue(r, "X-Forwarded-Prefix"), "/")
	return scheme + "://" + host + prefix
}

func forwardedHeaderValue(r *http.Request, name string) string {
	value := strings.TrimSpace(r.Header.Get(name))
	if value == "" {
		return ""
	}
	if comma := strings.Index(value, ","); comma >= 0 {
		value = value[:comma]
	}
	return strings.TrimSpace(value)
}

// HandleLogout clears session cookie.
func (a *Authenticator) HandleLogout(w http.ResponseWriter, r *http.Request) {
	a.clearCookie(w, a.sessionCookie)
	a.clearCookie(w, a.stateCookie)
	a.clearCookie(w, a.nonceCookie)
	a.clearCookie(w, a.pkceCookie)
	http.Redirect(w, r, "/", http.StatusFound)
}

// ClaimsFromContext returns claims injected by RequireAuth middleware.
func ClaimsFromContext(ctx context.Context) map[string]interface{} {
	claims, _ := ctx.Value(claimsContextKey).(map[string]interface{})
	return claims
}

// SubjectFromClaims returns a stable user identifier from claims.
func SubjectFromClaims(claims map[string]interface{}) string {
	if claims == nil {
		return ""
	}
	if v, ok := claims["email"].(string); ok && v != "" {
		return v
	}
	if v, ok := claims["sub"].(string); ok && v != "" {
		return v
	}
	return ""
}

func (a *Authenticator) sessionClaimsFromRequest(r *http.Request) (map[string]interface{}, error) {
	cookie, err := r.Cookie(a.sessionCookie)
	if err != nil {
		return nil, err
	}
	payload, err := a.verifySession(cookie.Value)
	if err != nil {
		return nil, err
	}
	if time.Now().Unix() > payload.Expiry {
		return nil, errors.New("session expired")
	}
	return payload.Claims, nil
}

func (a *Authenticator) signSession(payload SessionClaims) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	sig := hmacSHA256([]byte(body), a.signerKey)
	return body + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (a *Authenticator) verifySession(value string) (*SessionClaims, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return nil, errors.New("invalid session format")
	}
	bodyPart := parts[0]
	sigPart := parts[1]

	expected := hmacSHA256([]byte(bodyPart), a.signerKey)
	actual, err := base64.RawURLEncoding.DecodeString(sigPart)
	if err != nil {
		return nil, err
	}
	if !hmac.Equal(expected, actual) {
		return nil, errors.New("invalid session signature")
	}

	body, err := base64.RawURLEncoding.DecodeString(bodyPart)
	if err != nil {
		return nil, err
	}
	var payload SessionClaims
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload.Claims == nil {
		payload.Claims = map[string]interface{}{}
	}
	return &payload, nil
}

func (a *Authenticator) setShortCookie(w http.ResponseWriter, name string, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   300,
	})
}

func (a *Authenticator) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

func randomToken(byteLen int) (string, error) {
	raw := make([]byte, byteLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hmacSHA256(data []byte, key []byte) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write(data)
	return h.Sum(nil)
}

// ClaimsAsJSON is used by /api/me to expose authenticated principal information.
func ClaimsAsJSON(claims map[string]interface{}) map[string]string {
	out := map[string]string{}
	for k, v := range claims {
		switch vv := v.(type) {
		case string:
			out[k] = vv
		case float64:
			out[k] = strconv.FormatFloat(vv, 'f', -1, 64)
		case bool:
			out[k] = strconv.FormatBool(vv)
		default:
			b, _ := json.Marshal(v)
			out[k] = string(b)
		}
	}
	return out
}
