package uiconfig

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func testTasks(specs ...AllowedTaskSpec) AllowedTaskSpecs {
	return AllowedTaskSpecs(specs)
}

func TestTasksArtifactRulesUnmarshal(t *testing.T) {
	var cfg UIConfig
	data := []byte(`
repository:
  source: /tmp/repo
tasks:
  build:
    artifacts:
      - path: dist
        nameTemplate: web.zip
        format: zip
`)
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() unexpected error: %v", err)
	}
	if len(cfg.Tasks) != 1 || len(cfg.Tasks[0].Config.Artifacts) != 1 {
		t.Fatalf("unexpected artifacts: %+v", cfg.Tasks)
	}
	rule := cfg.Tasks[0].Config.Artifacts[0]
	if rule.Path != "dist" || rule.NameTemplate != "web.zip" || rule.Format != "zip" {
		t.Fatalf("unexpected artifact rule: %+v", rule)
	}
}

func TestValidatePublicURL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Repository.Source = "/tmp/local-repo"
	cfg.Server.PublicURL = "http://localhost:8080"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}

	cfg.Server.PublicURL = "localhost:8080"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid publicURL to fail validation")
	}
}

func TestMatchBranch(t *testing.T) {
	cfg := &UIConfig{Branches: []string{"main", "release/*", "feature/*"}}
	tests := []struct {
		branch string
		want   bool
	}{
		{"main", true},
		{"release/1.0", true},
		{"feature/my-feature", true},
		{"develop", false},
	}
	for _, tt := range tests {
		if got := cfg.MatchBranch(tt.branch); got != tt.want {
			t.Fatalf("MatchBranch(%q) = %v, want %v", tt.branch, got, tt.want)
		}
	}
}

func TestMatchBranchDefaultsToTopLevelBranchesOnly(t *testing.T) {
	cfg := &UIConfig{}
	tests := []struct {
		branch string
		want   bool
	}{
		{"main", true},
		{"dev", true},
		{"feature/demo", false},
	}
	for _, tt := range tests {
		if got := cfg.MatchBranch(tt.branch); got != tt.want {
			t.Fatalf("MatchBranch(%q) = %v, want %v", tt.branch, got, tt.want)
		}
	}
}

func TestMatchTask(t *testing.T) {
	cfg := &UIConfig{Tasks: testTasks(
		AllowedTaskSpec{Pattern: "go-build", Config: TaskUIConfig{}},
		AllowedTaskSpec{Pattern: "lint-*", Config: TaskUIConfig{}},
	)}
	if !cfg.MatchTask("go-build") {
		t.Fatal("expected go-build to match")
	}
	if !cfg.MatchTask("lint-all") {
		t.Fatal("expected lint-all to match")
	}
	if cfg.MatchTask("go-test") {
		t.Fatal("expected go-test not to match")
	}
}

func TestTaskConfigUsesFirstMatchingPattern(t *testing.T) {
	cfg := &UIConfig{Tasks: testTasks(
		AllowedTaskSpec{Pattern: "npm-*", Config: TaskUIConfig{Artifacts: []ArtifactRuleConfig{{Path: "dist/*", Format: "file"}}}},
		AllowedTaskSpec{Pattern: "*", Config: TaskUIConfig{WorktreeDisabled: true}},
	)}
	taskCfg, ok := cfg.TaskConfig("npm-build")
	if !ok {
		t.Fatal("expected task config")
	}
	if got := taskCfg.Artifacts[0].Format; got != "file" {
		t.Fatalf("artifacts[0].format = %q, want file", got)
	}
	if taskCfg.WorktreeDisabled {
		t.Fatalf("expected first match to win, got %+v", *taskCfg)
	}
}

func TestTaskConfigDefaults(t *testing.T) {
	cfg := &UIConfig{Tasks: testTasks(
		AllowedTaskSpec{Pattern: "build", Config: TaskUIConfig{Artifacts: []ArtifactRuleConfig{{Path: "dist/*"}}}},
	)}
	taskCfg, ok := cfg.TaskConfig("build")
	if !ok {
		t.Fatal("expected task config")
	}
	if taskCfg.Artifacts[0].Format != "zip" {
		t.Fatalf("artifacts[0].format = %q, want zip", taskCfg.Artifacts[0].Format)
	}
	if taskCfg.Artifacts[0].NameTemplate != DefaultArtifactArchive {
		t.Fatalf("artifacts[0].nameTemplate = %q, want %q", taskCfg.Artifacts[0].NameTemplate, DefaultArtifactArchive)
	}
}

