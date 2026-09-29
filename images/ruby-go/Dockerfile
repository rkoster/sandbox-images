# syntax=docker/dockerfile:1.7

ARG GO_BUILDER_IMAGE=golang:1.26.5-bookworm@sha256:53eeac89074db483fdf0ab3be1df32bf6e47562263d2d0d6baa7f26acb4957dd
ARG CFLINUXFS5_IMAGE=ghcr.io/cloudfoundry/k8s/cflinuxfs5:0.53.0@sha256:63774da0d2fd6f70030160d97ec173954990359a2617661a7fcfa7ee6f4c5966

FROM ${GO_BUILDER_IMAGE} AS execd-builder
ARG EXECD_REVISION=2bb5dde3af6736376ca127b1ae6ed0178727cd01
WORKDIR /src
RUN apt-get update \
    && apt-get install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && git clone https://github.com/opensandbox-group/OpenSandbox.git . \
    && git checkout --detach "${EXECD_REVISION}"
WORKDIR /src/components/execd
RUN go mod download \
    && CGO_ENABLED=0 go build -trimpath -buildvcs=false \
       -ldflags='-buildid= -B none' -o /out/execd ./main.go

FROM ${GO_BUILDER_IMAGE} AS workspace-sync-builder
WORKDIR /src
COPY workspace-sync/go.mod workspace-sync/go.sum ./
RUN go mod download
COPY workspace-sync/ ./
RUN go test ./... && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false \
    -ldflags='-s -w -buildid= -B none' -o /out/cf-sandbox-workspace-sync .

