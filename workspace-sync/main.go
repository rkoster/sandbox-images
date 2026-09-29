package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rkoster/cf-sandbox-workspace-sync/internal/credentials"
	"k8s.io/utils/inotify"
)

type config struct {
	Workspace string
	Rclone    string
	Remote    string
	Bucket    string
	Prefix    string
	Region    string
	Endpoint  string
	AccessKey string
	SecretKey string
	Session   string
	Poll      time.Duration
	Debounce  time.Duration
}

type cfApplication struct {
	ID string `json:"application_id"`
}

type commandRunner func(context.Context, string, ...string) ([]byte, error)

var errBindingMissing = errors.New("workspace-sync service binding not found")

func main() {
	checkBinding := flag.Bool("check-binding", false, "exit 0 when workspace-sync binding exists")
	flag.Parse()
	cfg, enabled, err := s3ConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	if *checkBinding {
		if enabled {
			os.Exit(0)
		}
		os.Exit(1)
	}
	if !enabled {
		log.Printf("workspace-sync service binding not found; persistence daemon disabled")
		return
	}
	applicationJSON, err := os.ReadFile("/tmp/workspace-sync-vcap-application.json")
	if err != nil {
		log.Fatalf("read private VCAP_APPLICATION handoff: %v", err)
	}
	var app cfApplication
	if err := json.Unmarshal(applicationJSON, &app); err != nil || app.ID == "" {
		log.Fatal("VCAP_APPLICATION application_id is required for workspace S3 prefix isolation")
	}
	if cfg.Prefix == "" {
		cfg.Prefix = "workspaces/" + app.ID
	} else {
		cfg.Prefix = strings.Trim(cfg.Prefix, "/") + "/" + app.ID
	}
	_ = os.Remove("/tmp/workspace-sync-vcap-application.json")
	if err := os.Unsetenv("VCAP_SERVICES"); err != nil {
		log.Fatalf("clear service credentials from child process environment: %v", err)
	}
	readyFile := env("WORKSPACE_SYNC_READY_FILE", "")
	cfg.Workspace = env("WORKSPACE_SYNC_PATH", "/workspace")
	cfg.Rclone = env("RCLONE_BIN", "/usr/bin/rclone")
	cfg.Remote = env("WORKSPACE_SYNC_REMOTE", "s3")
	cfg.Poll = durationEnv("WORKSPACE_SYNC_POLL_INTERVAL", 5*time.Minute)
	cfg.Debounce = durationEnv("WORKSPACE_SYNC_DEBOUNCE", 2*time.Second)
	if err := run(cfg); err != nil {
		if readyFile != "" {
			_ = os.Remove(readyFile)
		}
		log.Printf("workspace sync daemon: %v", err)
		os.Exit(1)
	}
}

func run(cfg config) error {
	cfg.Workspace = filepath.Clean(cfg.Workspace)
	if cfg.Workspace == "." || cfg.Workspace == "/" || cfg.Workspace == "" {
		return errors.New("workspace path must be a non-root directory")
	}
	if cfg.Remote == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return errors.New("S3 remote, bucket, access key, and secret key are required")
	}
	if cfg.Poll <= 0 || cfg.Debounce < 0 {
		return errors.New("poll interval must be positive and debounce non-negative")
	}
	if _, err := os.Stat(cfg.Rclone); err != nil {
		return fmt.Errorf("rclone executable: %w", err)
	}
	if err := os.MkdirAll(cfg.Workspace, 0755); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	tempConfig, err := os.CreateTemp(os.TempDir(), "workspace-sync-rclone-*.conf")
	if err != nil {
		return fmt.Errorf("create private rclone config: %w", err)
	}
	configPath := tempConfig.Name()
	if err := tempConfig.Chmod(0600); err != nil {
		_ = tempConfig.Close()
		_ = os.Remove(configPath)
		return fmt.Errorf("protect private rclone config: %w", err)
	}
	if err := tempConfig.Close(); err != nil {
		_ = os.Remove(configPath)
		return fmt.Errorf("close private rclone config: %w", err)
	}
	if err := writeRcloneConfig(configPath, cfg); err != nil {
		_ = os.Remove(configPath)
		return err
	}
	defer func() { _ = os.Remove(configPath) }()
	_ = os.Setenv("RCLONE_CONFIG", configPath)

	remote := remotePath(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	runner := func(ctx context.Context, binary string, args ...string) ([]byte, error) {
		return execRcloneWithConfig(ctx, configPath, binary, args...)
	}
	if err := initialSync(ctx, cfg, remote, runner); err != nil {
		return err
	}
	watcher, err := inotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create inotify watcher: %w", err)
	}
	defer watcher.Close()
	if err := watchTree(watcher, cfg.Workspace); err != nil {
		watcher.Close()
		return fmt.Errorf("watch workspace: %w", err)
	}
	if readyFile := env("WORKSPACE_SYNC_READY_FILE", ""); readyFile != "" {
		if err := os.WriteFile(readyFile, []byte("ready\n"), 0600); err != nil {
			watcher.Close()
			return fmt.Errorf("write startup-ready marker: %w", err)
		}
		defer os.Remove(readyFile)
	}
	log.Printf("watching %s; initial restore complete; local deletes are mirrored to S3", cfg.Workspace)
	return supervise(ctx, cfg, watcher, remote, runner)
}

