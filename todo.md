# TODO

Backlog of deferred work. Design detail for these lives in `_plans/`.

- [ ] **Quoting support for frontmatter values.** The simplified frontmatter parser
  has no quoting, so a value containing whitespace-then-`#` gets truncated by inline
  comment stripping (e.g. `title: Detect # Verify` → `Detect`). Add single/double
  quoted values that suppress inline-comment parsing so such values round-trip.
  (See `_plans/create-subcommand.md` → "Frontmatter parser change".)

- [ ] **`update` space/parent enforcement + moves.** Require `space` and `parent`
  in frontmatter; reconcile against the live page via the legacy move endpoint
  (`PUT /wiki/rest/api/content/{id}/move/{position}/{targetId}`).
  (See `_plans/create-subcommand.md` → "On paper only".)

- [ ] **`fix` command.** Given a `page_id` (or resolvable title), read the live page
  and populate/refresh `space` + `parent` + `page_id` in frontmatter.
  (See `_plans/create-subcommand.md` → "On paper only".)

- [ ] **Support `layout: article`.** Honor a `layout: article` directive (likely a
  frontmatter field) to control the published page's layout/appearance. Exact scope
  TBD.
