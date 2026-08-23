<!--
The prose matters more than the form. Say what changed and why, then delete
every heading and checkbox below that doesn't apply to this change -- a docs
typo shouldn't have to answer for the converter.
-->

## What this does

<!--
What changed, and why. If the diff doesn't make the reasoning obvious, say what
you ruled out and why: that's the part a reviewer can't reconstruct from the
code.
-->

Fixes #

## Verification

- [ ] `make check` passes locally (vet, fmt-check, test, build, lint -- the same target CI runs)

<!--
If this touches anything that talks to Confluence, say whether you exercised it
against a real instance and which flavor: Cloud, Cloud through the
api.atlassian.com gateway with a scoped token, or Data Center. `body.storage`
proves only what was stored, never what takes effect -- see docs/confluence/.
-->

## If it applies

- [ ] Converter output changed: goldens regenerated with `make regen-regressions`, and the diff read rather than just accepted
- [ ] A command's `--json` output changed: `schema/json-output/v1.json` updated to match
- [ ] A new subcommand: it's in the schema's `command` enum *with* an `if`/`then` branch, and it has a `ValidArgsFunction`
- [ ] Learned something new about how Confluence behaves: written up in `docs/confluence/` with how it was verified
- [ ] Commits follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) and each stands on its own -- they land individually on a rebase merge
