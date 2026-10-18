# Protect master — owner checklist

This is an **operator checklist**, not an applied configuration.

1. Go to repository **Settings -> Rules -> Rulesets** (or Branch protection rules if appropriate for your plan).
2. Create/enable a ruleset for exact branch `master`; require pull requests before merge.
3. Require the appropriate status checks (once added and observed); never list nonexistent checks. Require approvals and dismiss stale approvals where available.
4. Disallow force pushes and deletions; consider preventing bypass, and verify owner/admin/bot behavior under your actual GitHub plan.
5. Test using a disposable branch/PR: direct push to master rejected; PR checks run; unapproved merge blocked where supported; human-authorized merge succeeds.
6. Record observed results and limitations in `tests/`. If GitHub account/plan cannot enforce owner-only approval, document the procedural fallback explicitly; do not call it technically enforced.

**Do not accidentally lock the owner out**. Check rule preview and bypass policy before enabling.

Note: a solo contributor cannot approve their own PR as an independent reviewer. Separating PR creator from human reviewer is a design goal, but enforcement depends on actual authentication actors and GitHub plan.
