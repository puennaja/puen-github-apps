# Implementation Agent GitHub App — proposed

**Status: design only; not registered or enabled.**

Logical role: `Implementation Agent`, usable with Codex, Claude Code, or any approved tool. Suggested GitHub App name `puen-implementer` (availability unverified).

Responsibilities: push work-item feature branches, open/update Draft PRs, report tests. No direct writes to `master`; no merge or human approval.

Proposed repository permissions: Metadata read (implicit), Contents read/write, Pull requests read/write. No Administration, Actions, Secrets, or Workflows permission. **Note:** Pull requests write may allow merge API calls; branch rules and broker policy must deny those paths, then verify with negative tests. Restrict installation to selected repositories.

Authentication plan (not implemented): short-lived installation tokens issued by a trusted broker with restricted repository/permission scope. Never copy an App private key into agent prompts, CLI global config, workflows or this repository.

Audit each operation: implementation provider/tool identity, work-item ID, authenticated GitHub actor, commit SHA, branch, PR number, authorization reference. Commit author strings alone do not prove authenticated GitHub actor.
