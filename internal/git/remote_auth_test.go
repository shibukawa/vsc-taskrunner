package git

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"vsc-taskrunner/internal/uiconfig"
)

func TestRemoteAuthValidateGitHubSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/demo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"full_name":"acme/demo","permissions":{"pull":true,"triage":false,"push":false,"maintain":false,"admin":false}}`))
		case "/user/repos":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"full_name":"acme/demo"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	auth, err := newRemoteAuth("https://github.com/acme/demo.git", uiconfig.RepositoryAuthConfig{
		Type:             "envToken",
		Provider:         "github",
		TokenEnv:         "GITHUB_TOKEN",
		BaseURL:          server.URL,
		Repo:             "acme/demo",
		AllowedHosts:     []string{"github.com", strings.TrimPrefix(server.URL, "http://"), strings.TrimPrefix(server.URL, "https://")},
		RejectBroadScope: true,
		RequireReadOnly:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth.readEnv = func(key string) string { return "github_pat_test" }
	auth.httpClient = server.Client()

	if err := auth.validate(context.Background()); err != nil {
		t.Fatalf("validate() unexpected error: %v", err)
	}
}

func TestRemoteAuthValidateGitHubRejectsBroadScope(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/demo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"full_name":"acme/demo","permissions":{"pull":true,"triage":false,"push":false,"maintain":false,"admin":false}}`))
		case "/user/repos":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"full_name":"acme/demo"},{"full_name":"acme/other"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	auth, err := newRemoteAuth("https://github.com/acme/demo.git", uiconfig.RepositoryAuthConfig{
		Type:             "envToken",
		Provider:         "github",
		TokenEnv:         "GITHUB_TOKEN",
		BaseURL:          server.URL,
		Repo:             "acme/demo",
		AllowedHosts:     []string{"github.com", strings.TrimPrefix(server.URL, "http://"), strings.TrimPrefix(server.URL, "https://")},
		RejectBroadScope: true,
		RequireReadOnly:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth.readEnv = func(key string) string { return "github_pat_test" }
	auth.httpClient = server.Client()

	if err := auth.validate(context.Background()); err == nil {
		t.Fatal("expected broad github token scope to fail")
	}
}

func TestRemoteAuthValidateGitLabSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v4/personal_access_tokens/self":
			http.Error(w, "forbidden", http.StatusForbidden)
		case strings.Contains(r.RequestURI, "/api/v4/projects/acme%2Fdemo/access_tokens/self"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"scopes":["read_repository","read_api"]}`))
		case strings.Contains(r.RequestURI, "/api/v4/projects/acme%2Fdemo"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"path_with_namespace":"acme/demo"}`))
		case r.URL.Path == "/api/v4/projects":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"path_with_namespace":"acme/demo"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	auth, err := newRemoteAuth("https://gitlab.example.com/acme/demo.git", uiconfig.RepositoryAuthConfig{
		Type:             "envToken",
		Provider:         "gitlab",
		TokenEnv:         "GITLAB_TOKEN",
		BaseURL:          server.URL + "/api/v4",
		Repo:             "acme/demo",
		AllowedHosts:     []string{"gitlab.example.com", strings.TrimPrefix(server.URL, "http://"), strings.TrimPrefix(server.URL, "https://")},
		RejectBroadScope: true,
		RequireReadOnly:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth.readEnv = func(key string) string { return "glprojecttoken" }
	auth.httpClient = server.Client()

	if err := auth.validate(context.Background()); err != nil {
		t.Fatalf("validate() unexpected error: %v", err)
	}
}

func TestRemoteAuthValidateBitbucketRejectsWriteAccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/2.0/repositories/acme/demo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"full_name":"acme/demo","permission":"write"}`))
		case "/2.0/repositories/acme":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"values":[{"full_name":"acme/demo"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	auth, err := newRemoteAuth("https://bitbucket.org/acme/demo.git", uiconfig.RepositoryAuthConfig{
		Type:             "envToken",
		Provider:         "bitbucket",
		TokenEnv:         "BITBUCKET_TOKEN",
		BaseURL:          server.URL + "/2.0",
		Repo:             "acme/demo",
		AllowedHosts:     []string{"bitbucket.org", strings.TrimPrefix(server.URL, "http://"), strings.TrimPrefix(server.URL, "https://")},
		RejectBroadScope: true,
		RequireReadOnly:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth.readEnv = func(key string) string { return "bbtoken" }
	auth.httpClient = server.Client()

	if err := auth.validate(context.Background()); err == nil {
		t.Fatal("expected write-capable bitbucket token to fail")
	}
}

func TestBareRepositoryStoreMaintenanceSkipsValidationWhenTokenMissing(t *testing.T) {
	t.Parallel()

	store, err := NewBareRepositoryStore("https://github.com/acme/demo.git", filepath.Join(t.TempDir(), "cache.git"), 1, []string{".vscode"}, uiconfig.RepositoryAuthConfig{
		Type:             "envToken",
		Provider:         "github",
		TokenEnv:         "GITHUB_TOKEN",
		Repo:             "acme/demo",
		AllowedHosts:     []string{"github.com"},
		RejectBroadScope: true,
		RequireReadOnly:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.auth.readEnv = func(string) string { return "" }

	if err := store.Maintenance(context.Background()); err == nil {
		t.Fatal("expected git clone against unreachable public test repo to fail after auth skip")
	} else if strings.Contains(err.Error(), "repository token env") {
		t.Fatalf("expected missing token to skip auth validation, got %v", err)
	}
}
