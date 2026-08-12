<!--
Thanks for contributing. A few notes before you submit:

- Maintainers: prefix the title with the Linear issue ID, e.g. [ENT-40] fix: ...
- External contributors: a conventional-commits title is fine, e.g. fix: ...
- Keep the PR bounded to one change. Two unrelated fixes are two PRs.
-->

## What changed

<!-- A sentence or two. The diff shows what; this is for orientation. -->

## Why

<!--
The part that matters most, and the part a reviewer cannot reconstruct from
the diff. What was broken, or what became possible? If it fixes an issue,
link it here so it closes automatically:

Closes #123
-->

## How it was verified

<!--
Which tests you added or changed, and anything you checked by hand. "CI is
green" on its own is not verification if the change is not covered by tests.
-->

## Screenshots

<!-- For UI changes. Delete this section otherwise. -->

## Checklist

- [ ] Tests cover the change, and would have failed before it
- [ ] `bun run lint` and `bunx tsc --noEmit` pass
- [ ] `bun run test` passes locally
- [ ] No em dashes in copy, comments, commit messages, or this description
- [ ] No secrets, credentials, or real customer data in the diff
- [ ] Documentation updated if behaviour or setup changed
