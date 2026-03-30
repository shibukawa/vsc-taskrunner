package uiconfig

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

const SchemaURL = "https://raw.githubusercontent.com/shibukawa/vsc-taskrunner/main/config-schema.json"

const schemaComment = "# yaml-language-server: $schema=" + SchemaURL

type GeneratedConfig struct {
	RepositorySource string
	Host             string
	Branches         []string
	Port             int
	Tasks            []GeneratedTask
	Auth             GeneratedAuth
	Storage          GeneratedStorage
	MetricsEnabled   bool
	MaxParallelRuns  int
}

type GeneratedTask struct {
	Label        string
	ArtifactPath string
}

type GeneratedAuth struct {
	NoAuth           bool
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
}

type GeneratedStorage struct {
	Backend string

	HistoryDir string

	ObjectEndpoint       string
	ObjectBucket         string
	ObjectRegion         string
	ObjectAccessKey      string
	ObjectSecretKey      string
	ObjectPrefix         string
	ObjectForcePathStyle bool
}

func (g GeneratedConfig) Build() *UIConfig {
	cfg := DefaultConfig()
	cfg.Repository.Source = g.RepositorySource
	if g.Host != "" {
		cfg.Server.Host = g.Host
	}
	cfg.Server.Port = g.Port
	cfg.Branches = append([]string(nil), g.Branches...)
	cfg.Tasks = make(AllowedTaskSpecs, 0, len(g.Tasks))
	for _, task := range g.Tasks {
		taskCfg := TaskUIConfig{}
		if task.ArtifactPath != "" {
			taskCfg.Artifacts = []ArtifactRuleConfig{{Path: task.ArtifactPath}}
		}
		cfg.Tasks = append(cfg.Tasks, AllowedTaskSpec{
			Pattern: task.Label,
			Config:  taskCfg,
		})
	}

	cfg.Auth.NoAuth = g.Auth.NoAuth
	if !g.Auth.NoAuth {
		cfg.Auth.OIDCIssuer = g.Auth.OIDCIssuer
		cfg.Auth.OIDCClientID = g.Auth.OIDCClientID
		cfg.Auth.OIDCClientSecret = g.Auth.OIDCClientSecret
	}

	cfg.Storage.Backend = g.Storage.Backend
	switch g.Storage.Backend {
	case "object":
		cfg.Storage.Object.Endpoint = g.Storage.ObjectEndpoint
		cfg.Storage.Object.Bucket = g.Storage.ObjectBucket
		cfg.Storage.Object.Region = g.Storage.ObjectRegion
		cfg.Storage.Object.AccessKey = g.Storage.ObjectAccessKey
		cfg.Storage.Object.SecretKey = g.Storage.ObjectSecretKey
		cfg.Storage.Object.Prefix = g.Storage.ObjectPrefix
		cfg.Storage.Object.ForcePathStyle = g.Storage.ObjectForcePathStyle
	default:
		cfg.Storage.HistoryDir = g.Storage.HistoryDir
	}

	cfg.Metrics.Enabled = g.MetricsEnabled
	cfg.Execution.MaxParallelRuns = g.MaxParallelRuns
	return cfg
}

func MarshalGeneratedConfig(cfg *UIConfig) ([]byte, error) {
	return marshalConfig(cfg, false)
}

func MarshalConfig(cfg *UIConfig) ([]byte, error) {
	return marshalConfig(cfg, false)
}

func (c *UIConfig) SetTaskConfig(pattern string, taskCfg TaskUIConfig) {
	for index := range c.Tasks {
		if c.Tasks[index].Pattern == pattern {
			c.Tasks[index].Config = taskCfg
			return
		}
	}
	c.Tasks = append(c.Tasks, AllowedTaskSpec{
		Pattern: pattern,
		Config:  taskCfg,
	})
}

func (c *UIConfig) RemoveTaskConfig(pattern string) bool {
	for index := range c.Tasks {
		if c.Tasks[index].Pattern != pattern {
			continue
		}
		c.Tasks = append(c.Tasks[:index], c.Tasks[index+1:]...)
		return true
	}
	return false
}

