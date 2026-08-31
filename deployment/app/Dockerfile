# One image, four binaries: the three services and the tool that migrates the
# database. The sql is not copied -- go:embed compiles it into every binary, so
# which migrations a build carries is a fact about the build.
#
# Which binary runs is `command` in the compose file. Separate images differing
# only in their entrypoint would be separate builds of the same code, and as
# many chances for them to be built from different commits.
#
# Both base images are ARGs because the network, not the architecture, is what
# decides them here -- see the mirror loop below.

ARG GO_BASE_IMAGE=golang:1.26-alpine
ARG RUNTIME_BASE_IMAGE=alpine:3.22

FROM ${GO_BASE_IMAGE} AS build

# apk, through whichever mirror answers.
#
# The official CDN is tried first and usually works. The rest are for the days
# it does not: this host cannot reach several networks, and a blocked mirror
# hangs rather than refusing, so each attempt is bounded by `timeout` and fails
# fast instead of stalling the build.
#
# The Alpine version is read from the image rather than written here, so the
# repository lines can never drift from the base image above.
RUN set -eux; \
    alpine_ver="v$(cut -d. -f1,2 /etc/alpine-release)"; \
    success=0; \
    for mirror in \
      https://dl-cdn.alpinelinux.org/alpine \
      https://mirror.arvancloud.ir/alpine \
      https://mirror.liara.ir/alpine \
      https://mirrors.pardisco.co/alpine \
    ; do \
      echo "Trying $mirror ($alpine_ver)"; \
      printf '%s\n%s\n' \
        "$mirror/$alpine_ver/main" \
        "$mirror/$alpine_ver/community" \
        > /etc/apk/repositories; \
      if timeout 20 apk update >/dev/null 2>&1 && \
         timeout 60 apk add --no-cache git ca-certificates >/dev/null 2>&1; then \
        echo "Using $mirror"; success=1; break; \
      fi; \
    done; \
    [ "$success" = 1 ]

# Modules, through whichever proxy answers.
#
# The deploy host cannot reach Google: gcr.io answered 403 on a base image and
# proxy.golang.org answers 403 on module zips.
#
# The separator is `|` and not `,`, and that is the whole fix. With a comma Go
# falls through only on 404 and 410; a 403 or a dead connection stops it dead,
# which is exactly what happened. With `|` it moves to the next entry on any
# error, so the list ends at `direct` -- fetching from the source repositories
# themselves, and GitHub is reachable, since cloning this repository is how the
# build starts.
#
# GOSUMDB is off because sum.golang.org is Google's too. Integrity is not lost:
# go.sum is committed and every module is still verified against it. What is
# given up is the second opinion on modules NOT already in go.sum, and there
# are none -- `go mod download` reads the file we ship.
#
# Both are ARGs, so a host with a different set of reachable mirrors passes its
# own without editing this file:
#   docker build --build-arg GOPROXY='https://my.mirror|direct' ...
ARG GOPROXY=https://go.devneeds.ir|https://proxy.golang.org|direct
ARG GOSUMDB=off
ENV GOPROXY=${GOPROXY} \
    GOSUMDB=${GOSUMDB}

WORKDIR /src

# Dependencies in their own layer, so a code change does not re-download the
# module graph.
#
# The sdk's go.mod comes too, and has to: the root go.mod carries
# `replace github.com/Serajian/srosha/sdk/go => ./sdk/go`, so `go mod download`
# reads it. Only the two files, so the layer still caches on dependency changes
# rather than on every edit to the sdk.
COPY go.mod go.sum ./
COPY sdk/go/go.mod sdk/go/go.sum ./sdk/go/
RUN go mod download

COPY . .

# Static, so the runtime image needs no libc at all. -s -w drops the symbol
# table and DWARF; -trimpath keeps the build machine's paths out of the binary.
ENV CGO_ENABLED=0
RUN go build -ldflags="-s -w" -trimpath -o /out/gateway    ./cmd/gateway \
 && go build -ldflags="-s -w" -trimpath -o /out/dispatcher ./cmd/dispatcher \
 && go build -ldflags="-s -w" -trimpath -o /out/console    ./cmd/console \
 && go build -ldflags="-s -w" -trimpath -o /out/migrate    ./cmd/migrate


# alpine, and not distroless, for one reason that has nothing to do with which
# is better: the deploy server cannot reach gcr.io. The build failed there with
# a 403 on the manifest while Docker Hub pulled fine, so the base image has to
# come from a registry that answers.
ARG RUNTIME_BASE_IMAGE
FROM ${RUNTIME_BASE_IMAGE}

# ca-certificates because every channel is TLS, and without them each send
# fails with an opaque x509 error that reads like a bug in our code. tzdata
# because a schedule is read in UTC and a container with no zone database
# cannot say so.
#
# The same mirror loop as the build stage, for the same reason.
RUN set -eux; \
    alpine_ver="v$(cut -d. -f1,2 /etc/alpine-release)"; \
    success=0; \
    for mirror in \
      https://dl-cdn.alpinelinux.org/alpine \
      https://mirror.arvancloud.ir/alpine \
      https://mirror.liara.ir/alpine \
      https://mirrors.pardisco.co/alpine \
    ; do \
      echo "Trying $mirror ($alpine_ver)"; \
      printf '%s\n%s\n' \
        "$mirror/$alpine_ver/main" \
        "$mirror/$alpine_ver/community" \
        > /etc/apk/repositories; \
      if timeout 20 apk update >/dev/null 2>&1 && \
         timeout 60 apk add --no-cache ca-certificates tzdata >/dev/null 2>&1; then \
        echo "Using $mirror"; success=1; break; \
      fi; \
    done; \
    [ "$success" = 1 ]

# Not root. Nothing here writes to disk: the templates and assets are compiled
# into the console binary by go:embed, and the only state is postgres and nats.
RUN adduser -D -u 10001 srosha

WORKDIR /app

COPY --from=build /out/gateway /out/dispatcher /out/console /out/migrate ./

USER srosha

# No ENTRYPOINT and no CMD on purpose: every service names its own command, and
# a default here would be a fourth answer to a question already answered three
# times.
