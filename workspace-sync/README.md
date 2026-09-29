# CF Sandbox Workspace Sync daemon

This source builds a small Linux daemon for the CF cflinuxfs5 sandbox image. It
restores a sandbox's `/workspace` from its bound S3 service before execd or the
user workload starts, watches the directory tree with Linux inotify, and uses
rclone to sync local changes back to S3. New directories are watched too; a
periodic reconciliation covers event queue overflow or missed events.

## Binding contract

Bind a managed Garage service instance using the binding name `workspace-sync`
to the Docker-lifecycle sandbox app. User-provided bindings named
`workspace-sync` or `workspace-sync-*` also work. Credentials may be flat or
nested under `s3`:

```json
{
  "bucket": "sandbox-123",
  "region": "us-east-1",
  "endpoint": "https://s3.example.invalid",
  "access_key_id": "...",
  "secret_access_key": "...",
  "session_token": "optional",
  "prefix": "optional/prefix"
}
```

The sandbox bootstrap extracts only the named `workspace-sync` credential
binding into the daemon process and clears `VCAP_SERVICES` from the shell
workload. The daemon uses `workspaces/<sandbox-app-guid>` as its prefix,
preventing bucket key collisions, and removes binding credentials from its
environment before starting rclone. Credentials are written to a mode-0600,
process-specific temporary rclone config, passed by config-file path, and
removed at exit.

## Sync semantics

- On startup, a non-empty S3 prefix wins: `rclone sync` restores it into
  `/workspace`, replacing any colliding image-local files and removing local
  files absent remotely.
- An empty S3 prefix is seeded once using `rclone copy` from the current
  workspace; this avoids an empty bucket deleting image-provided starter files.
- After startup, local creates/updates/deletes are reconciled to S3 using
  `rclone sync`. Local deletions propagate to the per-sandbox prefix.
- The watcher debounces events and a periodic full sync covers missed events.
- S3 is not a POSIX filesystem. Concurrent writers, locking, atomic rename
  semantics, and rollback/version history are not provided by this daemon.

## Build and test

```sh
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o cf-sandbox-workspace-sync .
```

The sandbox image pins this daemon's dependencies and the rclone binary
separately. The sandbox image bootstrap starts the daemon only when the
`workspace-sync` binding exists and waits for its initial restore before starting
execd or the workload.

The OpenSandbox CAPI facade's opt-in Garage path creates a per-sandbox service
instance and app credential binding. Live broker integration remains subject to
the foundation's Garage broker and network policy configuration.
