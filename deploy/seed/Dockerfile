# The seed job's tools, installed at build time rather than at every boot
# (ENT-240).
#
# `seed.sh` used to open with `apk add postgresql-client curl jq`, which is one
# line and looks harmless, and which meant that every `docker compose up`
# fetched three packages from the Alpine CDN before the stack could finish
# starting. Two things are wrong with that, and neither was visible until the
# air-gap check went looking:
#
#   * It is a runtime fetch, which AGENTS.md forbids for the ordinary reason:
#     what the container runs is then whatever the mirror served today, not
#     what anybody pinned.
#   * It broke air-gapped operation outright. `docs/self-hosting.md` promises
#     an operator that a built and running install makes no outbound request,
#     and the stack could not reach a healthy state with egress blocked because
#     this job could not install its tools. The claim was an audit of the
#     application code, and the thing that falsified it was in the plumbing.
#
# Moving the install here makes it an image build, which is install-time egress
# and is in the documented table alongside every other image this stack pulls.
# `seed.sh` and `seed.sql` stay bind-mounted, so editing a fixture still needs
# no rebuild.
FROM alpine:3.21

RUN apk add --no-cache postgresql-client curl jq
