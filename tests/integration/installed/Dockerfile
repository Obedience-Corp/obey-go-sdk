ARG GO_IMAGE=golang:1.24-bookworm
FROM ${GO_IMAGE}
ARG SDK_UID=1000
ARG SDK_GID=1000
RUN apt-get update && apt-get install -y --no-install-recommends python3 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --non-unique --gid ${SDK_GID} demo \
    && useradd --non-unique --uid ${SDK_UID} --gid ${SDK_GID} --create-home --home-dir /demo demo
ENV HOME=/demo XDG_CONFIG_HOME=/demo/.config SHELL=/bin/bash \
    GOPATH=/demo/go GOCACHE=/demo/go-cache GOMODCACHE=/demo/go-modules \
    FESTIVAL_HOME=/demo/.obey/installer GOTOOLCHAIN=local \
    PATH=/demo/.obey/installer/bin:/demo/.local/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
USER demo
WORKDIR /sdk