func (c *UIConfig) FindExactTaskConfig(pattern string) (TaskUIConfig, bool) {
	for _, spec := range c.Tasks {
		if spec.Pattern == pattern {
			return spec.Config, true
		}
	}
	return TaskUIConfig{}, false
}

func (c *UIConfig) AddBranch(branch string) bool {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return false
	}
	if stringInSlice(c.Branches, branch) {
		return false
	}
	c.Branches = append(c.Branches, branch)
	return true
}

func (c *UIConfig) RemoveBranch(branch string) bool {
	for index, item := range c.Branches {
		if item != branch {
			continue
		}
		c.Branches = append(c.Branches[:index], c.Branches[index+1:]...)
		return true
	}
	return false
}

func (c *UIConfig) SetDefaultBranch(branch string) bool {
	for index, item := range c.Branches {
		if item != branch {
			continue
		}
		if index == 0 {
			return false
		}
		c.Branches = append([]string{branch}, append(c.Branches[:index], c.Branches[index+1:]...)...)
		return true
	}
	return false
}

func marshalConfig(cfg *UIConfig, preserveAll bool) ([]byte, error) {
	view := buildConfigView(cfg, preserveAll)
	data, err := yaml.Marshal(view)
	if err != nil {
		return nil, err
	}
	return prependSchemaComment(bytes.TrimSpace(data)), nil
}

func prependSchemaComment(data []byte) []byte {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return []byte(schemaComment)
	}
	var out bytes.Buffer
	out.WriteString(schemaComment)
	out.WriteByte('\n')
	out.Write(data)
	return out.Bytes()
}

func buildConfigView(cfg *UIConfig, preserveAll bool) configView {
	defaults := DefaultConfig()
	view := configView{
		Server: serverView{
			Port: cfg.Server.Port,
		},
		Repository: repositoryView{
			Source: cfg.Repository.Source,
		},
		Execution: executionView{
			MaxParallelRuns: cfg.Execution.MaxParallelRuns,
		},
		Metrics: metricsView{
			Enabled: cfg.Metrics.Enabled,
		},
		Storage: storageView{
			Backend: cfg.Storage.Backend,
		},
	}

	if cfg.Server.Host != "" && (preserveAll || cfg.Server.Host != defaults.Server.Host) {
		view.Server.Host = cfg.Server.Host
	}
	if cfg.Server.PublicURL != "" {
		view.Server.PublicURL = cfg.Server.PublicURL
	}

	if cfg.Repository.LocalPath != "" {
		view.Repository.LocalPath = cfg.Repository.LocalPath
	}
	if cfg.Repository.CachePath != "" && (preserveAll || cfg.Repository.CachePath != defaults.Repository.CachePath) {
		view.Repository.CachePath = cfg.Repository.CachePath
	}
	if auth := buildRepositoryAuthView(cfg.Repository.Auth, defaults.Repository.Auth, preserveAll); auth != nil {
		view.Repository.Auth = auth
	}

	if auth := buildAuthView(cfg.Auth, defaults.Auth, preserveAll); auth != nil {
		view.Auth = auth
	}
	if len(cfg.Branches) > 0 {
		view.Branches = append([]string(nil), cfg.Branches...)
	}
	if len(cfg.Tasks) > 0 {
		view.Tasks = cfg.Tasks
	}

	if preserveAll || cfg.Metrics.CPUInterval != defaults.Metrics.CPUInterval {
		view.Metrics.CPUInterval = cfg.Metrics.CPUInterval
	}
	if preserveAll || cfg.Metrics.MemoryInterval != defaults.Metrics.MemoryInterval {
		view.Metrics.MemoryInterval = cfg.Metrics.MemoryInterval
	}
	if preserveAll || cfg.Metrics.StorageInterval != defaults.Metrics.StorageInterval {
		view.Metrics.StorageInterval = cfg.Metrics.StorageInterval
	}
	if preserveAll || cfg.Metrics.MemoryHistoryWindow != defaults.Metrics.MemoryHistoryWindow {
		view.Metrics.MemoryHistoryWindow = cfg.Metrics.MemoryHistoryWindow
	}

	if logging := buildLoggingView(cfg.Logging); logging != nil {
		view.Logging = logging
	}

	switch cfg.Storage.Backend {
	case "object":
		view.Storage.Object = &cfg.Storage.Object
	default:
		view.Storage.HistoryDir = cfg.Storage.HistoryDir
	}
	if preserveAll || cfg.Storage.HistoryKeepCount != defaults.Storage.HistoryKeepCount {
		view.Storage.HistoryKeepCount = cfg.Storage.HistoryKeepCount
	}
	if preserveAll || cfg.Storage.Worktree.KeepOnSuccess != defaults.Storage.Worktree.KeepOnSuccess || cfg.Storage.Worktree.KeepOnFailure != defaults.Storage.Worktree.KeepOnFailure {
		view.Storage.Worktree = &cfg.Storage.Worktree
	}

	return view
}