func initialSync(ctx context.Context, cfg config, remote string, runner commandRunner) error {
	listing, err := runner(ctx, cfg.Rclone, "lsf", remote, "--max-depth", "1")
	if err != nil {
		return fmt.Errorf("inspect S3 workspace prefix: %w", err)
	}
	if len(strings.TrimSpace(string(listing))) == 0 {
		log.Printf("S3 workspace prefix is empty; seeding it from %s", cfg.Workspace)
		if _, err := runner(ctx, cfg.Rclone, "copy", cfg.Workspace, remote, "--create-empty-src-dirs", "--links"); err != nil {
			return fmt.Errorf("seed S3 workspace: %w", err)
		}
		return nil
	}
	log.Printf("restoring S3 workspace prefix %s into %s (remote wins startup conflicts)", remote, cfg.Workspace)
	if _, err := runner(ctx, cfg.Rclone, "sync", remote, cfg.Workspace, "--create-empty-src-dirs", "--links", "--delete-after"); err != nil {
		return fmt.Errorf("restore workspace: %w", err)
	}
	return nil
}

func supervise(ctx context.Context, cfg config, watcher *inotify.Watcher, remote string, runner commandRunner) error {
	var debounce <-chan time.Time
	var debounceTimer *time.Timer
	poll := time.NewTicker(cfg.Poll)
	defer poll.Stop()
	syncWorkspace := func(syncCtx context.Context, reason string) {
		log.Printf("syncing workspace to S3 (%s)", reason)
		if _, err := runner(syncCtx, cfg.Rclone, "sync", cfg.Workspace, remote, "--create-empty-src-dirs", "--links", "--delete-after"); err != nil {
			log.Printf("workspace sync failed: %v", err)
		}
	}
	for {
		select {
		case <-ctx.Done():
			finalCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			syncWorkspace(finalCtx, "shutdown")
			cancel()
			return nil
		case event, ok := <-watcher.Event:
			if !ok {
				return errors.New("inotify event channel closed")
			}
			if event.Mask&inotify.InQOverflow != 0 {
				log.Printf("inotify queue overflow; scheduling full workspace reconciliation")
			}
			if event.Mask&inotify.InIsdir != 0 && event.Mask&(inotify.InCreate|inotify.InMovedTo) != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					_ = watchTree(watcher, event.Name)
				}
			}
			if debounceTimer == nil {
				debounceTimer = time.NewTimer(cfg.Debounce)
			} else {
				if !debounceTimer.Stop() {
					select {
					case <-debounceTimer.C:
					default:
					}
				}
				debounceTimer.Reset(cfg.Debounce)
			}
			debounce = debounceTimer.C
		case err, ok := <-watcher.Error:
			if ok && err != nil {
				log.Printf("inotify warning: %v; periodic reconcile remains enabled", err)
			}
		case <-debounce:
			debounce = nil
			syncWorkspace(ctx, "filesystem changes")
		case <-poll.C:
			syncWorkspace(ctx, "periodic reconciliation")
		}
	}
}

func watchTree(watcher *inotify.Watcher, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return watcher.AddWatch(path, inotify.InCreate|inotify.InDelete|inotify.InModify|inotify.InAttrib|inotify.InMovedFrom|inotify.InMovedTo|inotify.InDeleteSelf|inotify.InMoveSelf|inotify.InOnlydir)
		}
		return nil
	})
}

func writeRcloneConfig(path string, cfg config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	contents := fmt.Sprintf("[%s]\ntype = s3\nprovider = Other\nenv_auth = false\naccess_key_id = %s\nsecret_access_key = %s\nregion = %s\nendpoint = %s\nforce_path_style = true\nno_check_bucket = true\n", cfg.Remote, ini(cfg.AccessKey), ini(cfg.SecretKey), ini(cfg.Region), ini(cfg.Endpoint))
	if cfg.Session != "" {
		contents += "session_token = " + ini(cfg.Session) + "\n"
	}
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		return fmt.Errorf("write private rclone config: %w", err)
	}
	return nil
}

func remotePath(cfg config) string {
	prefix := strings.Trim(cfg.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	return cfg.Remote + ":" + cfg.Bucket + "/" + prefix
}

func execRcloneWithConfig(ctx context.Context, configPath, binary string, args ...string) ([]byte, error) {
	full := []string{"--config", configPath, "--log-level", "ERROR"}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, binary, full...)
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

func ini(value string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		log.Printf("invalid %s=%q; using %s", name, value, fallback)
		return fallback
	}
	return parsed
}

func s3ConfigFromEnv() (config, bool, error) {
	binding, err := credentials.FromVCAP(os.Getenv("VCAP_SERVICES"))
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return config{}, false, nil
		}
		return config{}, false, err
	}
	return config{
		Workspace: "/workspace",
		Rclone:    "/usr/bin/rclone",
		Remote:    "s3",
		Bucket:    binding.Credentials.Bucket,
		Prefix:    binding.Credentials.Prefix,
		Region:    binding.Credentials.Region,
		Endpoint:  binding.Credentials.Endpoint,
		AccessKey: binding.Credentials.AccessKeyID,
		SecretKey: binding.Credentials.SecretAccessKey,
		Session:   binding.Credentials.SessionToken,
		Poll:      5 * time.Minute,
		Debounce:  2 * time.Second,
	}, true, nil
}
