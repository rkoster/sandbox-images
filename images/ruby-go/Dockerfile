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

FROM ${CFLINUXFS5_IMAGE} AS runtime
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
    find /tmp/toolchain -maxdepth 6 -type f \( -name ruby -o -name go \)

# Buildpack dependency archives contain stack-ready directory trees. Preserve
# their relative layout at /opt/stack and keep the image's normal /usr binaries.
RUN set -eux; \
    for component in ruby bundler node yarn go; do \
      cp -a "/tmp/toolchain/${component}/." /opt/stack/; \
    done; \
    chmod -R a+rX /opt/stack; \
    find /opt/stack -maxdepth 8 -type f \( -name ruby -o -name bundle -o -name node -o -name yarn -o -name go \) -print

ENV PATH="/opt/stack/bin:/usr/local/bin:/usr/bin:/bin" \
    GOPATH=/workspace/go \
    GEM_HOME=/workspace/gems \
    GEM_PATH=/opt/stack:/workspace/gems \
    BUNDLE_PATH=/workspace/bundle \
    HOME=/workspace/home \
    EXECD_PORT=44772

COPY --from=execd-builder /out/execd /opt/opensandbox/execd
COPY images/ruby-go/bootstrap /opt/opensandbox/bootstrap
RUN chmod 0755 /opt/opensandbox/execd /opt/opensandbox/bootstrap \
    && mkdir -p /workspace/go /workspace/gems /workspace/bundle /workspace/home \
    && chown -R vcap:vcap /workspace

USER vcap
WORKDIR /workspace
EXPOSE 44772
ENTRYPOINT ["/opt/opensandbox/bootstrap"]
CMD []