func buildRepositoryAuthView(cfg RepositoryAuthConfig, defaults RepositoryAuthConfig, preserveAll bool) *repositoryAuthView {
	if !preserveAll &&
		cfg.Type == defaults.Type &&
		cfg.Provider == defaults.Provider &&
		cfg.TokenEnv == defaults.TokenEnv &&
		cfg.BaseURL == defaults.BaseURL &&
		cfg.Repo == defaults.Repo &&
		equalStrings(cfg.AllowedHosts, defaults.AllowedHosts) &&
		cfg.RejectBroadScope == defaults.RejectBroadScope &&
		cfg.RequireReadOnly == defaults.RequireReadOnly {
		return nil
	}
	view := &repositoryAuthView{
		Type:             cfg.Type,
		Provider:         cfg.Provider,
		TokenEnv:         cfg.TokenEnv,
		BaseURL:          cfg.BaseURL,
		Repo:             cfg.Repo,
		AllowedHosts:     append([]string(nil), cfg.AllowedHosts...),
		RejectBroadScope: cfg.RejectBroadScope,
		RequireReadOnly:  cfg.RequireReadOnly,
	}
	return view
}

func buildAuthView(cfg AuthConfig, defaults AuthConfig, preserveAll bool) *authView {
	if cfg.NoAuth {
		return &authView{NoAuth: true}
	}
	if cfg.OIDCIssuer == "" && cfg.OIDCClientID == "" && cfg.OIDCClientSecret == "" && cfg.SessionSecret == "" &&
		len(cfg.AllowUsers) == 0 && len(cfg.AdminUsers) == 0 && !cfg.APITokens.Enabled {
		return nil
	}
	view := &authView{
		OIDCIssuer:       cfg.OIDCIssuer,
		OIDCClientID:     cfg.OIDCClientID,
		OIDCClientSecret: cfg.OIDCClientSecret,
		SessionSecret:    cfg.SessionSecret,
		AllowUsers:       cfg.AllowUsers,
		AdminUsers:       cfg.AdminUsers,
	}
	if tokenStore := buildAPITokenStoreView(cfg.APITokens.Store, defaults.APITokens.Store, preserveAll); tokenStore != nil ||
		cfg.APITokens.Enabled || preserveAll || cfg.APITokens.DefaultTTLHours != defaults.APITokens.DefaultTTLHours || cfg.APITokens.MaxPerUser != defaults.APITokens.MaxPerUser {
		view.APITokens = &apiTokenView{
			Enabled:         cfg.APITokens.Enabled,
			DefaultTTLHours: cfg.APITokens.DefaultTTLHours,
			MaxPerUser:      cfg.APITokens.MaxPerUser,
			Store:           tokenStore,
		}
	}
	return view
}

func buildAPITokenStoreView(cfg APITokenStoreConfig, defaults APITokenStoreConfig, preserveAll bool) *apiTokenStoreView {
	if !preserveAll && cfg.Backend == defaults.Backend && cfg.LocalPath == defaults.LocalPath &&
		cfg.Object == (ObjectStorageConfig{}) {
		return nil
	}
	view := &apiTokenStoreView{
		Backend:   cfg.Backend,
		LocalPath: cfg.LocalPath,
	}
	if cfg.Backend == "object" {
		view.Object = &cfg.Object
	}
	return view
}

