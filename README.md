# Puen GitHub Apps

Provider-neutral GitHub integrations for My Engineer.

## Purpose
This repository hosts **source code and configuration templates** for GitHub Apps that separate:
- **Implementation Agent** — an authorized implementation tool (Codex, Claude Code, or another provider).
- **AI Reviewer** — independently evaluates pull requests and records findings; never grants human approval.
- **Human Gate Owner** — retains final merge authority.

Governance decisions remain in [puen-engineer](https://github.com/puennaja/puen-engineer). This repository holds implementation and integration details, not new governance authority.

## Status
**GOV-001A: offline Go implementation; integration not enabled.** A provider-neutral
Implementation Agent broker, restricted operation interface and synthetic Go tests
are implemented with `github.com/palantir/go-githubapp` as the sole directly selected
third-party GitHub integration framework. No GitHub App is registered or installed.
Live App authentication, GitHub permission enforcement and the human merge gate are **NOT RUN**.
No AI API billing is authorized. GOV-001 remains incomplete under ADR-0002 until live
evidence and human decisions exist.

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

## Next steps
1. Obtain Wit review of the latest HEAD and explicit Human Gate Owner authorization. Wit left a source/design checkpoint on the prior Go commit; formal independent execution verification remains **NOT RUN**.
2. Keep the existing protected `master` ruleset; resolve the bootstrap author/approver identity gate through an approved human decision.
3. Before any activation, separately authorize App registration/installation and live tests; complete the evidence record in the architecture document.

Offline developer checks (Go 1.27.2 toolchain, synthetic fixtures, no credentials):

```bash
go test ./...
go test -race ./...
go vet ./...
```

**Never store private keys, tokens, credentials, .env files or confidential production data in this public repository.**
