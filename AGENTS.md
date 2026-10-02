# Agent guidance

## CHANGELOG.md

Write changelog entries for bktec users, not for reviewers of the PR. Match the style of entries before 2.4.0 and the tightened 3.2.0 entries: short, plain, and outcome-focused.

- Add entries under `## Unreleased`, one bullet per user-visible change. If several PRs make up one feature, keep one bullet for that feature and update it; don't add a bullet per PR.
- Include only material changes: new features, behavior changes, notable bug fixes, deprecations, and breaking changes.
- Omit internal-only changes with no user-visible behavior change, such as refactors, test changes, dependency or tooling cleanup, and CI changes.
- Aim for one or two sentences per bullet. Start with a verb such as Add, Change, Fix, Remove, or Support.
- Say what users can now do or what behaves differently. If it fixes a bug, briefly say what was wrong before.
- Mention a consequence for existing users only when it changes what they see, such as an env var that was ignored and now takes effect. Mention required server support in one short sentence.
- Skip implementation details, such as API response fields, request contents, retry or lease internals, or every flag, env var, and edge case. Name a flag or env var only when users need it to use the change. Put full usage in the README or `docs/` and link there.
- Mark breaking changes with `⚠️ **BREAKING:**` and state the required user action in one sentence. Link to a migration guide if one exists.
- Do not rewrite released entries unless asked.

Good:

```
- Add support for the Cucumber test runner.
- Fix issue where the run would pass despite errors outside of tests, such as syntax or runtime errors.
- When a selection matches no tests, `bktec run` and `bktec plan` warn and run nothing instead of falling back to the full suite. Error plans still fall back.
- Add `bktec pool plan` and `bktec pool exec` for running tests from a [Test Scheduler pool](./docs/pool-exec.md). Requires Test Scheduler support on the server.
```

Too verbose: two bullets for the pool commands that list exported env vars, lease timing, signal handling, and fallback rules. That detail belongs in `docs/pool-exec.md`. Also too verbose: a bullet that quotes API fields like `selection.applied: true` instead of saying what users see.
