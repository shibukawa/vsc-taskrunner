package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vsc-taskrunner/internal/uiconfig"
)

func TestAPITokenServiceCreateAuthenticateRevoke(t *testing.T) {
	t.Parallel()

	store := NewLocalAPITokenStore(t.TempDir() + "/tokens.json")
	service := NewAPITokenService(uiconfig.APITokenConfig{
		Enabled:         true,
		DefaultTTLHours: 24,
		MaxPerUser:      3,
		Store: uiconfig.APITokenStoreConfig{
			Backend:   "local",
			LocalPath: "tokens.json",
		},
	}, store)
	claims := map[string]interface{}{
		"email": "alice@example.com",
		"role":  []interface{}{"runner", "administrator"},
	}

	tokenValue, created, err := service.Create(t.Context(), "alice@example.com", claims, "ci", []string{APITokenScopeRunsRead, APITokenScopeRunsWrite}, 24)
	if err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}
	if tokenValue == "" {
		t.Fatal("expected token value")
	}
	if created.ID == "" {
		t.Fatal("expected token id")
	}

	record, err := service.Authenticate(t.Context(), tokenValue)
	if err != nil {
		t.Fatalf("Authenticate() unexpected error: %v", err)
	}
	if record.Subject != "alice@example.com" {
		t.Fatalf("subject = %q, want alice@example.com", record.Subject)
	}
	if !service.HasScopes(record, APITokenScopeRunsRead, APITokenScopeRunsWrite) {
		t.Fatal("expected token scopes")
	}

	if err := service.Revoke(t.Context(), "alice@example.com", created.ID); err != nil {
		t.Fatalf("Revoke() unexpected error: %v", err)
	}
	if _, err := service.Authenticate(t.Context(), tokenValue); err != ErrAPITokenRevoked {
		t.Fatalf("Authenticate() after revoke err = %v, want %v", err, ErrAPITokenRevoked)
	}
}

func TestBearerTokenCanReadRunsButRequiresScope(t *testing.T) {
	t.Parallel()

	cfg := uiconfig.DefaultConfig()
	cfg.Repository.Source = "/tmp/repo"
	cfg.Auth.AllowUsers = uiconfig.UserAccessRules{{Claim: "role", Value: "runner"}}
	cfg.Auth.AdminUsers = uiconfig.UserAccessRules{{Claim: "role", Value: "administrator"}}
	cfg.Auth.APITokens.Enabled = true

	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	auth := newTestAuthenticator(t, cfg)
	auth.SetTokenService(newTestTokenService(t, cfg.Auth.APITokens))
	server := NewServer(nil, cfg, NewTaskManager(nil, cfg, history), auth)

	readToken := createTestToken(t, auth.TokenService(), map[string]interface{}{
		"email": "alice@example.com",
		"role":  []interface{}{"runner", "administrator"},
	}, []string{APITokenScopeRunsRead}, "read")

	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	req.Header.Set("Authorization", "Bearer "+readToken)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/runs status = %d body=%s", rec.Code, rec.Body.String())
	}

	writeOnlyToken := createTestToken(t, auth.TokenService(), map[string]interface{}{
		"email": "bob@example.com",
		"role":  []interface{}{"runner", "administrator"},
	}, []string{APITokenScopeRunsWrite}, "write")

	req = httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	req.Header.Set("Authorization", "Bearer "+writeOnlyToken)
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /api/runs with write-only token status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPITokenManagementRequiresAdminSession(t *testing.T) {
	t.Parallel()

	cfg := uiconfig.DefaultConfig()
	cfg.Repository.Source = "/tmp/repo"
	cfg.Auth.AllowUsers = uiconfig.UserAccessRules{{Claim: "role", Value: "runner"}}
	cfg.Auth.AdminUsers = uiconfig.UserAccessRules{{Claim: "role", Value: "administrator"}}
	cfg.Auth.APITokens.Enabled = true

	auth := newTestAuthenticator(t, cfg)
	auth.SetTokenService(newTestTokenService(t, cfg.Auth.APITokens))
	server := NewServer(nil, cfg, nil, auth)

	nonAdminReq := httptest.NewRequest(http.MethodGet, "/api/tokens", nil)
	attachSessionCookie(t, auth, nonAdminReq, map[string]interface{}{
		"email": "runner@example.com",
		"role":  []interface{}{"runner"},
	})
	nonAdminRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(nonAdminRec, nonAdminReq)
	if nonAdminRec.Code != http.StatusForbidden {
		t.Fatalf("non-admin GET /api/tokens status = %d body=%s", nonAdminRec.Code, nonAdminRec.Body.String())
	}

	adminReq := httptest.NewRequest(http.MethodPost, "/api/tokens", strings.NewReader(`{"label":"ci","scopes":["runs:read","runs:write"],"ttlHours":24}`))
	attachSessionCookie(t, auth, adminReq, map[string]interface{}{
		"email": "admin@example.com",
		"role":  []interface{}{"runner", "administrator"},
	})
	adminRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusCreated {
		t.Fatalf("admin POST /api/tokens status = %d body=%s", adminRec.Code, adminRec.Body.String())
	}
	if !strings.Contains(adminRec.Body.String(), `"token":"rtu_`) {
		t.Fatalf("expected token in response, got %s", adminRec.Body.String())
	}
}

func newTestAuthenticator(t *testing.T, cfg *uiconfig.UIConfig) *Authenticator {
	t.Helper()
	return &Authenticator{
		enabled:       true,
		config:        cfg,
		sessionCookie: "runtask_session",
		signerKey:     []byte("01234567890123456789012345678901"),
	}
}

func newTestTokenService(t *testing.T, cfg uiconfig.APITokenConfig) *APITokenService {
	t.Helper()
	store := NewLocalAPITokenStore(t.TempDir() + "/tokens.json")
	return NewAPITokenService(cfg, store)
}

func createTestToken(t *testing.T, service *APITokenService, claims map[string]interface{}, scopes []string, label string) string {
	t.Helper()
	tokenValue, _, err := service.Create(t.Context(), SubjectFromClaims(claims), claims, label, scopes, 24)
	if err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}
	return tokenValue
}

func attachSessionCookie(t *testing.T, auth *Authenticator, req *http.Request, claims map[string]interface{}) {
	t.Helper()
	value, err := auth.signSession(SessionClaims{
		Claims: claims,
		Expiry: time.Now().Add(24 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("signSession() unexpected error: %v", err)
	}
	req.AddCookie(&http.Cookie{
		Name:  auth.sessionCookie,
		Value: value,
		Path:  "/",
	})
}
