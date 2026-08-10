# Security policy

## Reporting a vulnerability

**Please do not report security vulnerabilities through public GitHub issues,
discussions, or pull requests.**

Report them through GitHub's private vulnerability reporting instead:

**[Report a vulnerability](https://github.com/Entear-OU/kindlast/security/advisories/new)**

That channel is private between you and the maintainers, lets us collaborate on
a fix in a private fork, and handles CVE assignment if one is warranted.

If you cannot use it for any reason, open a public issue containing **no
details** beyond a request for a private contact channel, and we will follow up.

## What to include

- The type of issue, for example authentication bypass, RLS policy gap, SQL
  injection, prompt injection leading to data disclosure, or cross-tenant data
  access.
- Paths to the source files involved.
- Steps to reproduce, ideally with a proof of concept.
- What an attacker could achieve.

The more precisely you describe the impact, the faster we can triage it.

## What to expect

- **Acknowledgement within 3 working days.**
- An assessment and a target remediation timeline within 10 working days.
- Progress updates as we work on a fix.
- Credit in the advisory when it is published, unless you prefer otherwise.

We ask that you give us a reasonable opportunity to fix an issue before
disclosing it publicly. We will not pursue legal action against researchers who
report in good faith and follow this policy.

## Scope

This policy covers the Kindlast application in this repository.

Particularly relevant given what this product does:

- **Cross-tenant data access.** Kindlast holds organisations' compliance
  posture, gaps, and findings. Anything that lets one tenant observe another's
  data is the highest severity class here. Row Level Security policies live in
  `supabase/migrations/`.
- **Authentication and session handling.** See `lib/auth/` and
  `lib/supabase/`.
- **Signed-link forgery.** The one-tap approve, reject, and unsubscribe links in
  notification emails are signed tokens. Forging one is in scope. See
  `lib/notifications/`.
- **Cron endpoint authentication.** The scheduled endpoints authenticate with a
  shared bearer secret.
- **Prompt injection** that causes disclosure of another tenant's data, or
  causes an agent to take an action outside its intended authority.

Out of scope:

- Findings in third-party dependencies without a demonstrated exploit path
  through Kindlast. Report those upstream.
- Missing security headers or best practices with no demonstrated impact.
- Social engineering, physical attacks, or denial of service through volume.
- Automated scanner output submitted without validation.

## Self-hosted deployments

Kindlast is AGPL-3.0 licensed and can be self-hosted. If you run your own
instance, you are responsible for your deployment: your provider credentials,
your Supabase configuration, and keeping your instance updated. We will
document security-relevant fixes in release notes so self-hosters can assess
their exposure.

## A note on secrets

If you find credentials committed anywhere in this repository or its history,
please report it through the private channel above rather than opening an
issue. Include the file and commit, not the credential value.
