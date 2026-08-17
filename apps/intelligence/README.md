# Intelligence

The agent harness (ENT-218, design doc §1.6, §26). Python, because §10 puts
narrative drafting here and the language split is §1.6's rather than a
preference.

What is here today is the floor everything else stands on: token verification
and the tests that prove it refuses what it must. The agent loop, `skills/`,
the guardrail ring and `DraftNarrative` land on top of it.

## Two properties, and they are the reason this service can exist at all

**It accepts no user tokens, ever.** Not "does not expect". Cannot. Two
independent things enforce it: the audience is `kindlast-intelligence` and a
human's console token carries a different one, and `internal:intelligence` is
required and is granted only to the service principal. Either alone would do.

**It holds no database credential and opens no connection.** Go loads and Go
persists; Python drafts and returns.

The two are the same argument from different ends. Intelligence has no tenancy
GUCs and no RLS session, so it has no way to check whether a human should see
what it is processing. It never needs one, because a human cannot reach it.
Give it a connection and that stops being structural and becomes a promise.

`tests/test_no_database.py` asserts the second one, because it is exactly the
kind of property that holds until somebody adds one import to solve a real
problem quickly.

## Running it

```bash
cd apps/intelligence
uv sync
uv run pytest
```

Nothing else is needed. The token battery runs a real authorization server on
localhost and signs real tokens, so it has no dependency on the compose stack.

## The token battery

`tests/test_token_battery.py` is §13.2's battery, ported from
`libs/chassis/oidc`. Each case spoils exactly one property of a token that
would otherwise be accepted, and asserts **which** check refused it. Asserting
only "denied" would let a broken audience check hide behind a working expiry
check.

Nothing is stubbed. §13.2 is explicit that the verifier is the code most worth
testing and that substituting a mock means every test is really testing the
mock, so the fixture is an actual HTTP server serving an actual JWKS.

### One case is more interesting than the others

`test_the_allow_list_holds_only_asymmetric_algorithms` exists because of what
happened when the battery was checked against deliberate sabotage.

Adding `HS256` to `SIGNING_ALGORITHMS` left **all** the token tests green. The
public-key-as-HMAC forgery was still refused, but by a second, independent
mechanism: the verifier hands PyJWK's key *object* to the decoder, and PyJWT's
HMAC path will not take an RSA public key object as a secret. Two real
defences, which is the belt and braces the Go side describes.

The consequence is uncomfortable and worth stating plainly: the forgery test
passes whether or not the allow-list is correct, so it cannot be the thing that
protects the allow-list. A test that stays green while the property it names is
broken is precisely what `AGENTS.md` warns about. So there is now a direct
assertion on the constant, which does go red the moment somebody widens it.

Every security property here was checked the same way: broken deliberately,
watched go red, restored. The allow-list, the audience check, the scope gate
and the no-database rule.

## What it does not have yet

The agent loop, `skills/`, the guardrail middleware ring, the citation
validator, `agent_runs`, and `DraftNarrative`. Those are the rest of ENT-218
and they arrive on top of this, not beside it.
