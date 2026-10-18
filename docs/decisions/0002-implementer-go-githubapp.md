# Decision 0002 — Go implementation with `go-githubapp`

- Date: 2026-10-18
- Status: Accepted implementation direction for GOV-001A by the Work Item request;
  offline implementation only, integration not enabled
- Scope: this repository's Implementation Agent integration. Governance remains
  [puen-engineer ADR-0002](https://github.com/puennaja/puen-engineer/blob/22799c6d4d9d22ce106b0a1e86142e31d89ea111/decisions/0002-provider-neutral-pr-governance.md).

## Context and decision

The initial GOV-001A Draft PR used Python's standard library. The Work Item owner
selected Go and `github.com/palantir/go-githubapp` as the sole directly selected
third-party GitHub integration framework. We migrated the application and its
synthetic offline security tests to Go, pinned v0.48.0, and removed Python source.
This release requires Go 1.27; the local toolchain auto-downloaded Go 1.27.2.

The framework provides GitHub App JWT signing and App clients, static-token REST
clients, REST client creation, and a webhook dispatcher that verifies signatures.
Webhook dispatch is a future reviewer concern and is not activated here. The
[source assessment](../research/go-githubapp.md) records tagged source links and gaps.

**Crucial scoping choice:** `NewInstallationClient` creates a token for the entire
installation grant; it accepts no repository or permission options. GOV-001A does
not use it. The private broker gets an App client, sends a GitHub token request
with exactly one repository ID and an operation-specific subset of the App's
Metadata read / Contents write / Pull requests write permission map. It checks
the returned scope and expiry,
then creates a private framework `NewTokenClient`. A missing or wider scope fails
closed. No direct `ghinstallation`, `go-githubauth`, or `go-github` module is selected
by our source. The framework's generic REST client is used with in-house
standard-library request and response structures; no parallel GitHub SDK is added.

## In-house responsibilities

Go standard-library code validates configuration and immutable human assignment,
restricts four operations, constructs regular-file Git commits on authorized
feature refs, protects the base and workflow paths, enforces Draft/own-PR checks,
checks token scope/expiry, and writes non-secret fsynced audit events. A stdio host
has bounded input and fixed error codes. Framework client middleware blocks
network by default, fixes HTTPS origin, denies redirects and unexpected methods;
activation rejects proxy environment variables. The broker alone holds App PEM,
JWTs, tokens and full framework clients. No untrusted PR code runs in that process.

The host is not a deployment supervisor. A human-approved bridge must authenticate
the caller, bind a private stream to the assignment and isolate agent and broker OS
contexts. GitHub Contents/PR write permissions are broader than individual actions;
server branch rules and live negative tests are required. No branch/work-item
scoping exists at GitHub's token level. Race, timeout or post-write audit failure
can leave a remote mutation; audit marks effects unknown and requires reconciliation.
Go and framework internals cannot promise secure memory erasure.

## Dependencies and validation

`go.mod` directly requires only `go-githubapp v0.48.0`. Its pinned transitive
module graph includes `ghinstallation/v2`, `go-github/v92`, `go-github/v88`,
`githubv4`, `oauth2`, JWT, caching, logging, metrics and their smaller support
modules. These are indirect requirements from the framework, recorded with exact
versions in `go.mod` and checksums in `go.sum`; their presence does not authorize
direct use in application code. Future dependency upgrades require a scoped review.

Offline Go unit/race/vet results are in [tests/README.md](../../tests/README.md).
Live App authentication, GitHub permission enforcement, actor attribution, token
revocation and rollback are **NOT RUN**. Registration/installation, private-key
generation, live token issuance, Ruleset changes, paid AI APIs and reviewer App
implementation are outside this decision. The Human Gate Owner must approve any
activation after exact scopes, repos, actors, custody, test results, audit and
rollback evidence are recorded.
