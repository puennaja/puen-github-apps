# GOV-001A Go verification evidence

## Offline checks

Executed on 2026-10-18 on Linux with the downloaded Go 1.27.2 toolchain and
`go-githubapp` v0.48.0. A sandbox attempt initially failed because its Go build
cache was read-only; the required commands below were rerun successfully outside
the sandbox with a writable cache. No GitHub App credential or live API was used.

| Check | Result |
| --- | --- |
| `go test ./...` | **PASSED** — implementer unit package; CLI compiles |
| `go test -race ./...` | **PASSED** — same suite with race detector |
| `go vet ./...` | **PASSED** |
| GitHub Actions `Go offline checks` | **NOT RUN** for this workflow revision; PR check results must be read from GitHub after push |
| Formal independent behavior verification | **NOT RUN** — developer-owned checks only |
| Wit source/design review | **BLOCKED** for activation/merge: a `COMMENTED` checkpoint on `c02c08e7` identified isolation and governance blockers. Tests were not independently rerun; review of the next HEAD is **NOT RUN** |
| Human Gate Owner decision | **BLOCKED** pending explicit authorization |

`internal/implementer/implementer_test.go` ports the prior Python security scenarios
using only synthetic App/token fixtures, a recording fake API and standard-library
HTTP transport doubles. The framework's signed App JWT path is compile-checked, not
executed with a key. No test calls GitHub or generates credentials.

Coverage includes missing/malformed configuration, duplicate repository keys,
unapproved repositories and branches, App/installation identity, exact installation
and per-operation token permissions, single-repository token scope, expiry, disabled network,
redirect denial, operation and path allowlists, workflow and arbitrary commit
denials, feature-ref construction, Draft PR creation and own-PR restrictions.
Audit metadata, pre-auth audit failure, secret-free errors/stdio output, private
audit file protections and bounded host input are also exercised.

This is application policy evidence only. Framework/GitHub behavior under real
credentials, bot actor attribution, branch rules, permission enforcement and
token revocation remain **NOT RUN**.

The `pull_request` workflow runs the same three checks on GitHub-hosted Linux with
the pinned Go toolchain. It receives only a read-only repository token, does not
persist checkout credentials, and receives no App keys or installation tokens.
Passing CI would verify this source revision independently of the developer's
machine; it would not verify live App or GitHub permission behavior.

## Live evidence matrix

| ID | Scenario | Status |
| --- | --- | --- |
| ID-01 | Implementer App authenticated PR/branch push has correct GitHub actor | NOT RUN |
| ID-02 | Reviewer App posts review under independent actor (outside GOV-001A) | NOT RUN |
| ID-03 | Implementer direct push to protected master rejected | NOT RUN |
| ID-04 | Implementer merge and GitHub approval operations rejected | NOT RUN |
| ID-05 | Reviewer content push/merge rejected (outside GOV-001A) | NOT RUN |
| ID-06 | Real token expiry/revocation and repository scoping enforced | NOT RUN |
| ID-07 | New PR head invalidates stale review decision (future governance work) | NOT RUN |
| ID-08 | Human Gate Owner can approve and merge only after required checks | NOT RUN |

Read-only ruleset metadata is recorded in the
[architecture evidence record](../docs/architecture/implementer-boundary.md).
Any later live testing requires a separate human decision and must record actual
API operation, actor, repo/HEAD, expected/observed result, non-secret evidence,
token custody/expiry and rollback without credential values.

## GOV-003A Reviewer offline evidence

The synthetic `internal/reviewer/reviewer_test.go` suite verifies HMAC-SHA256
acceptance/rejection through `go-githubapp`, accepted events, Draft filtering,
repository/installation/base identity, duplicate deliveries, queue overflow and
redelivery, stale SHA suppression, protected-base context, token scope/expiry,
unavailable engine, BLOCKER/MAJOR/MINOR Check Run outcomes, failure retries,
reporting failure, 99/100/101/200-file pagination and the production constructor's
unavailable-only result. No real key, token,
webhook delivery or AI model is used. The CI workflow runs `go test -count=1 ./...`,
`go test -race -count=1 ./...` and `go vet ./...` without App credentials.

| GOV-003A check | Result |
| --- | --- |
| `go test ./...` | **PASSED** — synthetic reviewer and existing implementer packages |
| `go test -race ./...` | **PASSED** — synthetic reviewer and existing implementer packages |
| `go vet ./...` | **PASSED** |
| `go test -cover ./internal/reviewer` | **PASSED** — 69.3% statement coverage; framework App/JWT and real GitHub transport paths are untested |
| GitHub PR CI | **NOT RUN** until feature branch push |
| Live App actor, webhook, token permission and Check Run enforcement | **NOT RUN**; requires separate approval |
| Real isolated AI engine and human approval/merge path | **BLOCKED** pending terms, isolation, identity and owner authorization |
