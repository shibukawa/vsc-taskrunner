// Package uiconfig handles loading and validating runtask-ui.yaml configuration files.
package uiconfig

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultFetchDepth         = 1
	DefaultTasksSparsePath    = ".vscode"
	DefaultArtifactArchive    = "artifacts.zip"
	DefaultHistoryDir         = ".runtask/history"
	DefaultHistoryKeepCount   = 100
	DefaultWorktreeKeepOK     = 0
	DefaultWorktreeKeepError  = 3
	DefaultAPITokenTTLHours   = 24 * 30
	DefaultAPITokenMaxPerUser = 10
	DefaultAPITokenLocalPath  = ".runtask/api-tokens.json"
)

type UIConfig struct {
	Server     ServerConfig     `yaml:"server"`
	Repository RepositoryConfig `yaml:"repository"`
	Auth       AuthConfig       `yaml:"auth"`
	Branches   []string         `yaml:"branches"`
	Tasks      AllowedTaskSpecs `yaml:"tasks"`
	Execution  ExecutionConfig  `yaml:"execution"`
	Metrics    MetricsConfig    `yaml:"metrics"`
	Logging    LoggingConfig    `yaml:"logging"`
	Storage    StorageConfig    `yaml:"storage"`
}

type LoggingConfig struct {
	Redaction RedactionConfig `yaml:"redaction"`
}

type RedactionConfig struct {
	Names  []string `yaml:"names"`
	Tokens []string `yaml:"tokens"`
}

type ServerConfig struct {
	Host      string `yaml:"host"`
	Port      int    `yaml:"port"`
	PublicURL string `yaml:"publicURL"`
}

type AuthConfig struct {
	OIDCIssuer       string          `yaml:"oidcIssuer"`
	OIDCClientID     string          `yaml:"oidcClientID"`
	OIDCClientSecret string          `yaml:"oidcClientSecret"`
	NoAuth           bool            `yaml:"noAuth"`
	SessionSecret    string          `yaml:"sessionSecret"`
	AllowUsers       UserAccessRules `yaml:"allowUsers"`
	AdminUsers       UserAccessRules `yaml:"adminUsers"`
	APITokens        APITokenConfig  `yaml:"apiTokens"`
}

type APITokenConfig struct {
	Enabled         bool                `yaml:"enabled"`
	DefaultTTLHours int                 `yaml:"defaultTTLHours"`
	MaxPerUser      int                 `yaml:"maxPerUser"`
	Store           APITokenStoreConfig `yaml:"store"`
}

type APITokenStoreConfig struct {
	Backend   string              `yaml:"backend"`
	LocalPath string              `yaml:"localPath"`
	Object    ObjectStorageConfig `yaml:"object"`
}

type RepositoryConfig struct {
	Source    string               `yaml:"source"`
	LocalPath string               `yaml:"localPath"`
	CachePath string               `yaml:"cachePath"`
	Auth      RepositoryAuthConfig `yaml:"auth"`
}

type RepositoryAuthConfig struct {
	Type             string   `yaml:"type"`
	Provider         string   `yaml:"provider"`
	TokenEnv         string   `yaml:"tokenEnv"`
	BaseURL          string   `yaml:"baseURL"`
	Repo             string   `yaml:"repo"`
	AllowedHosts     []string `yaml:"allowedHosts"`
	RejectBroadScope bool     `yaml:"rejectBroadScope"`
	RequireReadOnly  bool     `yaml:"requireReadOnly"`
}

type AllowedTaskSpecs []AllowedTaskSpec

func (s AllowedTaskSpecs) MarshalYAML() (any, error) {
	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, spec := range s {
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: spec.Pattern})
		valueNode := &yaml.Node{}
		if err := valueNode.Encode(spec.Config); err != nil {
			return nil, err
		}
		node.Content = append(node.Content, valueNode)
	}
	return node, nil
}

type AllowedTaskSpec struct {
	Pattern string
	Config  TaskUIConfig
}

