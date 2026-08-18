# The console and marketing site (ENT-241). Build from the repository root:
#
#     docker build -f apps/web/Dockerfile .
#
# The build context is the root rather than this directory, for the same reason
# core-api's is: this is a Bun workspace, and the manifest that decides what
# gets installed is the root `package.json` with its `bun.lock`. A context
# rooted at apps/web cannot see either, so the install would resolve from a
# manifest nobody committed a lockfile for.
#
# WHY THIS IS A PRODUCTION BUILD AND NOT `next dev`
#
# It would be less code to run the dev server in a container, and it would be
# wrong twice over.
#
# For a self-hoster it is the difference between the product and a development
# tool: `next dev` compiles on demand, serves unminified output, and is
# explicitly not supported for anything but development.
#
# For this repository it closes the gap ENT-241 was raised about. `next dev` is
# not `next build && next start`: a server component that compiles in dev and
# fails under a production build is invisible to every check here, because
# nothing built the production output. Now the image build does, so that
# failure stops a container from existing rather than being discovered by a
# customer.
#
# TWO TOOLCHAINS, WHICH IS DELIBERATE
#
# Bun is the package manager and Node runs Next, which is exactly the split
# AGENTS.md describes and CI already uses. Both are pinned to the versions in
# .github/workflows/ci.yml, so the image resolves the dependency graph the same
# way a pull request does.

FROM node:22-alpine AS build

# Bun, copied from its own image rather than installed by a script, and pinned
# for the reason every toolchain here is pinned: an installer that moves on its
# own is the one thing you never want changing quietly under a build. Same
# arrangement apps/intelligence/Dockerfile uses for uv.
COPY --from=oven/bun:1.3.14-alpine /usr/local/bin/bun /usr/local/bin/bun

# Bun's musl binary links against libstdc++, which the Node alpine image does
# not carry. Without this the copy above exits with a loader error that reads
# like a corrupt binary.
RUN apk add --no-cache libstdc++

WORKDIR /src

# Manifests first, so a source-only change does not re-resolve and re-download
# the dependency graph. Every workspace member's package.json has to be here:
# `bun install` reads the workspace list from the root manifest and fails on a
# member it cannot find, even one no image ever runs.
COPY package.json bun.lock ./
COPY apps/web/package.json apps/web/
COPY db/tests/package.json db/tests/

# `--frozen-lockfile` refuses to update the lockfile. A build that quietly
# resolved something other than what a developer tested is a build whose green
# run means less than it appears to.
RUN bun install --frozen-lockfile

COPY apps/web apps/web

# The one value Next inlines at build time rather than reading at runtime.
#
# `NEXT_PUBLIC_*` is substituted into the output during compilation, so it
# cannot be supplied by compose the way everything else is. It feeds robots.txt
# and the sitemap, so the default is the edge's address on the bundled stack; a
# self-hoster on a real domain passes their own:
#
#     docker build --build-arg NEXT_PUBLIC_APP_URL=https://compliance.example
#
# Nothing security relevant depends on it. The OAuth redirect URI, the issuer
# and the core-api address are all read at runtime, which is what lets one
# image serve any deployment.
ARG NEXT_PUBLIC_APP_URL=http://localhost:8000
ENV NEXT_PUBLIC_APP_URL=${NEXT_PUBLIC_APP_URL}

# Next runs on Node, so the binary is invoked directly rather than through
# `bun run`. Telemetry off: a build in someone else's deployment should not
# phone anywhere.
ENV NEXT_TELEMETRY_DISABLED=1
RUN cd apps/web && ../../node_modules/.bin/next build


FROM node:22-alpine

# No shell tools beyond what the base carries, and no bun: the runtime needs
# neither a package manager nor a compiler. This is not distroless the way
# core-api's runtime is, because Next's server is Node and Node needs its own
# base; the reduction available here is to copy three directories rather than
# the repository.
WORKDIR /app

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
# 0.0.0.0 rather than the standalone server's localhost default, or the port is
# published and nothing outside the container can reach it.
ENV HOSTNAME=0.0.0.0
ENV PORT=3000

# The three things `output: 'standalone'` produces, and nothing else.
#
# `standalone` holds server.js plus the traced subset of node_modules, roughly
# 80 MB against the gigabyte-plus the full install occupies. `static` and
# `public` are not traced, because they are read from disk rather than
# imported, so Next documents copying them separately and an image without them
# serves pages with no CSS and no images.
#
# The paths carry `apps/web` because `outputFileTracingRoot` is the monorepo
# root: the standalone tree mirrors the repository, so the server sits where
# the workspace does.
COPY --from=build --chown=node:node /src/apps/web/.next/standalone ./
COPY --from=build --chown=node:node /src/apps/web/.next/static ./apps/web/.next/static
COPY --from=build --chown=node:node /src/apps/web/public ./apps/web/public

# The `node` user ships with this base image. The process serves requests and
# writes nothing to disk, so it has no reason to be root.
USER node

EXPOSE 3000

# The standalone entry point. `next start` is not available here, and that is
# the point of standalone: no Next CLI, no dependency resolution at boot, just
# a Node server.
CMD ["node", "apps/web/server.js"]
