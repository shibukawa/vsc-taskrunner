package web

import (
	"net/http/httptest"
	"testing"

	"vsc-taskrunner/internal/uiconfig"
)

func TestRedirectURLForRequestPrefersForwardedHost(t *testing.T) {
	t.Parallel()

	auth := &Authenticator{
		config: &uiconfig.UIConfig{
			Server: uiconfig.ServerConfig{
				Host:      "localhost",
				Port:      8080,
				PublicURL: "http://localhost:8080",
			},
		},
	}

	req := httptest.NewRequest("GET", "http://localhost:8080/auth/login", nil)
	req.Header.Set("X-Forwarded-Proto", "http")
	req.Header.Set("X-Forwarded-Host", "localhost:5173")

	got := auth.redirectURLForRequest(req)
	want := "http://localhost:5173/auth/callback"
	if got != want {
		t.Fatalf("redirectURLForRequest() = %q, want %q", got, want)
	}
}

func TestRedirectURLForRequestFallsBackToPublicURL(t *testing.T) {
	t.Parallel()

	auth := &Authenticator{
		config: &uiconfig.UIConfig{
			Server: uiconfig.ServerConfig{
				Host:      "localhost",
				Port:      8080,
				PublicURL: "http://localhost:8080",
			},
		},
	}

	req := httptest.NewRequest("GET", "http://localhost:8080/auth/login", nil)
	req.Host = ""

	got := auth.redirectURLForRequest(req)
	want := "http://localhost:8080/auth/callback"
	if got != want {
		t.Fatalf("redirectURLForRequest() = %q, want %q", got, want)
	}
}
