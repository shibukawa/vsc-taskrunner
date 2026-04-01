package git

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"

	"vsc-taskrunner/internal/uiconfig"
)

type remoteAuth struct {
	sourceURL  *url.URL
	source     string
	config     uiconfig.RepositoryAuthConfig
	httpClient *http.Client
	readEnv    func(string) string
}

func newRemoteAuth(source string, cfg uiconfig.RepositoryAuthConfig) (*remoteAuth, error) {
	if cfg.Type == "" || cfg.Type == "none" {
		return nil, nil
	}
	sourceURL, err := url.Parse(strings.TrimSpace(source))
	if err != nil {
		return nil, fmt.Errorf("parse repository source: %w", err)
	}
	return &remoteAuth{
		sourceURL:  sourceURL,
		source:     strings.TrimSpace(source),
		config:     cfg,
		httpClient: http.DefaultClient,
		readEnv:    os.Getenv,
	}, nil
}

func (a *remoteAuth) validate(ctx context.Context) error {
	if a == nil {
		return nil
	}
	token := strings.TrimSpace(a.readEnv(a.config.TokenEnv))
	if token == "" {
		return nil
	}
	switch a.config.Provider {
	case "github":
		return a.validateGitHub(ctx, token)
	case "gitlab":
		return a.validateGitLab(ctx, token)
	case "bitbucket":
		return a.validateBitbucket(ctx, token)
	default:
		return fmt.Errorf("unsupported repository auth provider %q", a.config.Provider)
	}
}

func (a *remoteAuth) applyToCommand(cmd *exec.Cmd) error {
	if a == nil {
		return nil
	}
	token := strings.TrimSpace(a.readEnv(a.config.TokenEnv))
	if token == "" {
		return nil
	}
	headerValue, err := a.gitAuthorizationHeader(token)
	if err != nil {
		return err
	}
	baseEnv := cmd.Env
	if len(baseEnv) == 0 {
		baseEnv = os.Environ()
	}
	cmd.Env = append(baseEnv,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraHeader",
		"GIT_CONFIG_VALUE_0="+headerValue,
	)
	return nil
}

func (a *remoteAuth) gitAuthorizationHeader(token string) (string, error) {
	switch a.config.Provider {
	case "github":
		return basicAuthorizationHeader("x-access-token", token), nil
	case "gitlab":
		return basicAuthorizationHeader("oauth2", token), nil
	case "bitbucket":
		return basicAuthorizationHeader("x-token-auth", token), nil
	default:
		return "", fmt.Errorf("unsupported repository auth provider %q", a.config.Provider)
	}
}

func basicAuthorizationHeader(username, token string) string {
	credential := base64.StdEncoding.EncodeToString([]byte(username + ":" + token))
	return "Authorization: Basic " + credential
}

func (a *remoteAuth) validateGitHub(ctx context.Context, token string) error {
	if !strings.HasPrefix(token, "github_pat_") {
		return fmt.Errorf("github token must be a fine-grained personal access token")
	}
	apiBaseURL := strings.TrimRight(a.config.BaseURL, "/")
	if apiBaseURL == "" {
		apiBaseURL = "https://api.github.com"
	}

	var repo struct {
		FullName    string `json:"full_name"`
		Permissions struct {
			Pull     bool `json:"pull"`
			Triage   bool `json:"triage"`
			Push     bool `json:"push"`
			Maintain bool `json:"maintain"`
			Admin    bool `json:"admin"`
		} `json:"permissions"`
	}
	if err := a.getJSON(ctx, apiBaseURL+"/repos/"+a.config.Repo, token, "Bearer", map[string]string{
		"Accept": "application/vnd.github+json",
	}, &repo); err != nil {
		return fmt.Errorf("validate github repository access: %w", err)
	}
	if normalizeRemoteRepo(repo.FullName) != a.config.Repo {
		return fmt.Errorf("github token does not grant access to repository %q", a.config.Repo)
	}
	if !repo.Permissions.Pull {
		return fmt.Errorf("github token does not grant repository read access")
	}
	if a.config.RequireReadOnly && (repo.Permissions.Triage || repo.Permissions.Push || repo.Permissions.Maintain || repo.Permissions.Admin) {
		return fmt.Errorf("github token must be read-only for repository %q", a.config.Repo)
	}
	if a.config.RejectBroadScope {
		items, hasNext, err := a.listGitHubRepositories(ctx, apiBaseURL, token)
		if err != nil {
			return fmt.Errorf("validate github repository scope: %w", err)
		}
		if hasNext || len(items) != 1 || normalizeRemoteRepo(items[0].FullName) != a.config.Repo {
			return fmt.Errorf("github token scope must be limited to repository %q", a.config.Repo)
		}
	}
	return nil
}

