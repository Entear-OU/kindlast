# The resource server image. Build from the repository root:
#
#     docker build -f apps/core-api/Dockerfile .
#
# The build context is the root rather than this directory because a Go
# workspace spans modules: core-api compiles against gen/go and libs/chassis,
# and neither is reachable from a context rooted here.
#
# Written up in docs/core-api-configuration.md, under "Building the image",
# including the consequence that `go mod tidy` cannot run in these modules.
#
# This is the mechanism §21.4 flagged as unresolved. With no `replace`
# directives and no versioned requires, gen/go and libs/chassis resolve only
# through the workspace, so a standalone build needs the workspace file in the
# context. Copying go.work, which already names exactly these three modules, is
# the option that keeps the no-replace rule intact. The alternative, versioned
# pseudo-modules, would mean publishing and bumping two internal modules on
# every change.

FROM golang:1.25-alpine AS build

WORKDIR /src

# Manifests first, so a source-only change does not re-download the module
# cache. go.work.sum is not committed, hence the glob rather than a plain copy.
COPY go.work go.work.sum* ./
COPY apps/core-api/go.mod apps/core-api/go.sum apps/core-api/
# apps/workers is not built here and its manifest is still required, because
# go.work names it and `go mod download` loads every module the workspace
# lists before it downloads anything. Missing, the build fails with "cannot
# load module apps/workers listed in go.work file", which reads like a
# workspace problem rather than a missing COPY.
COPY apps/workers/go.mod apps/workers/go.sum apps/workers/
COPY gen/go/go.mod gen/go/go.sum gen/go/
COPY libs/chassis/go.mod libs/chassis/go.sum libs/chassis/

RUN go mod download

COPY apps/core-api apps/core-api
COPY gen/go gen/go
COPY libs/chassis libs/chassis

# Static, because the runtime image has no libc to dynamically link against.
# Symbols and DWARF stripped: they are of no use in a container nobody can
# attach a debugger to, and they are a third of the binary.
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags='-s -w' \
      -o /out/core-api \
      ./apps/core-api/cmd/core-api

# distroless/static: no shell, no package manager, no libc. Around 15-25 MB
# total, and a meaningfully smaller attack surface to describe in a security
# questionnaire than an Alpine base (§0.4). The consequence is that the compose
# healthcheck cannot be a curl, so the binary probes itself; see main.go.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/core-api /core-api

USER nonroot:nonroot

# No EXPOSE and no published port anywhere, which is a statement about now
# rather than about the architecture.
#
# Web, mobile and third-party clients are all intended from the start, so this
# service is not shaped around any one of them. Staying dark is defence while
# `web` is the only client that exists: it removes an exposed surface nobody
# needs yet. Build-order step 7b opens the door deliberately, once there is a
# stable contract behind it. Nothing in the code assumes the door stays shut,
# and the interceptor chain verifies every token itself rather than trusting
# where a request came from.
ENTRYPOINT ["/core-api"]
