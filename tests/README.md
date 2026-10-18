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
| Formal independent behavior verification | **NOT RUN** — developer-owned checks only |
| Independent technical review by Wit | **BLOCKED** pending review |
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