func (a *remoteAuth) listGitHubRepositories(ctx context.Context, apiBaseURL, token string) ([]struct {
	FullName string `json:"full_name"`
}, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBaseURL+"/user/repos?per_page=2&sort=full_name&direction=asc", nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, false, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var items []struct {
		FullName string `json:"full_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, false, err
	}
	return items, strings.Contains(resp.Header.Get("Link"), `rel="next"`), nil
}

func (a *remoteAuth) validateGitLab(ctx context.Context, token string) error {
	apiBaseURL := strings.TrimRight(a.config.BaseURL, "/")
	if apiBaseURL == "" {
		apiBaseURL = strings.TrimRight((&url.URL{
			Scheme: a.sourceURL.Scheme,
			Host:   a.sourceURL.Host,
		}).String(), "/") + "/api/v4"
	}
	projectPath := url.PathEscape(a.config.Repo)

	var self struct {
		Scopes []string `json:"scopes"`
	}
	if err := a.getJSON(ctx, apiBaseURL+"/personal_access_tokens/self", token, "", map[string]string{
		"PRIVATE-TOKEN": token,
	}, &self); err == nil {
		return fmt.Errorf("gitlab personal access tokens are not allowed")
	}

	var project struct {
		PathWithNamespace string `json:"path_with_namespace"`
		Permissions       struct {
			ProjectAccess any `json:"project_access"`
			GroupAccess   any `json:"group_access"`
		} `json:"permissions"`
	}
	if err := a.getJSON(ctx, apiBaseURL+"/projects/"+projectPath, token, "", map[string]string{
		"PRIVATE-TOKEN": token,
	}, &project); err != nil {
		return fmt.Errorf("validate gitlab project access: %w", err)
	}
	if normalizeRemoteRepo(project.PathWithNamespace) != a.config.Repo {
		return fmt.Errorf("gitlab token does not grant access to project %q", a.config.Repo)
	}

	var repos []struct {
		PathWithNamespace string `json:"path_with_namespace"`
	}
	if err := a.getJSON(ctx, apiBaseURL+"/projects?membership=true&simple=true&per_page=2", token, "", map[string]string{
		"PRIVATE-TOKEN": token,
	}, &repos); err != nil {
		return fmt.Errorf("validate gitlab project scope: %w", err)
	}
	if len(repos) != 1 || normalizeRemoteRepo(repos[0].PathWithNamespace) != a.config.Repo {
		return fmt.Errorf("gitlab token scope must be limited to project %q", a.config.Repo)
	}

	var tokenInfo struct {
		Scopes []string `json:"scopes"`
	}
	if err := a.getJSON(ctx, apiBaseURL+"/projects/"+projectPath+"/access_tokens/self", token, "", map[string]string{
		"PRIVATE-TOKEN": token,
	}, &tokenInfo); err != nil {
		return fmt.Errorf("validate gitlab token type: %w", err)
	}
	if !stringSliceContains(tokenInfo.Scopes, "read_repository") {
		return fmt.Errorf("gitlab token must include read_repository")
	}
	if !stringSliceContains(tokenInfo.Scopes, "read_api") {
		return fmt.Errorf("gitlab token must include read_api for strict validation")
	}
	if a.config.RequireReadOnly {
		for _, denied := range []string{"api", "write_repository", "create_runner", "manage_runner"} {
			if stringSliceContains(tokenInfo.Scopes, denied) {
				return fmt.Errorf("gitlab token must not include %s", denied)
			}
		}
	}
	return nil
}

func (a *remoteAuth) validateBitbucket(ctx context.Context, token string) error {
	apiBaseURL := strings.TrimRight(a.config.BaseURL, "/")
	if apiBaseURL == "" {
		apiBaseURL = "https://api.bitbucket.org/2.0"
	}
	parts := strings.Split(a.config.Repo, "/")
	if len(parts) != 2 {
		return fmt.Errorf("bitbucket repository id must use workspace/repo form")
	}
	repoPath := path.Join("repositories", parts[0], parts[1])

	var repo struct {
		FullName   string `json:"full_name"`
		Permission string `json:"permission"`
	}
	if err := a.getJSON(ctx, apiBaseURL+"/"+repoPath+"?role=member", token, "Bearer", nil, &repo); err != nil {
		return fmt.Errorf("validate bitbucket repository access: %w", err)
	}
	if normalizeRemoteRepo(repo.FullName) != a.config.Repo {
		return fmt.Errorf("bitbucket token does not grant access to repository %q", a.config.Repo)
	}
	if a.config.RequireReadOnly && repo.Permission != "" && repo.Permission != "read" {
		return fmt.Errorf("bitbucket token must be read-only for repository %q", a.config.Repo)
	}

	var repos struct {
		Values []struct {
			FullName string `json:"full_name"`
		} `json:"values"`
		Next string `json:"next"`
	}
	if err := a.getJSON(ctx, apiBaseURL+"/repositories/"+parts[0]+"?pagelen=2&role=member", token, "Bearer", nil, &repos); err != nil {
		return fmt.Errorf("validate bitbucket repository scope: %w", err)
	}
	if repos.Next != "" || len(repos.Values) != 1 || normalizeRemoteRepo(repos.Values[0].FullName) != a.config.Repo {
		return fmt.Errorf("bitbucket token scope must be limited to repository %q", a.config.Repo)
	}
	return nil
}

func (a *remoteAuth) getJSON(ctx context.Context, rawURL, token, authScheme string, headers map[string]string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	if authScheme != "" {
		req.Header.Set("Authorization", authScheme+" "+token)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return err
	}
	return nil
}

func stringSliceContains(values []string, target string) bool {
	return slices.Contains(values, target)
}

func normalizeRemoteRepo(repo string) string {
	return strings.Trim(strings.TrimSpace(strings.TrimSuffix(repo, ".git")), "/")
}
