# One image, three binaries, and the tool that migrates the database.
#
# Which binary runs is `command` in the compose file. Three images differing
# only in their entrypoint would be three builds of the same code, and three
# chances for them to be built from different commits.

FROM golang:1.26-alpine AS build

RUN apk add --no-cache git ca-certificates

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

# Pinned, deliberately. `make setup-dev` installs goose @latest, which is right
# for a laptop and wrong for an image that has to be rebuildable: the tool that
# migrates a production database must not change because a day passed.
RUN go install -ldflags="-s -w" github.com/pressly/goose/v3/cmd/goose@v3.27.3

COPY . .

# Static, so the runtime image needs no libc at all. -s -w drops the symbol
# table and DWARF; -trimpath keeps the build machine's paths out of the binary.
ENV CGO_ENABLED=0
RUN go build -ldflags="-s -w" -trimpath -o /out/gateway    ./cmd/gateway \
 && go build -ldflags="-s -w" -trimpath -o /out/dispatcher ./cmd/dispatcher \
 && go build -ldflags="-s -w" -trimpath -o /out/console    ./cmd/console


# alpine, and not distroless, for one reason that has nothing to do with which
# is better: the deploy server cannot reach gcr.io. The build failed there with
# a 403 on the manifest while Docker Hub pulled fine, so the base image has to
# come from a registry that answers.
#
# ca-certificates because every channel is TLS, and without them each send
# fails with an opaque x509 error that reads like a bug in our code. tzdata
# because a schedule is read in UTC and a container with no zone database
# cannot say so.
#
# The shell that comes with it is not why cmd/* learned `healthcheck`. That
# subcommand still earns its place: it asks the same /readyz an orchestrator
# would, so the check cannot drift from what readiness actually means.
FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata

# Not root. Nothing here writes to disk: the templates and assets are compiled
# into the console binary by go:embed, and the only state is postgres and nats.
RUN adduser -D -u 10001 srosha

WORKDIR /app

COPY --from=build /out/gateway /out/dispatcher /out/console ./
COPY --from=build /go/bin/goose ./goose

# The sql travels with the code, so a migration can never come from a different
# commit than the binaries that expect it.
COPY migrations/ ./migrations/

USER srosha

# No ENTRYPOINT and no CMD on purpose: every service names its own command, and
# a default here would be a fourth answer to a question already answered three
# times.
