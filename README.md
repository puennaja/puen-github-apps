# Puen GitHub Apps

Provider-neutral GitHub integrations for My Engineer.

## Purpose
This repository hosts **source code and configuration templates** for GitHub Apps that separate:
- **Implementation Agent** — an authorized implementation tool (Codex, Claude Code, or another provider).
- **AI Reviewer** — independently evaluates pull requests and records findings; never grants human approval.
- **Human Gate Owner** — retains final merge authority.

Governance decisions remain in [puen-engineer](https://github.com/puennaja/puen-engineer). This repository holds implementation and integration details, not new governance authority.

## Status
**Bootstrap / design only.** No GitHub App has been registered, no installation or token broker has been implemented, and no permissions have been tested. No AI API billing is authorized.

## Layout
- [AGENTS.md](AGENTS.md) — development and security instructions.
- [apps/implementer/README.md](apps/implementer/README.md) — Implementation Agent integration contract.
- [apps/reviewer/README.md](apps/reviewer/README.md) — independent reviewer integration contract.
- [packages/github-auth/README.md](packages/github-auth/README.md) — future shared authentication boundary.
- [docs/architecture/overview.md](docs/architecture/overview.md) — roles and lifecycle.
- [docs/security/permissions.md](docs/security/permissions.md) — initial permission matrix and constraints.
- [docs/decisions/0001-bootstrap.md](docs/decisions/0001-bootstrap.md) — design decision.
- [tests/README.md](tests/README.md) — future validation plan.

## Next steps
1. Protect `master` and verify PR-only policy is effective (see [branch protection guide](docs/security/branch-protection.md)).
2. Implement and test App installation/token brokering **only after separate explicit approval**.
3. Verify distinct authenticated GitHub actors for Push/PR and review, plus human-only merge enforcement.

**Never store private keys, tokens, credentials, .env files or confidential production data in this public repository.**
