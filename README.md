# CF cflinuxfs5 coding sandbox image

This repository assembles a public, Docker-lifecycle Cloud Foundry sandbox
image containing Ruby, Bundler, Node.js, Yarn, Go, the CF CLI, upstream
OpenSandbox `execd`, and a workspace sync daemon. The target is Linux/amd64 on
the CF `cflinuxfs5` stack.

The image also packages [`workspace-sync/`](workspace-sync/README.md), a small
Go daemon that restores `/workspace` from a bound per-sandbox S3 bucket and
syncs watched local changes back with rclone 1.75.1. It is started by the Docker
image bootstrap when a `workspace-sync` service binding is present, restores
before execd and the workload start, then is stopped after a finite workload
exits. The daemon has no FUSE mount requirement. The CAPI facade provisions and
binds a Garage service per sandbox when workspace storage is enabled.

The first image is intentionally a toolchain image, not a hardened security
boundary. Workspace synchronization is opt-in and has no POSIX persistence
guarantees; the image does not add user isolation or an endpoint proxy.
Keep sandbox ingress behind the owning-agent mTLS route policy described in the
workspace architecture.

Runtime packaging removes Go tests/test data, Ruby's unused static archive,
Node package-manager caches, and development/documentation files from the
cflinuxfs5 filesystem. It preserves Go's GOROOT compiler sources and tools so
users can build Go programs in the sandbox. The final stage starts from
`scratch` and copies the pruned root filesystem so removed base layers are not
counted toward CF's unpacked disk limit.

## Pins

All toolchain artifacts come from the current upstream Ruby/Go buildpack
manifests, filtered to `cflinuxfs5`, and are pinned by SHA-256 in
`dependencies.lock`:

- cflinuxfs5 base: `ghcr.io/cloudfoundry/k8s/cflinuxfs5:0.53.0`, amd64 digest
  `sha256:63774da0d2fd6f70030160d97ec173954990359a2617661a7fcfa7ee6f4c5966`
- Go build stage: `golang:1.26.5-bookworm`, pinned to its amd64 manifest digest in
  `dependencies.lock`
- Ruby 3.3.11 (latest Ruby artifact presently listed for cflinuxfs5)
- Bundler 2.7.2
- Node.js 24.15.0
- Yarn 1.22.22
- Go 1.26.5
- CF CLI 8.19.0 (Linux x86-64, checksum-pinned)
- execd source revision recorded in `dependencies.lock`
- Ruby/Go buildpack manifest revisions recorded in `dependencies.lock`

Ruby 3.4 is not selected because the current Ruby buildpack manifest does not
publish a Ruby 3.4 artifact for cflinuxfs5. Recheck upstream manifests before
updating the pins.

## Build

The multi-stage build compiles execd from the pinned OpenSandbox source checkout
using Go 1.26.5, then copies the static binary and a minimal bootstrap into the
cflinuxfs5 runtime image. Build context is the OpenSandbox repository root so
the module-local `replace` for `components/internal` resolves:

```sh
docker build --platform linux/amd64 \
  -f images/ruby-go/Dockerfile \
  -t ghcr.io/rkoster/cf-cflinuxfs5-ruby-go:slim-0.1.1 \
  .
```

Run the command from this repository root. The build stage clones the pinned
OpenSandbox revision itself. The base image is pinned by digest in the
Dockerfile. `dependencies.lock` records the other checksums/revisions for review
and updates.

## Runtime contract

The image starts `/opt/opensandbox/bootstrap` as PID 1. The bootstrap restores
the `/opt/stack/bin` toolchain path because CF's Docker lifecycle replaces the
image `PATH`. To make the toolchain work with CF's default `/bin:/usr/bin`
environment—including separate `cf ssh` shells—the image also places symlinks
for Ruby, Bundler, Node.js, Yarn, and Go in `/usr/bin`, pointing to their
installed binaries in `/opt/stack/bin`. The CF CLI is installed directly in
`/usr/bin/cf`. Both the image symlinks and bootstrap PATH are retained for
execd-launched workloads.

By default the image runs
`execd` on port `44772`. CF's Docker lifecycle supplies a process command which
overrides the image command; that command is executed directly. This lets the
facade request a per-sandbox entrypoint while keeping execd running alongside
it. `CF_SANDBOX_PORT` is not an image setting: execd's API remains on `44772`.

```sh
docker run --rm --entrypoint /opt/opensandbox/bootstrap IMAGE /bin/sh -lc 'ruby -v; bundle -v; node --version; yarn --version; go version'
```

## Publish to GHCR

The public image is `ghcr.io/rkoster/cf-cflinuxfs5-ruby-go`. A GitHub Actions
workflow publishes `linux/amd64` on version tags (`v*`) and supports a manual
run. Deploy CF sandbox apps by immutable digest rather than the moving `latest`
tag.

## Upstream sources

- [cflinuxfs5 package](https://github.com/cloudfoundry/cf-k8s-releases/pkgs/container/k8s%2Fcflinuxfs5)
- [Ruby buildpack manifest](https://github.com/cloudfoundry/ruby-buildpack/blob/master/manifest.yml)
- [Go buildpack manifest](https://github.com/cloudfoundry/go-buildpack/blob/master/manifest.yml)
- [OpenSandbox execd](https://github.com/opensandbox-group/OpenSandbox/tree/main/components/execd)
