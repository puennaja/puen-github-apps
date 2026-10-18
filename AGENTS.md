# Agent instructions — puen-github-apps

- This repository is **PUBLIC**. Never write private keys, installation tokens, OAuth tokens, PATs, .env files, credentials, or confidential data. Use placeholders only.
- Maintain provider-neutral roles: **Implementation Agent** may use Codex, Claude Code, or another provider. Never equate a role with a model vendor.
- Governance source of truth: `puen-engineer`; do not silently amend accepted decisions here.
- No AI/model API calls, paid services, GitHub App registration, credential generation, installations, permission expansion, or live token tests without explicit human approval.
- Keep the Implementation Agent, AI Reviewer, and Human Gate Owner separated. AI must not approve on behalf of the human or merge.
- Before enabling an integration, record exact GitHub App scopes, repos, authenticated actor, token custody/expiry, test results, audit evidence and rollback procedure.
- After bootstrap, work in scoped branches and PRs against protected `master`. Use `[work-item] type: description` commit messages. Never bypass protections.
- No untrusted PR code execution using App private keys or installation tokens; use least privilege and isolated execution.
- Label all validation claims as `PASSED`, `FAILED`, `BLOCKED`, or `NOT RUN`; no speculative green statuses.
