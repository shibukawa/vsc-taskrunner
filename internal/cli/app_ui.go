package cli

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	gitutil "vsc-taskrunner/internal/git"
	"vsc-taskrunner/internal/uiconfig"
	"vsc-taskrunner/internal/web"
)

func (a *App) runUI(args []string) int {
	if len(args) > 0 && args[0] == "init" {
		return a.runUIInit(args[1:])
	}
	if len(args) > 0 && args[0] == "edit" {
		return a.runUIEdit(args[1:])
	}

	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var repoPath string
	var configPath string
	var host string
	var port int
	var noAuth bool
	var openBrowser bool
	var runtimeMode string
	var redactNames multiValueFlag
	var redactTokens multiValueFlag

	fs.StringVar(&repoPath, "repo", "", "git repository root")
	fs.StringVar(&configPath, "config", "", "path to runtask-ui.yaml")
	fs.StringVar(&host, "host", "", "override bind host")
	fs.IntVar(&port, "port", 0, "override bind port")
	fs.BoolVar(&noAuth, "no-auth", false, "disable authentication")
	fs.BoolVar(&openBrowser, "open", false, "open UI in browser")
	fs.StringVar(&runtimeMode, "runtime-mode", web.RuntimeModeAlwaysOn.String(), "runtime mode: always-on or serverless")
	fs.Var(&redactNames, "redact-name", "extra secret-like env/input name to redact; repeatable")
	fs.Var(&redactTokens, "redact-token", "extra secret-like env/input token to redact; repeatable")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	store, cfg, historyDir, err := a.loadUIContext(repoPath, configPath)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	if host != "" {
		cfg.Server.Host = host
	}
	if port != 0 {
		cfg.Server.Port = port
	}
	if noAuth {
		cfg.Auth.NoAuth = true
	}
	switch mode := web.RuntimeMode(runtimeMode); mode {
	case web.RuntimeModeAlwaysOn, web.RuntimeModeServerless:
	default:
		fmt.Fprintf(a.stderr, "invalid runtime mode %q: must be always-on or serverless\n", runtimeMode)
		return 2
	}
	cfg.Logging.Redaction.Names = append(cfg.Logging.Redaction.Names, []string(redactNames)...)
	cfg.Logging.Redaction.Tokens = append(cfg.Logging.Redaction.Tokens, []string(redactTokens)...)

	history, indexStoreKind, runStoreKind, err := newHistoryStoreWithKinds(context.Background(), historyDir, cfg)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	log.Printf(
		"runtask startup storage backend=%q history=%q indexStore=%q runStore=%q",
		cfg.Storage.Backend,
		historyDir,
		indexStoreKind,
		runStoreKind,
	)
	if err := history.Prune(cfg.Storage.HistoryKeepCount); err != nil {
		fmt.Fprintf(a.stderr, "startup cleanup warning: prune history: %v\n", err)
	}
	if err := history.PruneWorktrees(cfg.Storage.Worktree.KeepOnSuccess, cfg.Storage.Worktree.KeepOnFailure); err != nil {
		fmt.Fprintf(a.stderr, "startup cleanup warning: prune worktrees: %v\n", err)
	}
	if err := store.Maintenance(context.Background()); err != nil {
		fmt.Fprintf(a.stderr, "startup cleanup warning: prepare repository cache: %v\n", err)
	}
	log.Printf("runtask startup cleanup completed cache=%q history=%q", store.BasePath(), historyDir)

	manager := web.NewTaskManager(store, cfg, history)
	scheduleStateStore, scheduleStoreKind, err := newScheduleStateStoreWithKind(context.Background(), historyDir, cfg)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	scheduler := web.NewScheduler(store, cfg, manager, scheduleStateStore)
	authenticator, err := web.NewAuthenticator(cfg)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	if cfg.Auth.APITokens.Enabled {
		tokenService, storeKind, err := newAPITokenServiceWithKind(context.Background(), historyDir, cfg)
		if err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		authenticator.SetTokenService(tokenService)
		log.Printf("runtask startup api token store=%q", storeKind)
	}
	server := web.NewServer(store, cfg, manager, authenticator)
	server.SetRuntimeMode(web.RuntimeMode(runtimeMode))
	server.SetScheduler(scheduler)
	log.Printf("runtask startup schedule state store=%q", scheduleStoreKind)
	go scheduler.RunLoop(context.Background())
	go func() {
		if err := server.WarmBranchMetadata(context.Background()); err != nil {
			log.Printf("runtask startup branch preload failed: %v", err)
		}
	}()

	if openBrowser {
		go openURL(cfg.PublicBaseURL())
	}

	fmt.Fprintf(a.stdout, "runtask ui listening on %s (bind: %s)\n", cfg.PublicBaseURL(), cfg.Addr())
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	return 0
}