type TaskUIConfig struct {
	Artifacts        []ArtifactRuleConfig `yaml:"artifacts,omitempty"`
	PreRunTasks      []PreRunTaskConfig   `yaml:"preRunTask,omitempty"`
	// Per-task overrides. If nil/empty the global storage settings are used.
	HistoryKeepCount *int                `yaml:"historyKeepCount,omitempty"`
	Worktree         *TaskWorktreeConfig `yaml:"worktree,omitempty"`
}

type ArtifactRuleConfig struct {
	Path         string `yaml:"path"`
	NameTemplate string `yaml:"nameTemplate,omitempty"`
	Format       string `yaml:"format,omitempty"`
}

type UserAccessRule struct {
	Claim string `yaml:"claim"`
	Value string `yaml:"value"`
}

type UserAccessRules []UserAccessRule

func (r UserAccessRules) MarshalYAML() (any, error) {
	if len(r) == 0 {
		return nil, nil
	}
	grouped := make(map[string][]string)
	order := make([]string, 0, len(r))
	for _, rule := range r {
		if _, ok := grouped[rule.Claim]; !ok {
			order = append(order, rule.Claim)
		}
		grouped[rule.Claim] = append(grouped[rule.Claim], rule.Value)
	}
	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, claim := range order {
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: claim})
		values := grouped[claim]
		if len(values) == 1 {
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: values[0]})
			continue
		}
		valueNode := &yaml.Node{Kind: yaml.SequenceNode}
		for _, value := range values {
			valueNode.Content = append(valueNode.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: value})
		}
		node.Content = append(node.Content, valueNode)
	}
	return node, nil
}

func (r *UserAccessRules) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case 0:
		*r = nil
		return nil
	case yaml.MappingNode:
		rules := make([]UserAccessRule, 0, len(value.Content))
		for index := 0; index < len(value.Content); index += 2 {
			keyNode := value.Content[index]
			valueNode := value.Content[index+1]
			claim := strings.TrimSpace(keyNode.Value)
			if claim == "" {
				return fmt.Errorf("user access rule claim must not be empty")
			}
			patterns, err := decodeUserAccessPatterns(valueNode)
			if err != nil {
				return fmt.Errorf("decode user access rule %q: %w", claim, err)
			}
			for _, pattern := range patterns {
				rules = append(rules, UserAccessRule{
					Claim: claim,
					Value: pattern,
				})
			}
		}
		*r = UserAccessRules(rules)
		return nil
	default:
		return fmt.Errorf("user access rules must be a mapping")
	}
}

func decodeUserAccessPatterns(node *yaml.Node) ([]string, error) {
	switch node.Kind {
	case yaml.ScalarNode:
		pattern := strings.TrimSpace(node.Value)
		if pattern == "" {
			return nil, fmt.Errorf("pattern must not be empty")
		}
		return []string{pattern}, nil
	case yaml.SequenceNode:
		patterns := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("patterns must be strings")
			}
			pattern := strings.TrimSpace(item.Value)
			if pattern == "" {
				return nil, fmt.Errorf("pattern must not be empty")
			}
			patterns = append(patterns, pattern)
		}
		return patterns, nil
	default:
		return nil, fmt.Errorf("rule value must be a string or string list")
	}
}

type ExecutionConfig struct {
	MaxParallelRuns int `yaml:"maxParallelRuns"`
}

type MetricsConfig struct {
	Enabled             bool `yaml:"enabled"`
	CPUInterval         int  `yaml:"cpuInterval"`
	MemoryInterval      int  `yaml:"memoryInterval"`
	StorageInterval     int  `yaml:"storageInterval"`
	MemoryHistoryWindow int  `yaml:"memoryHistoryWindow"`
}