func buildLoggingView(cfg LoggingConfig) *loggingView {
	if len(cfg.Redaction.Names) == 0 && len(cfg.Redaction.Tokens) == 0 {
		return nil
	}
	return &loggingView{Redaction: &cfg.Redaction}
}

func equalStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type configView struct {
	Server     serverView       `yaml:"server"`
	Repository repositoryView   `yaml:"repository"`
	Auth       *authView        `yaml:"auth,omitempty"`
	Branches   []string         `yaml:"branches,omitempty"`
	Tasks      AllowedTaskSpecs `yaml:"tasks,omitempty"`
	Execution  executionView    `yaml:"execution"`
	Metrics    metricsView      `yaml:"metrics"`
	Logging    *loggingView     `yaml:"logging,omitempty"`
	Storage    storageView      `yaml:"storage"`
}

type serverView struct {
	Host      string `yaml:"host,omitempty"`
	Port      int    `yaml:"port"`
	PublicURL string `yaml:"publicURL,omitempty"`
}

type repositoryView struct {
	Source    string              `yaml:"source"`
	LocalPath string              `yaml:"localPath,omitempty"`
	CachePath string              `yaml:"cachePath,omitempty"`
	Auth      *repositoryAuthView `yaml:"auth,omitempty"`
}

type repositoryAuthView struct {
	Type             string   `yaml:"type,omitempty"`
	Provider         string   `yaml:"provider,omitempty"`
	TokenEnv         string   `yaml:"tokenEnv,omitempty"`
	BaseURL          string   `yaml:"baseURL,omitempty"`
	Repo             string   `yaml:"repo,omitempty"`
	AllowedHosts     []string `yaml:"allowedHosts,omitempty"`
	RejectBroadScope bool     `yaml:"rejectBroadScope,omitempty"`
	RequireReadOnly  bool     `yaml:"requireReadOnly,omitempty"`
}

type authView struct {
	OIDCIssuer       string          `yaml:"oidcIssuer,omitempty"`
	OIDCClientID     string          `yaml:"oidcClientID,omitempty"`
	OIDCClientSecret string          `yaml:"oidcClientSecret,omitempty"`
	NoAuth           bool            `yaml:"noAuth,omitempty"`
	SessionSecret    string          `yaml:"sessionSecret,omitempty"`
	AllowUsers       UserAccessRules `yaml:"allowUsers,omitempty"`
	AdminUsers       UserAccessRules `yaml:"adminUsers,omitempty"`
	APITokens        *apiTokenView   `yaml:"apiTokens,omitempty"`
}

type apiTokenView struct {
	Enabled         bool               `yaml:"enabled,omitempty"`
	DefaultTTLHours int                `yaml:"defaultTTLHours,omitempty"`
	MaxPerUser      int                `yaml:"maxPerUser,omitempty"`
	Store           *apiTokenStoreView `yaml:"store,omitempty"`
}

type apiTokenStoreView struct {
	Backend   string               `yaml:"backend,omitempty"`
	LocalPath string               `yaml:"localPath,omitempty"`
	Object    *ObjectStorageConfig `yaml:"object,omitempty"`
}

type executionView struct {
	MaxParallelRuns int `yaml:"maxParallelRuns"`
}

type metricsView struct {
	Enabled             bool `yaml:"enabled"`
	CPUInterval         int  `yaml:"cpuInterval,omitempty"`
	MemoryInterval      int  `yaml:"memoryInterval,omitempty"`
	StorageInterval     int  `yaml:"storageInterval,omitempty"`
	MemoryHistoryWindow int  `yaml:"memoryHistoryWindow,omitempty"`
}

type loggingView struct {
	Redaction *RedactionConfig `yaml:"redaction,omitempty"`
}

type storageView struct {
	HistoryDir       string               `yaml:"historyDir,omitempty"`
	HistoryKeepCount int                  `yaml:"historyKeepCount,omitempty"`
	Backend          string               `yaml:"backend"`
	Object           *ObjectStorageConfig `yaml:"object,omitempty"`
	Worktree         *WorktreeConfig      `yaml:"worktree,omitempty"`
}