FROM ${GO_BUILDER_IMAGE} AS rclone-binary
ARG RCLONE_VERSION=1.75.1
ARG RCLONE_URI=https://github.com/rclone/rclone/releases/download/v1.75.1/rclone-v1.75.1-linux-amd64.zip
ARG RCLONE_SHA256=982b5aa772841168f8e380f139e9e787b2a105403e32b94da8676a0e1c0a13ab
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl unzip \
    && rm -rf /var/lib/apt/lists/* \
    && curl --fail --location --retry 3 --silent --show-error "$RCLONE_URI" -o /tmp/rclone.zip \
    && echo "$RCLONE_SHA256  /tmp/rclone.zip" | sha256sum -c - \
    && unzip -j /tmp/rclone.zip "rclone-v${RCLONE_VERSION}-linux-amd64/rclone" -d /out \
    && rm -f /tmp/rclone.zip \
    && chmod 0755 /out/rclone

FROM ${CFLINUXFS5_IMAGE} AS cflinuxfs5-rootfs
ARG RUBY_URI=https://buildpacks.cloudfoundry.org/dependencies/ruby/ruby_3.3.11_linux_x64_cflinuxfs5_5b5128df.tgz
ARG RUBY_SHA256=5b5128df028a3173b59cd45281584659cead4d41ada12f31bf325bfb7aac30ce
ARG BUNDLER_URI=https://buildpacks.cloudfoundry.org/dependencies/bundler/bundler_2.7.2_linux_noarch_any-stack_54c53519.tgz
ARG BUNDLER_SHA256=54c53519ff41ac0ad0d498086039f6ee2eac5d1bdef5ad31ab33e8ab2c2b1e21
ARG NODE_URI=https://buildpacks.cloudfoundry.org/dependencies/node/node_24.15.0_linux_x64_cflinuxfs5_23519f1d.tgz
ARG NODE_SHA256=23519f1df6d063ffb2edc5ccc98aa38ee9608ddf11a512441912d4f5e23e7ef3
ARG YARN_URI=https://buildpacks.cloudfoundry.org/dependencies/yarn/yarn_1.22.22_linux_noarch_any-stack_b6132b86.tgz
ARG YARN_SHA256=b6132b864c1948174385b4efec842d21cb964c2ae1e79fedcb4eb783ffdaa8a5
ARG GO_URI=https://buildpacks.cloudfoundry.org/dependencies/go/go_1.26.5_linux_x64_cflinuxfs5_72d90969.tgz
ARG GO_SHA256=72d90969690f8d75d986910b11bab271b46c7cfdb807741d7324f15160dc59be
ARG CF_CLI_VERSION=8.19.0
ARG CF_CLI_URI=https://github.com/cloudfoundry/cli/releases/download/v8.19.0/cf8-cli_8.19.0_linux_x86-64.tgz
ARG CF_CLI_SHA256=98268ab3134bb3a1c97ffce797b4e6d35590a82e006cd098ad7a29f0a5cae7d8

USER root
RUN set -eux; \
    install -d /tmp/toolchain /opt/stack /opt/opensandbox; \
    fetch_extract() { name="$1"; uri="$2"; sha="$3"; \
      curl --fail --location --silent --show-error "$uri" -o "/tmp/${name}.tgz"; \
      echo "${sha}  /tmp/${name}.tgz" | sha256sum -c -; \
      mkdir -p "/tmp/toolchain/${name}"; \
      tar -xzf "/tmp/${name}.tgz" -C "/tmp/toolchain/${name}"; \
    }; \
    fetch_extract ruby "$RUBY_URI" "$RUBY_SHA256"; \
    fetch_extract bundler "$BUNDLER_URI" "$BUNDLER_SHA256"; \
    fetch_extract node "$NODE_URI" "$NODE_SHA256"; \
    fetch_extract yarn "$YARN_URI" "$YARN_SHA256"; \
    fetch_extract go "$GO_URI" "$GO_SHA256"; \
    curl --fail --location --silent --show-error "$CF_CLI_URI" -o /tmp/cf-cli.tgz; \
    echo "$CF_CLI_SHA256  /tmp/cf-cli.tgz" | sha256sum -c -; \
    mkdir -p /tmp/cf-cli && tar -xzf /tmp/cf-cli.tgz -C /tmp/cf-cli; \
    install -m 0755 /tmp/cf-cli/cf8 /usr/bin/cf; \
    printf '%s\n' "$CF_CLI_VERSION" > /opt/cf-cli-version; \
    for component in ruby bundler node yarn go; do \
      cp -a "/tmp/toolchain/${component}/." /opt/stack/; \
    done; \
    rm -rf /opt/stack/test /opt/stack/doc /opt/stack/misc /opt/stack/api /opt/stack/pkg/bootstrap /opt/stack/pkg/obj/go-build /opt/stack/include/node /opt/stack/lib/libruby-static.a /opt/stack/lib/node_modules/npm /opt/stack/lib/node_modules/corepack /opt/stack/lib/v8-compile-cache.js /opt/stack/preinstall.js /opt/stack/package.json /opt/stack/go.env /opt/stack/gopath /usr/share/doc /usr/share/man /usr/share/info /usr/share/lintian /usr/share/bash-completion /usr/share/locale /usr/share/fonts /usr/share/icons /var/lib/apt/lists /usr/share/perl /usr/lib/perl /usr/lib/x86_64-linux-gnu/perl /usr/lib/x86_64-linux-gnu/perl-base /usr/lib/python3.12 /usr/bin/apt /usr/bin/apt-get /usr/bin/dpkg /usr/bin/dpkg-query /usr/bin/dpkg-deb /usr/bin/dpkg-divert /usr/bin/dpkg-maintscript-helper /usr/bin/dpkg-trigger /usr/bin/dpkg-vendor /usr/bin/apt-cache /usr/bin/apt-config /usr/bin/apt-mark /usr/bin/apt-key /usr/bin/update-alternatives /usr/share/aptitude /usr/share/aclocal /usr/share/aclocal-1.16 /usr/share/automake-1.16 /usr/share/cmake-3.28 /usr/share/gdb /usr/share/gitweb /usr/share/lintian /usr/share/source-highlight /usr/share/vim /usr/share/X11 /usr/lib/git-core /usr/lib/ocaml /usr/lib/udev /usr/lib/file /usr/lib/gnupg1 /usr/lib/gnupg /usr/lib/sysstat /usr/lib/tasksel /usr/libexec/sudo /usr/libexec/dpkg /usr/bin/gcc /usr/bin/g++ /usr/bin/make /usr/bin/cmake /usr/bin/git /usr/bin/git-receive-pack /usr/bin/git-upload-pack /usr/bin/perl /usr/bin/perl5.38.2 /usr/bin/python3 /usr/bin/python3.12 /usr/lib/x86_64-linux-gnu/openblas-pthread /usr/lib/x86_64-linux-gnu/lapack /usr/lib/x86_64-linux-gnu/ImageMagick-6.9.12 /usr/share/ImageMagick-6 /usr/bin/convert /usr/bin/identify /usr/bin/animate /usr/bin/compare /usr/bin/composite /usr/bin/montage /usr/bin/graphviz /usr/lib/x86_64-linux-gnu/graphviz /usr/share/graphviz /usr/lib/x86_64-linux-gnu/gprofng /usr/lib/x86_64-linux-gnu/blas /usr/lib/x86_64-linux-gnu/ldscripts /usr/lib/x86_64-linux-gnu/cmake /usr/lib/x86_64-linux-gnu/pkgconfig /usr/share/pkgconfig /usr/share/fonts/X11 /usr/share/X11 /usr/lib/systemd /usr/lib/x86_64-linux-gnu/systemd /usr/lib/udev /usr/bin/dpkg-split /usr/bin/dpkg-statoverride /usr/bin/deb-systemd-helper /usr/bin/deb-systemd-invoke /usr/bin/update-mime-database /usr/bin/fc-cache /usr/bin/fc-list /usr/bin/fc-match /usr/bin/locale-gen /usr/bin/localedef /usr/share/i18n /usr/share/zoneinfo/posix /usr/share/zoneinfo/right /var/lib/apt/lists /var/cache/apt/archives; \
    find /opt/stack/src -type f -name '*_test.go' -delete; \
    find /opt/stack/src -type d -name testdata -prune -exec rm -rf {} +; \
    chmod -R a+rX /opt/stack; \
    rm -rf /tmp/toolchain /tmp/cf-cli /tmp/*.tgz

# Keep only the merged, pruned cflinuxfs5 filesystem. This avoids carrying the
# large upstream base-image layers plus whiteout layers in the final Docker
# image, which matters because CF enforces disk quota over the unpacked image.
FROM scratch AS runtime
COPY --from=cflinuxfs5-rootfs / /

ENV PATH="/opt/stack/bin:/usr/local/bin:/usr/bin:/bin" \
    GOROOT=/opt/stack \
    GOPATH=/workspace/go \
    GEM_HOME=/workspace/gems \
    GEM_PATH=/opt/stack:/workspace/gems \
    BUNDLE_PATH=/workspace/bundle \
    HOME=/workspace/home \
    EXECD_PORT=44772

COPY --from=execd-builder /out/execd /opt/opensandbox/execd
COPY --from=workspace-sync-builder /out/cf-sandbox-workspace-sync /opt/opensandbox/cf-sandbox-workspace-sync
COPY --from=rclone-binary /out/rclone /usr/bin/rclone
COPY images/ruby-go/bootstrap /opt/opensandbox/bootstrap
RUN chmod 0755 /opt/opensandbox/execd /opt/opensandbox/bootstrap /opt/opensandbox/cf-sandbox-workspace-sync \
    && mkdir -p /workspace/go /workspace/gems /workspace/bundle /workspace/home \
    && for binary in bundle bundler erb gem go gofmt irb node npm npx racc rake rbs rdbg rdoc ri ruby syntax_suggest typeprof yarn yarnpkg; do ln -s "/opt/stack/bin/${binary}" "/usr/bin/${binary}"; done \
    && chown -R vcap:vcap /workspace

USER vcap
WORKDIR /workspace
EXPOSE 44772
ENTRYPOINT ["/opt/opensandbox/bootstrap"]
CMD []