func (a *App) runCleanup(args []string) int {
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var repoPath string
	var configPath string

	fs.StringVar(&repoPath, "repo", "", "git repository root")
	fs.StringVar(&configPath, "config", "", "path to runtask-ui.yaml")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	_, cfg, historyDir, err := a.loadUIContext(repoPath, configPath)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	history, _, _, err := newHistoryStoreWithKinds(context.Background(), historyDir, cfg)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	if err := history.Prune(cfg.Storage.HistoryKeepCount); err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	if err := history.PruneWorktrees(cfg.Storage.Worktree.KeepOnSuccess, cfg.Storage.Worktree.KeepOnFailure); err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	fmt.Fprintln(a.stdout, "cleanup completed")
	return 0
}

func (a *App) loadUIContext(repoPath string, configPath string) (gitutil.RepositoryStore, *uiconfig.UIConfig, string, error) {
	wd, err := a.wd()
	if err != nil {
		return nil, nil, "", fmt.Errorf("resolve working directory: %w", err)
	}
	if configPath == "" {
		configPath = filepath.Join(wd, "runtask-ui.yaml")
		if _, err := os.Stat(configPath); err != nil && repoPath != "" && !isRemoteSource(repoPath) {
			repoRoot, repoErr := gitutil.FindRepoRoot(repoPath)
			if repoErr == nil {
				configPath = filepath.Join(repoRoot, "runtask-ui.yaml")
			}
		}
	}
	cfg, err := uiconfig.LoadConfig(configPath)
	if err != nil {
		return nil, nil, "", err
	}
	if repoPath != "" {
		cfg.Repository.Source = repoPath
	}
	paths := uiconfig.ResolveRuntimePaths(wd, cfg)
	cfg.Repository.Source = paths.RepositorySource
	store, err := gitutil.NewBareRepositoryStore(
		cfg.Repository.Source,
		paths.CachePath,
		uiconfig.DefaultFetchDepth,
		[]string{uiconfig.DefaultTasksSparsePath},
		cfg.Repository.Auth,
	)
	if err != nil {
		return nil, nil, "", err
	}
	if err := store.Maintenance(context.Background()); err != nil {
		return nil, nil, "", err
	}
	basePath := store.BasePath()
	if !isRemoteSource(cfg.Repository.Source) {
		basePath = cfg.Repository.Source
	}
	historyDir := paths.HistoryDir
	if historyDir == "" {
		historyDir = filepath.Join(basePath, cfg.Storage.HistoryDir)
	}

	return store, cfg, historyDir, nil
}

func isRemoteSource(source string) bool {
	return (uiconfig.RepositoryConfig{Source: source}).IsRemoteSource()
}

func openURL(rawURL string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Start()
}

func newHistoryStore(ctx context.Context, historyDir string, cfg *uiconfig.UIConfig) (*web.HistoryStore, error) {
	history, _, _, err := newHistoryStoreWithKinds(ctx, historyDir, cfg)
	return history, err
}

