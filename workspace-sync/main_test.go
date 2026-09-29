package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rkoster/cf-sandbox-workspace-sync/internal/credentials"
)

func TestLoadServiceBindingFlatCredentials(t *testing.T) {
	raw := `{"user-provided":[{"name":"unrelated","credentials":{}},{"name":"workspace-sync-my-sandbox","credentials":{"bucket":"bucket-a","region":"us-east-1","endpoint":"http://s3.local","access_key_id":"key","secret_access_key":"secret","session_token":"token","prefix":"workspaces/sbx"}}]}`
	binding, err := credentials.FromVCAP(raw)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Credentials.Bucket != "bucket-a" || binding.Credentials.Region != "us-east-1" || binding.Credentials.Prefix != "workspaces/sbx" || binding.Credentials.SessionToken != "token" {
		t.Fatalf("unexpected binding: %+v", binding)
	}
}

func TestLoadServiceBindingWrappedCredentials(t *testing.T) {
	binding, err := credentials.FromVCAP(`{"user-provided":[{"name":"workspace-sync","credentials":{"s3":{"bucket":"bucket-b","access_key_id":"key","secret_access_key":"secret"}}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Credentials.Bucket != "bucket-b" {
		t.Fatalf("bucket=%q", binding.Credentials.Bucket)
	}
}

func TestLoadServiceBindingMissing(t *testing.T) {
	_, err := credentials.FromVCAP(`{"user-provided":[]}`)
	if !errors.Is(err, credentials.ErrNotFound) {
		t.Fatalf("expected missing binding error, got %v", err)
	}
}

func TestRcloneConfigIsPrivateAndUsesBindingValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "rclone.conf")
	cfg := config{Remote: "s3", AccessKey: "key", SecretKey: "secret", Region: "region-1", Endpoint: "https://s3.example", Session: "session"}
	if err := writeRcloneConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("permissions=%o, want 600", got)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"type = s3", "access_key_id = key", "secret_access_key = secret", "region = region-1", "endpoint = https://s3.example", "session_token = session"} {
		if !strings.Contains(string(contents), expected) {
			t.Errorf("config missing %q", expected)
		}
	}
}

func TestS3SyncConfigWritesTemporaryCredentialsWithPrivateMode(t *testing.T) {
	t.TempDir()
	path := filepath.Join(t.TempDir(), "rclone.conf")
	if err := os.WriteFile(path, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config{Remote: "s3", AccessKey: "key", SecretKey: "secret", Region: "test", Bucket: "bucket"}
	if err := writeRcloneConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private rclone config stat=%v err=%v", info, err)
	}
}

func TestInitialSyncSeedsEmptyPrefixAndRestoresExistingPrefix(t *testing.T) {
	for _, test := range []struct {
		name       string
		listing    string
		want       []string
		wantRemote string
	}{
		{name: "empty bucket seeds local state", listing: "", want: []string{"lsf", "s3:bucket/prefix/", "--max-depth", "1", "copy", "/workspace", "s3:bucket/prefix/", "--create-empty-src-dirs", "--links"}, wantRemote: "s3:bucket/prefix"},
		{name: "existing bucket restores remote state", listing: "file.txt\n", want: []string{"lsf", "s3:bucket/prefix/", "--max-depth", "1", "sync", "s3:bucket/prefix/", "/workspace", "--create-empty-src-dirs", "--links", "--delete-after"}, wantRemote: "s3:bucket/prefix"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := config{Workspace: "/workspace", Rclone: "/usr/bin/rclone", Remote: "s3", Bucket: "bucket", Prefix: "prefix"}
			var got []string
			calls := 0
			runner := func(_ context.Context, binary string, args ...string) ([]byte, error) {
				if binary != cfg.Rclone {
					t.Fatalf("binary=%q", binary)
				}
				calls++
				got = append(got, args...)
				if calls == 1 {
					return []byte(test.listing), nil
				}
				return nil, nil
			}
			if err := initialSync(context.Background(), cfg, remotePath(cfg), runner); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("commands=%v want %v", got, test.want)
			}
		})
	}
}

func TestInitialSyncFailureDoesNotReportReady(t *testing.T) {
	workspace := t.TempDir()
	ready := filepath.Join(t.TempDir(), "ready")
	if _, err := os.Stat(ready); !os.IsNotExist(err) {
		t.Fatalf("ready marker unexpectedly exists before successful restore")
	}
	// The integration path writes the marker only after initialSync and watcher setup;
	// a failed rclone invocation must terminate bootstrap before the marker is created.
	if err := initialSync(context.Background(), config{Workspace: workspace, Remote: "s3", Bucket: "bucket"}, "s3:bucket/workspaces/id/", func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("remote unavailable")
	}); err == nil {
		t.Fatal("expected initial sync failure")
	}
	if _, err := os.Stat(ready); !os.IsNotExist(err) {
		t.Fatalf("ready marker exists after failed restore")
	}
}
