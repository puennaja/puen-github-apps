# Puen GitHub Apps

Provider-neutral GitHub integrations for My Engineer.

## Purpose
This repository hosts **source code and configuration templates** for GitHub Apps that separate:
- **Implementation Agent** — an authorized implementation tool (Codex, Claude Code, or another provider).
- **AI Reviewer** — independently evaluates pull requests and records findings; never grants human approval.
- **Human Gate Owner** — retains final merge authority.

Governance decisions remain in [puen-engineer](https://github.com/puennaja/puen-engineer). This repository holds implementation and integration details, not new governance authority.

## Status
**GOV-001A: offline Go implementation; App integration deferred.** A provider-neutral
Implementation Agent broker, restricted operation interface and synthetic Go tests
are implemented with `github.com/palantir/go-githubapp` as the sole directly selected
third-party GitHub integration framework. No GitHub App is registered or installed.
Live App authentication, GitHub permission enforcement and the human merge gate are **NOT RUN**.
No AI API billing is authorized. GOV-001 remains incomplete under ADR-0002 until live
evidence and human decisions exist.

**GOV-003A: offline Reviewer MVP; integration and AI engine not enabled.** The
current implementation workflow uses an independent GitHub identity to open PRs.
The new signed-webhook handler, bounded reviewer orchestration, scoped Check Run
adapter and synthetic tests are in `internal/reviewer`. Without an approved real
engine, the Reviewer reports `REVIEW_UNAVAILABLE`, never a passing review.

## Layout
- [AGENTS.md](AGENTS.md) — development and security instructions.
- [apps/implementer/README.md](apps/implementer/README.md) — Implementation Agent integration contract.
- [apps/reviewer/README.md](apps/reviewer/README.md) — independent reviewer integration contract.
- [packages/github-auth/README.md](packages/github-auth/README.md) — trusted authentication boundary.
- [docs/architecture/overview.md](docs/architecture/overview.md) — roles and lifecycle.
- [docs/security/permissions.md](docs/security/permissions.md) — initial permission matrix and constraints.
- [docs/decisions/0001-bootstrap.md](docs/decisions/0001-bootstrap.md) — design decision.
- [tests/README.md](tests/README.md) — offline results and live validation gaps.
- [GOV-001A architecture](docs/architecture/implementer-boundary.md) — implementation decisions, custody, activation and rollback.
- [Go framework decision](docs/decisions/0002-implementer-go-githubapp.md) and [source assessment](docs/research/go-githubapp.md).
- [Reviewer boundary](docs/architecture/reviewer-boundary.md), [permissions](docs/security/reviewer-permissions.md) and [decision](docs/decisions/0003-ai-reviewer.md).

## Next steps
1. Obtain independent review of GOV-003A and explicit Human Gate Owner authorization for any future activation.
2. Verify a real Reviewer engine's terms, isolation, identity and cost before adding a CLI adapter; until then `REVIEW_UNAVAILABLE` is the only runtime review outcome.
3. Keep the protected `master` ruleset and human-only approval/merge. Separately authorize App registration/installation, live tests and the deployment evidence record before enabling a Reviewer.

Offline developer checks (Go 1.27.2 toolchain, synthetic fixtures, no credentials):

```bash
go test ./...
go test -race ./...
go vet ./...
```

**Never store private keys, tokens, credentials, .env files or confidential production data in this public repository.**