func TestMatchUser(t *testing.T) {
	cfg := &UIConfig{Auth: AuthConfig{AllowUsers: []UserAccessRule{{Claim: "email", Value: "*@example.com"}, {Claim: "groups", Value: "admin"}}}}
	if !cfg.MatchUser(map[string]interface{}{"email": "alice@example.com"}) {
		t.Fatal("expected email match")
	}
	if !cfg.MatchUser(map[string]interface{}{"groups": []interface{}{"viewer", "admin"}}) {
		t.Fatal("expected groups match")
	}
	if cfg.MatchUser(map[string]interface{}{"email": "alice@elsewhere.com"}) {
		t.Fatal("expected mismatch")
	}
}

func TestUserAccessRulesUnmarshalMapping(t *testing.T) {
	var cfg UIConfig
	data := []byte(`
repository:
  source: /tmp/repo
auth:
  allowUsers:
    email: "*@example.com"
    role:
      - administrator
      - ops-*
  adminUsers:
    role:
      - administrator
`)
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() unexpected error: %v", err)
	}
	if len(cfg.Auth.AllowUsers) != 3 {
		t.Fatalf("allowUsers len = %d, want 3", len(cfg.Auth.AllowUsers))
	}
	if got := cfg.Auth.AllowUsers[0]; got.Claim != "email" || got.Value != "*@example.com" {
		t.Fatalf("unexpected first allowUsers rule: %+v", got)
	}
	if !cfg.MatchUser(map[string]interface{}{"role": []interface{}{"ops-prod"}}) {
		t.Fatal("expected role list rule to match")
	}
	if !cfg.IsAdminUser(map[string]interface{}{"role": "administrator"}) {
		t.Fatal("expected adminUsers mapping rule to match")
	}
}

func TestUserAccessRulesRejectLegacySequence(t *testing.T) {
	var cfg UIConfig
	data := []byte(`
repository:
  source: /tmp/repo
auth:
  allowUsers:
    - claim: email
      value: "*@example.com"
`)
	if err := yaml.Unmarshal(data, &cfg); err == nil {
		t.Fatal("expected legacy sequence form to fail")
	}
}

func TestIsAdminUser(t *testing.T) {
	cfg := &UIConfig{Auth: AuthConfig{AdminUsers: []UserAccessRule{{Claim: "groups", Value: "admin"}}}}
	if !cfg.IsAdminUser(map[string]interface{}{"groups": []interface{}{"viewer", "admin"}}) {
		t.Fatal("expected admin match")
	}
	if cfg.IsAdminUser(map[string]interface{}{"groups": []interface{}{"viewer"}}) {
		t.Fatal("expected admin mismatch")
	}
}

func TestValidateUserAccessRules(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Repository.Source = "/tmp/local-repo"
	cfg.Auth.AllowUsers = UserAccessRules{{Claim: "role", Value: "["}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid auth.allowUsers glob to fail validation")
	}

	cfg.Auth.AllowUsers = UserAccessRules{{Claim: "role", Value: "admin"}}
	cfg.Auth.AdminUsers = UserAccessRules{{Claim: "", Value: "administrator"}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected empty auth.adminUsers claim to fail validation")
	}
}

func TestCanRunUsesAllowUsers(t *testing.T) {
	cfg := &UIConfig{Auth: AuthConfig{AllowUsers: []UserAccessRule{{Claim: "email", Value: "*@example.com"}}}}
	if !cfg.CanRun(map[string]interface{}{"email": "alice@example.com"}) {
		t.Fatal("expected canRun")
	}
	if cfg.CanRun(map[string]interface{}{"email": "alice@elsewhere.com"}) {
		t.Fatal("expected canRun=false")
	}
}

func TestValidateRepositoryConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Repository.Source = "https://example.com/demo.git"
	cfg.Repository.CachePath = "/tmp/demo-cache"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}

	cfg.Repository.CachePath = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing cachePath for remote source to fail validation")
	}
}

func TestValidateRepositoryAuthConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Repository.Source = "https://github.com/acme/demo.git"
	cfg.Repository.CachePath = "/tmp/demo-cache"
	cfg.Repository.Auth = RepositoryAuthConfig{
		Type:         "envToken",
		Provider:     "github",
		TokenEnv:     "GITHUB_TOKEN",
		Repo:         "acme/demo",
		AllowedHosts: []string{"github.com", "api.github.com"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
}

func TestValidateTaskPreRunTasks(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Repository.Source = "/tmp/local-repo"
	cfg.Tasks = testTasks(
		AllowedTaskSpec{Pattern: "build", Config: TaskUIConfig{PreRunTasks: []PreRunTaskConfig{{Command: "npm"}}}},
	)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}

	cfg.Tasks[0].Config.PreRunTasks[0].Command = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing pre-run command to fail validation")
	}
}

func TestValidateTaskArtifacts(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Repository.Source = "/tmp/local-repo"
	cfg.Tasks = testTasks(
		AllowedTaskSpec{Pattern: "build", Config: TaskUIConfig{Artifacts: []ArtifactRuleConfig{{Path: "dist/*", Format: "file"}}}},
	)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}

	cfg.Tasks[0].Config.Artifacts[0].Format = "tar"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid artifact format to fail validation")
	}

	cfg.Tasks[0].Config.Artifacts[0].Format = "zip"
	cfg.Tasks[0].Config.Artifacts[0].Path = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing artifact path to fail validation")
	}
}

func TestMatchingPreRunTasks(t *testing.T) {
	cfg := &UIConfig{Tasks: testTasks(
		AllowedTaskSpec{Pattern: "npm-*", Config: TaskUIConfig{PreRunTasks: []PreRunTaskConfig{{Command: "npm"}}}},
		AllowedTaskSpec{Pattern: "build", Config: TaskUIConfig{PreRunTasks: []PreRunTaskConfig{{Command: "make"}}}},
	)}
	matched := cfg.MatchingPreRunTasks("npm-build")
	if len(matched) != 1 || matched[0].Command != "npm" {
		t.Fatalf("unexpected hooks: %+v", matched)
	}
}

func TestUseSparseRunWorkspace(t *testing.T) {
	cfg := &UIConfig{Tasks: testTasks(
		AllowedTaskSpec{Pattern: "build", Config: TaskUIConfig{WorktreeDisabled: true}},
		AllowedTaskSpec{Pattern: "lint", Config: TaskUIConfig{}},
	)}
	if !cfg.UseSparseRunWorkspace("build") {
		t.Fatal("expected build to use sparse run workspace")
	}
	if cfg.UseSparseRunWorkspace("lint") {
		t.Fatal("expected lint not to use sparse run workspace")
	}
}

func TestWorktreeDefaults(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Repository.Source = "/tmp/local-repo"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.Worktree.KeepOnSuccess != DefaultWorktreeKeepOK {
		t.Fatalf("keepOnSuccess = %d, want %d", cfg.Storage.Worktree.KeepOnSuccess, DefaultWorktreeKeepOK)
	}
	if cfg.Storage.Worktree.KeepOnFailure != DefaultWorktreeKeepError {
		t.Fatalf("keepOnFailure = %d, want %d", cfg.Storage.Worktree.KeepOnFailure, DefaultWorktreeKeepError)
	}
}

func TestTasksUnmarshalPreservesOrder(t *testing.T) {
	var cfg struct {
		Tasks AllowedTaskSpecs `yaml:"tasks"`
	}
	data := `
tasks:
  "npm-*":
    artifacts:
      - path: dist/*
  "*":
    worktreeDisabled: true
`
	if err := yaml.Unmarshal([]byte(data), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(cfg.Tasks))
	}
	if got := cfg.Tasks[0].Pattern; got != "npm-*" {
		t.Fatalf("first pattern = %q, want npm-*", got)
	}
}
