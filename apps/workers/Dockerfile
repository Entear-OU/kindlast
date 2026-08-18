# The policy gateway image. Build from the repository root:
#
#     docker build -f apps/workers/Dockerfile .
#
# The build context is the root rather than this directory because a Go
# workspace spans modules: workers compiles against gen/go and libs/chassis,
# and neither is reachable from a context rooted here. The same arrangement
# core-api's Dockerfile uses, and the same reason is written up there at
# length: with no `replace` directives and no versioned requires, the sibling
# modules resolve only through go.work, so a standalone build needs the
# workspace file in the context.
#
# It does NOT copy apps/core-api, and that is deliberate rather than an
# economy. This service must not import it, CI checks that it does not, and an
# image whose build context cannot see it is one more place the boundary holds
# without anybody remembering.

FROM golang:1.25-alpine AS build

WORKDIR /src

# Manifests first, so a source-only change does not re-download the module
# cache. go.work.sum is not committed, hence the glob rather than a plain copy.
COPY go.work go.work.sum* ./
COPY apps/core-api/go.mod apps/core-api/go.sum apps/core-api/
COPY apps/workers/go.mod apps/workers/go.sum apps/workers/
COPY gen/go/go.mod gen/go/go.sum gen/go/
COPY libs/chassis/go.mod libs/chassis/go.sum libs/chassis/

RUN go mod download

COPY apps/workers apps/workers
COPY gen/go gen/go
COPY libs/chassis libs/chassis

# Static, because the runtime image has no libc to dynamically link against.
# Symbols and DWARF stripped: of no use in a container nobody can attach a
# debugger to, and a third of the binary.
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags='-s -w' \
      -o /out/workers \
      ./apps/workers/cmd/workers

# distroless/static: no shell, no package manager, no libc.
#
# It matters more here than anywhere else in this repository. This is the one
# process that fetches content from a system somebody else controls, so it is
# the one where a remote code execution would have the most to work with. There
# is no shell for it to reach, no package manager to fetch a tool with, and no
# writable filesystem to stage anything in.
#
# The consequence is that the compose healthcheck cannot be a curl, so the
# binary probes itself; see cmd/workers/main.go.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/workers /workers

USER nonroot:nonroot

# No EXPOSE and no published port. The gateway answers core-api on the compose
# network and nothing else should be able to reach it at all: the shared secret
# is defence in depth behind that, not instead of it.
ENTRYPOINT ["/workers"]