type PreRunTaskConfig struct {
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args,omitempty"`
	CWD     string            `yaml:"cwd,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
	Shell   *ShellConfig      `yaml:"shell,omitempty"`
}

type ShellConfig struct {
	Executable string   `yaml:"executable"`
	Args       []string `yaml:"args,omitempty"`
}

type StorageConfig struct {
	HistoryDir       string              `yaml:"historyDir"`
	HistoryKeepCount int                 `yaml:"historyKeepCount"`
	Backend          string              `yaml:"backend"`
	Object           ObjectStorageConfig `yaml:"object"`
	Worktree         WorktreeConfig      `yaml:"worktree"`
}

type TaskWorktreeConfig struct {
	Disabled      *bool `yaml:"disabled,omitempty"`
	KeepOnSuccess *int  `yaml:"keepOnSuccess,omitempty"`
	KeepOnFailure *int  `yaml:"keepOnFailure,omitempty"`
}

type ObjectStorageConfig struct {
	Endpoint       string `yaml:"endpoint"`
	Bucket         string `yaml:"bucket"`
	Region         string `yaml:"region"`
	AccessKey      string `yaml:"accessKey"`
	SecretKey      string `yaml:"secretKey"`
	Prefix         string `yaml:"prefix"`
	ForcePathStyle bool   `yaml:"forcePathStyle"`
}

type WorktreeConfig struct {
	KeepOnSuccess int `yaml:"keepOnSuccess"`
	KeepOnFailure int `yaml:"keepOnFailure"`
}

func (s *AllowedTaskSpecs) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("tasks must be a mapping")
	}
	specs := make([]AllowedTaskSpec, 0, len(value.Content)/2)
	for index := 0; index < len(value.Content); index += 2 {
		keyNode := value.Content[index]
		valueNode := value.Content[index+1]
		pattern := strings.TrimSpace(keyNode.Value)
		var cfg TaskUIConfig
		if err := valueNode.Decode(&cfg); err != nil {
			return err
		}
		specs = append(specs, AllowedTaskSpec{
			Pattern: pattern,
			Config:  cfg,
		})
	}
	*s = specs
	return nil
}

func DefaultConfig() *UIConfig {
	return &UIConfig{
		Server:    ServerConfig{Host: "localhost", Port: 8080},
		Execution: ExecutionConfig{MaxParallelRuns: 4},
		Metrics: MetricsConfig{
			Enabled:             true,
			CPUInterval:         5,
			MemoryInterval:      15,
			StorageInterval:     60,
			MemoryHistoryWindow: 300,
		},
		Storage: StorageConfig{
			HistoryDir:       DefaultHistoryDir,
			HistoryKeepCount: DefaultHistoryKeepCount,
			Backend:          "local",
			Worktree: WorktreeConfig{
				KeepOnSuccess: DefaultWorktreeKeepOK,
				KeepOnFailure: DefaultWorktreeKeepError,
			},
		},
		Repository: RepositoryConfig{
			CachePath: ".runtask/repo-cache",
			Auth: RepositoryAuthConfig{
				RejectBroadScope: true,
				RequireReadOnly:  true,
			},
		},
		Auth: AuthConfig{
			APITokens: APITokenConfig{
				DefaultTTLHours: DefaultAPITokenTTLHours,
				MaxPerUser:      DefaultAPITokenMaxPerUser,
				Store: APITokenStoreConfig{
					Backend:   "local",
					LocalPath: DefaultAPITokenLocalPath,
				},
			},
		},
	}
}

func LoadConfig(filePath string) (*UIConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", filePath, err)
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", filePath, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", filePath, err)
	}
	return cfg, nil
}

func (c *UIConfig) Validate() error {
	if strings.TrimSpace(c.Repository.Source) == "" {
		return fmt.Errorf("repository.source is required")
	}
	remoteSource := c.Repository.IsRemoteSource()
	if strings.TrimSpace(c.Repository.CachePath) == "" {
		c.Repository.CachePath = c.Repository.LocalPath
	}
	if remoteSource && strings.TrimSpace(c.Repository.CachePath) == "" {
		return fmt.Errorf("repository.cachePath is required")
	}
	if err := c.Repository.Auth.Validate(c.Repository.Source, remoteSource); err != nil {
		return err
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535, got %d", c.Server.Port)
	}
	if c.Server.PublicURL != "" {
		parsed, err := url.Parse(c.Server.PublicURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("server.publicURL must be an absolute http(s) URL")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("server.publicURL must use http or https")
		}
	}
	if err := validateUserAccessRules(c.Auth.AllowUsers, "auth.allowUsers"); err != nil {
		return err
	}
	if err := validateUserAccessRules(c.Auth.AdminUsers, "auth.adminUsers"); err != nil {
		return err
	}
	if err := c.Auth.APITokens.Validate(c.Auth.NoAuth); err != nil {
		return err
	}
	if c.Execution.MaxParallelRuns < 0 {
		return fmt.Errorf("execution.maxParallelRuns must be >= 0")
	}
	if c.Metrics.CPUInterval <= 0 {
		return fmt.Errorf("metrics.cpuInterval must be > 0")
	}
	if c.Metrics.MemoryInterval <= 0 {
		return fmt.Errorf("metrics.memoryInterval must be > 0")
	}
	if c.Metrics.StorageInterval <= 0 {
		return fmt.Errorf("metrics.storageInterval must be > 0")
	}
	if c.Metrics.MemoryHistoryWindow <= 0 {
		return fmt.Errorf("metrics.memoryHistoryWindow must be > 0")
	}
	switch c.Storage.Backend {
	case "", "local":
		if c.Storage.Backend == "" {
			c.Storage.Backend = "local"
		}
	case "object":
		if strings.TrimSpace(c.Storage.Object.Region) == "" {
			c.Storage.Object.Region = "us-east-1"
		}
		if err := validateObjectStorageConfig(c.Storage.Object, "storage.object"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("storage.backend must be one of local or object, got %q", c.Storage.Backend)
	}
	if c.Storage.HistoryDir == "" {
		c.Storage.HistoryDir = DefaultHistoryDir
	}
	if c.Storage.HistoryKeepCount <= 0 {
		c.Storage.HistoryKeepCount = DefaultHistoryKeepCount
	}
	if c.Storage.Worktree.KeepOnSuccess < 0 {
		return fmt.Errorf("storage.worktree.keepOnSuccess must be >= 0")
	}
	if c.Storage.Worktree.KeepOnFailure < 0 {
		return fmt.Errorf("storage.worktree.keepOnFailure must be >= 0")
	}
	for index, spec := range c.Tasks {
		if strings.TrimSpace(spec.Pattern) == "" {
			return fmt.Errorf("tasks[%d] key must not be empty", index)
		}
		if _, err := path.Match(spec.Pattern, "sample"); err != nil {
			return fmt.Errorf("tasks[%d] pattern %q is invalid: %w", index, spec.Pattern, err)
		}
		if err := validateTaskUIConfig(spec.Config, fmt.Sprintf("tasks[%q]", spec.Pattern)); err != nil {
			return err
		}
	}
	return nil
}

func (c *APITokenConfig) Validate(noAuth bool) error {
	if c.DefaultTTLHours <= 0 {
		c.DefaultTTLHours = DefaultAPITokenTTLHours
	}
	if c.MaxPerUser <= 0 {
		c.MaxPerUser = DefaultAPITokenMaxPerUser
	}
	if strings.TrimSpace(c.Store.Backend) == "" {
		c.Store.Backend = "local"
	}
	if strings.TrimSpace(c.Store.LocalPath) == "" {
		c.Store.LocalPath = DefaultAPITokenLocalPath
	}
	if !c.Enabled {
		return nil
	}
	if noAuth {
		return fmt.Errorf("auth.apiTokens.enabled requires auth.noAuth=false")
	}
	switch c.Store.Backend {
	case "", "local":
		if strings.TrimSpace(c.Store.LocalPath) == "" {
			return fmt.Errorf("auth.apiTokens.store.localPath is required when auth.apiTokens.store.backend=local")
		}
	case "object":
		if strings.TrimSpace(c.Store.Object.Region) == "" {
			c.Store.Object.Region = "us-east-1"
		}
		if err := validateObjectStorageConfig(c.Store.Object, "auth.apiTokens.store.object"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("auth.apiTokens.store.backend must be one of local or object, got %q", c.Store.Backend)
	}
	return nil
}

func validateTaskUIConfig(cfg TaskUIConfig, prefix string) error {
	for index, artifact := range cfg.Artifacts {
		if err := validateArtifactRuleConfig(artifact, fmt.Sprintf("%s.artifacts[%d]", prefix, index)); err != nil {
			return err
		}
	}
	for index, hook := range cfg.PreRunTasks {
		if err := validatePreRunTaskConfig(hook, fmt.Sprintf("%s.preRunTask[%d]", prefix, index)); err != nil {
			return err
		}
	}
	if cfg.HistoryKeepCount != nil {
		if *cfg.HistoryKeepCount <= 0 {
			return fmt.Errorf("%s.historyKeepCount must be > 0", prefix)
		}
	}
	if cfg.Worktree != nil {
		if cfg.Worktree.KeepOnSuccess != nil && *cfg.Worktree.KeepOnSuccess < 0 {
			return fmt.Errorf("%s.worktree.keepOnSuccess must be >= 0", prefix)
		}
		if cfg.Worktree.KeepOnFailure != nil && *cfg.Worktree.KeepOnFailure < 0 {
			return fmt.Errorf("%s.worktree.keepOnFailure must be >= 0", prefix)
		}
	}
	return nil
}

// HistoryKeepCountFor returns the effective historyKeepCount for the given taskLabel,
// falling back to the global storage value when not overridden.
func (c *UIConfig) HistoryKeepCountFor(taskLabel string) int {
	if specCfg, ok := c.TaskConfig(taskLabel); ok {
		if specCfg.HistoryKeepCount != nil {
			return *specCfg.HistoryKeepCount
		}
	}
	return c.Storage.HistoryKeepCount
}

// WorktreeRetentionFor returns the effective keepOnSuccess and keepOnFailure
// values for the given taskLabel, falling back to global storage when not overridden.
func (c *UIConfig) WorktreeRetentionFor(taskLabel string) (int, int) {
	keepOnSuccess := c.Storage.Worktree.KeepOnSuccess
	keepOnFailure := c.Storage.Worktree.KeepOnFailure
	if specCfg, ok := c.TaskConfig(taskLabel); ok {
		if specCfg.Worktree != nil {
			if specCfg.Worktree.KeepOnSuccess != nil {
				keepOnSuccess = *specCfg.Worktree.KeepOnSuccess
			}
			if specCfg.Worktree.KeepOnFailure != nil {
				keepOnFailure = *specCfg.Worktree.KeepOnFailure
			}
		}
	}
	return keepOnSuccess, keepOnFailure
}

func validateArtifactRuleConfig(rule ArtifactRuleConfig, prefix string) error {
	if strings.TrimSpace(rule.Path) == "" {
		return fmt.Errorf("%s.path is required", prefix)
	}
	switch rule.Format {
	case "", "zip", "file":
		return nil
	default:
		return fmt.Errorf("%s.format must be one of zip or file, got %q", prefix, rule.Format)
	}
}

func validatePreRunTaskConfig(hook PreRunTaskConfig, prefix string) error {
	if strings.TrimSpace(hook.Command) == "" {
		return fmt.Errorf("%s.command is required", prefix)
	}
	if hook.Shell != nil && strings.TrimSpace(hook.Shell.Executable) == "" {
		return fmt.Errorf("%s.shell.executable is required when shell is set", prefix)
	}
	return nil
}

func validateObjectStorageConfig(cfg ObjectStorageConfig, prefix string) error {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return fmt.Errorf("%s.endpoint is required", prefix)
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return fmt.Errorf("%s.bucket is required", prefix)
	}
	return nil
}

func (a *RepositoryAuthConfig) Validate(source string, remoteSource bool) error {
	a.Type = strings.TrimSpace(a.Type)
	a.Provider = strings.TrimSpace(strings.ToLower(a.Provider))
	a.TokenEnv = strings.TrimSpace(a.TokenEnv)
	a.BaseURL = strings.TrimSpace(a.BaseURL)
	a.Repo = normalizeRepositoryID(a.Repo)
	for index, host := range a.AllowedHosts {
		a.AllowedHosts[index] = normalizeHost(host)
	}
	if !a.RejectBroadScope {
		a.RejectBroadScope = true
	}
	if !a.RequireReadOnly {
		a.RequireReadOnly = true
	}

	if a.Type == "" || a.Type == "none" {
		if a.Provider != "" || a.TokenEnv != "" || a.BaseURL != "" || a.Repo != "" || len(a.AllowedHosts) > 0 {
			return fmt.Errorf("repository.auth fields require repository.auth.type=envToken")
		}
		return nil
	}
	if !remoteSource {
		return fmt.Errorf("repository.auth is only supported for remote repository sources")
	}
	if a.Type != "envToken" {
		return fmt.Errorf("repository.auth.type must be one of none or envToken, got %q", a.Type)
	}
	if a.Provider != "github" && a.Provider != "gitlab" && a.Provider != "bitbucket" {
		return fmt.Errorf("repository.auth.provider must be one of github, gitlab, or bitbucket")
	}
	if a.TokenEnv == "" {
		return fmt.Errorf("repository.auth.tokenEnv is required when repository.auth.type=envToken")
	}
	if a.Repo == "" {
		return fmt.Errorf("repository.auth.repo is required when repository.auth.type=envToken")
	}
	if len(a.AllowedHosts) == 0 {
		return fmt.Errorf("repository.auth.allowedHosts must not be empty when repository.auth.type=envToken")
	}

	sourceURL, err := url.Parse(strings.TrimSpace(source))
	if err != nil || sourceURL.Host == "" {
		return fmt.Errorf("repository.source must be an absolute remote URL when repository.auth is enabled")
	}
	if sourceURL.Scheme != "https" && sourceURL.Scheme != "http" {
		return fmt.Errorf("repository.auth only supports http(s) remote sources")
	}
	sourceHost := normalizeHost(sourceURL.Host)
	if !stringInSlice(a.AllowedHosts, sourceHost) {
		return fmt.Errorf("repository.source host %q is not listed in repository.auth.allowedHosts", sourceHost)
	}
	sourceRepo, err := repositoryIDFromRemoteURL(a.Provider, sourceURL)
	if err != nil {
		return err
	}
	if sourceRepo != a.Repo {
		return fmt.Errorf("repository.auth.repo %q does not match repository.source repo %q", a.Repo, sourceRepo)
	}
	if a.BaseURL != "" {
		parsedBaseURL, err := url.Parse(a.BaseURL)
		if err != nil || parsedBaseURL.Scheme == "" || parsedBaseURL.Host == "" {
			return fmt.Errorf("repository.auth.baseURL must be an absolute http(s) URL")
		}
		if parsedBaseURL.Scheme != "http" && parsedBaseURL.Scheme != "https" {
			return fmt.Errorf("repository.auth.baseURL must use http or https")
		}
		if !stringInSlice(a.AllowedHosts, normalizeHost(parsedBaseURL.Host)) {
			return fmt.Errorf("repository.auth.baseURL host %q is not listed in repository.auth.allowedHosts", normalizeHost(parsedBaseURL.Host))
		}
	}
	return nil
}

func (c *UIConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

func (c *UIConfig) PublicBaseURL() string {
	if c.Server.PublicURL != "" {
		return c.Server.PublicURL
	}
	return "http://" + c.Addr()
}

func (c *UIConfig) MatchBranch(branchName string) bool {
	if len(c.Branches) == 0 {
		return !strings.Contains(branchName, "/")
	}
	return matchPatterns(c.Branches, branchName)
}

func (c *UIConfig) MatchTask(taskLabel string) bool {
	_, ok := c.TaskConfig(taskLabel)
	return ok
}

func (c *UIConfig) TaskConfig(taskLabel string) (*TaskUIConfig, bool) {
	for _, spec := range c.Tasks {
		if matchGlob(spec.Pattern, taskLabel) {
			cfg := spec.Config
			if len(cfg.Artifacts) > 0 {
				artifacts := make([]ArtifactRuleConfig, len(cfg.Artifacts))
				for index, artifact := range cfg.Artifacts {
					if artifact.Format == "" {
						artifact.Format = "zip"
					}
					if artifact.NameTemplate == "" {
						artifact.NameTemplate = DefaultArtifactArchive
					}
					artifacts[index] = artifact
				}
				cfg.Artifacts = artifacts
			}
			if cfg.Worktree != nil {
				copied := *cfg.Worktree
				cfg.Worktree = &copied
			}
			return &cfg, true
		}
	}
	return nil, false
}

func (c *UIConfig) MatchingPreRunTasks(taskLabel string) []PreRunTaskConfig {
	cfg, ok := c.TaskConfig(taskLabel)
	if !ok || len(cfg.PreRunTasks) == 0 {
		return nil
	}
	return append([]PreRunTaskConfig(nil), cfg.PreRunTasks...)
}

func (c *UIConfig) UseSparseRunWorkspace(taskLabel string) bool {
	cfg, ok := c.TaskConfig(taskLabel)
	if !ok || cfg.Worktree == nil || cfg.Worktree.Disabled == nil {
		return false
	}
	return *cfg.Worktree.Disabled
}

func (c *UIConfig) MatchUser(claims map[string]any) bool {
	return c.matchUserRules(c.Auth.AllowUsers, claims, true)
}

func (c *UIConfig) IsAdminUser(claims map[string]any) bool {
	return c.matchUserRules(c.Auth.AdminUsers, claims, false)
}

func (c *UIConfig) CanManageTokens(claims map[string]any) bool {
	return c.IsAdminUser(claims)
}

func (c *UIConfig) CanRun(claims map[string]any) bool {
	return c.MatchUser(claims)
}

func (c *UIConfig) matchUserRules(rules []UserAccessRule, claims map[string]any, allowWhenEmpty bool) bool {
	if len(rules) == 0 {
		return allowWhenEmpty
	}
	for _, rule := range rules {
		raw, ok := claims[rule.Claim]
		if !ok {
			continue
		}
		switch value := raw.(type) {
		case string:
			if matchGlob(rule.Value, value) {
				return true
			}
		case []any:
			for _, item := range value {
				if matchGlob(rule.Value, fmt.Sprintf("%v", item)) {
					return true
				}
			}
		default:
			if matchGlob(rule.Value, fmt.Sprintf("%v", value)) {
				return true
			}
		}
	}
	return false
}

func validateUserAccessRules(rules []UserAccessRule, fieldName string) error {
	for index, rule := range rules {
		if strings.TrimSpace(rule.Claim) == "" {
			return fmt.Errorf("%s[%d].claim must not be empty", fieldName, index)
		}
		if strings.TrimSpace(rule.Value) == "" {
			return fmt.Errorf("%s[%d].value must not be empty", fieldName, index)
		}
		if _, err := path.Match(rule.Value, "sample"); err != nil {
			return fmt.Errorf("%s[%d].value %q is invalid: %w", fieldName, index, rule.Value, err)
		}
	}
	return nil
}

func matchPatterns(patterns []string, value string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		if matchGlob(pattern, value) {
			return true
		}
	}
	return false
}

func matchGlob(pattern string, value string) bool {
	matched, err := path.Match(pattern, value)
	if err != nil {
		return false
	}
	return matched
}

func (c RepositoryConfig) IsRemoteSource() bool {
	source := strings.TrimSpace(c.Source)
	return strings.HasPrefix(source, "http://") ||
		strings.HasPrefix(source, "https://") ||
		strings.HasPrefix(source, "file://") ||
		strings.HasPrefix(source, "ssh://") ||
		strings.HasPrefix(source, "git@")
}

func normalizeHost(host string) string {
	return strings.ToLower(strings.TrimSpace(host))
}

func normalizeRepositoryID(repo string) string {
	return strings.Trim(strings.TrimSpace(strings.TrimSuffix(repo, ".git")), "/")
}

func repositoryIDFromRemoteURL(provider string, remoteURL *url.URL) (string, error) {
	repoPath := normalizeRepositoryID(remoteURL.Path)
	if repoPath == "" {
		return "", fmt.Errorf("repository.source must include a repository path")
	}
	segments := strings.Split(repoPath, "/")
	switch provider {
	case "github", "bitbucket":
		if len(segments) != 2 {
			return "", fmt.Errorf("repository.source must use owner/repo form for %s", provider)
		}
	case "gitlab":
		if len(segments) < 2 {
			return "", fmt.Errorf("repository.source must include group/project form for gitlab")
		}
	}
	return repoPath, nil
}

func stringInSlice(values []string, target string) bool {
	return slices.Contains(values, target)
}
