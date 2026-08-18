# The Intelligence service image. Build from the repository root:
#
#     docker build -f apps/intelligence/Dockerfile .
#
# The context is the root rather than this directory for the same reason
# core-api's is: this service compiles against generated code that is not
# reachable from a context rooted here. `gen/python` holds the Connect stubs
# and wire types, committed, and there is no packaging inside it because gen/
# holds contract and nothing else (§21.5). So the image copies it and puts it
# on the path, which is the same arrangement the pytest config makes locally.

FROM python:3.12-slim AS build

# uv, pinned for the reason every other toolchain here is pinned: an installer
# that moves on its own is the one thing you never want changing quietly under
# a build.
COPY --from=ghcr.io/astral-sh/uv:0.12.3 /uv /usr/local/bin/uv

WORKDIR /app

# Manifests first, so a source-only change does not re-resolve the dependency
# graph. `--frozen` refuses to update the lockfile: a build that silently
# resolved something different from what a developer tested is a build whose
# green run means less than it appears to.
COPY apps/intelligence/pyproject.toml apps/intelligence/uv.lock ./
RUN uv sync --frozen --no-install-project --no-dev

# README.md comes along because pyproject declares it as the project readme,
# and uv builds the project itself in this step. Without it the build fails
# with "failed to open file /app/README.md", which is a confusing way to be
# told about packaging metadata.
COPY apps/intelligence/README.md ./README.md
COPY apps/intelligence/src ./src
RUN uv sync --frozen --no-dev


FROM python:3.12-slim

# A non-root user, because this process runs a model's output through a parser
# and has no reason to hold more than it needs. It writes nothing to disk.
RUN useradd --system --uid 10001 --create-home intelligence

WORKDIR /app

COPY --from=build --chown=intelligence:intelligence /app/.venv /app/.venv
COPY --from=build --chown=intelligence:intelligence /app/src /app/src
COPY --chown=intelligence:intelligence gen/python /app/gen/python

USER intelligence

# `gen/python` on the path rather than installed, matching the pytest config.
# See the header for why there is no packaging inside a generated tree.
ENV PATH="/app/.venv/bin:$PATH" \
    PYTHONPATH="/app/src:/app/gen/python" \
    PYTHONUNBUFFERED=1

EXPOSE 8090

# No HEALTHCHECK here, deliberately. A Connect service has no unauthenticated
# endpoint to probe, and adding one purely so a healthcheck can reach it would
# widen the surface this service exists to keep narrow. compose depends on it
# through `service_started`, and the honest readiness signal is whether
# core-api's call succeeds.
CMD ["python", "-m", "kindlast_intelligence.main"]