func newAPITokenServiceWithKind(ctx context.Context, historyDir string, cfg *uiconfig.UIConfig) (*web.APITokenService, string, error) {
	switch cfg.Auth.APITokens.Store.Backend {
	case "", "local":
		path := uiconfig.ResolveAPITokenLocalPath(historyDir, cfg.Auth.APITokens.Store.LocalPath)
		if path == "" {
			path = cfg.Auth.APITokens.Store.LocalPath
		}
		store := web.NewLocalAPITokenStore(path)
		return web.NewAPITokenService(cfg.Auth.APITokens, store), "*web.LocalAPITokenStore", nil
	case "object":
		store, err := web.NewObjectAPITokenStore(ctx, web.ObjectIndexStoreOptions{
			Endpoint:       cfg.Auth.APITokens.Store.Object.Endpoint,
			Bucket:         cfg.Auth.APITokens.Store.Object.Bucket,
			Region:         cfg.Auth.APITokens.Store.Object.Region,
			AccessKey:      cfg.Auth.APITokens.Store.Object.AccessKey,
			SecretKey:      cfg.Auth.APITokens.Store.Object.SecretKey,
			Prefix:         cfg.Auth.APITokens.Store.Object.Prefix,
			ForcePathStyle: cfg.Auth.APITokens.Store.Object.ForcePathStyle,
		})
		if err != nil {
			return nil, "", err
		}
		return web.NewAPITokenService(cfg.Auth.APITokens, store), "*web.ObjectAPITokenStore", nil
	default:
		return nil, "", fmt.Errorf("unsupported api token storage backend %q", cfg.Auth.APITokens.Store.Backend)
	}
}

func newHistoryStoreWithKinds(ctx context.Context, historyDir string, cfg *uiconfig.UIConfig) (*web.HistoryStore, string, string, error) {
	switch cfg.Storage.Backend {
	case "", "local":
		history, err := web.NewHistoryStore(historyDir)
		return history, "*web.LocalIndexStore", "*web.LocalRunStore", err
	case "object":
		indexStore, err := web.NewObjectIndexStore(ctx, web.ObjectIndexStoreOptions{
			Endpoint:       cfg.Storage.Object.Endpoint,
			Bucket:         cfg.Storage.Object.Bucket,
			Region:         cfg.Storage.Object.Region,
			AccessKey:      cfg.Storage.Object.AccessKey,
			SecretKey:      cfg.Storage.Object.SecretKey,
			Prefix:         cfg.Storage.Object.Prefix,
			ForcePathStyle: cfg.Storage.Object.ForcePathStyle,
		})
		if err != nil {
			return nil, "", "", err
		}
		runStore, err := web.NewObjectRunStore(ctx, historyDir, web.ObjectIndexStoreOptions{
			Endpoint:       cfg.Storage.Object.Endpoint,
			Bucket:         cfg.Storage.Object.Bucket,
			Region:         cfg.Storage.Object.Region,
			AccessKey:      cfg.Storage.Object.AccessKey,
			SecretKey:      cfg.Storage.Object.SecretKey,
			Prefix:         cfg.Storage.Object.Prefix,
			ForcePathStyle: cfg.Storage.Object.ForcePathStyle,
		})
		if err != nil {
			return nil, "", "", err
		}
		history, err := web.NewHistoryStoreWithStores(historyDir, indexStore, runStore)
		return history, "*web.ObjectIndexStore", "*web.ObjectRunStore", err
	default:
		return nil, "", "", fmt.Errorf("unsupported storage backend %q", cfg.Storage.Backend)
	}
}

func newScheduleStateStoreWithKind(ctx context.Context, historyDir string, cfg *uiconfig.UIConfig) (web.ScheduleStateStore, string, error) {
	switch cfg.Storage.Backend {
	case "", "local":
		return web.NewLocalScheduleStateStore(historyDir), "*web.LocalScheduleStateStore", nil
	case "object":
		store, err := web.NewObjectScheduleStateStore(ctx, web.ObjectIndexStoreOptions{
			Endpoint:       cfg.Storage.Object.Endpoint,
			Bucket:         cfg.Storage.Object.Bucket,
			Region:         cfg.Storage.Object.Region,
			AccessKey:      cfg.Storage.Object.AccessKey,
			SecretKey:      cfg.Storage.Object.SecretKey,
			Prefix:         cfg.Storage.Object.Prefix,
			ForcePathStyle: cfg.Storage.Object.ForcePathStyle,
		})
		if err != nil {
			return nil, "", err
		}
		return store, "*web.ObjectScheduleStateStore", nil
	default:
		return nil, "", fmt.Errorf("unsupported storage backend %q", cfg.Storage.Backend)
	}
}
