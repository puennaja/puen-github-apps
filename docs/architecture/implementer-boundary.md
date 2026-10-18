# GOV-001A implementation boundary

Status: offline implementation; deployment and live enforcement **NOT RUN**.
Work item authorization is the explicit GOV-001A request for this repository, feature
branch, offline checks, commit, push and Draft PR. It grants no registration,
installation, new credentials, live App API tests, governance changes or merge authority.

Governance remains [puen-engineer ADR-0002](https://github.com/puennaja/puen-engineer/blob/22799c6d4d9d22ce106b0a1e86142e31d89ea111/decisions/0002-provider-neutral-pr-governance.md),
read on 2026-10-18. Its role separation and evidence requirements remain unchanged.
The accepted bootstrap decision is preserved. This document records implementation
choices for review, not a new accepted governance decision.

## Components and choices

```text
Human-approved assignment + authenticated operator bridge
  -> private JSON request/response pipes (one assignment per host)
  -> ImplementerGateway: four exact operations + immutable assignment
  -> go-githubapp App client -> one-repository, exact-permission token
  -> private go-githubapp token client (network disabled by default)
  -> non-secret durable local audit events
```

Go 1.27.2 and the pinned `go-githubapp` v0.48.0 framework are used. All in-house
policy, config, audit, HTTP guard and test code uses the Go standard library. No web
framework or provider SDK is needed for the bounded stdio protocol. POSIX host file
protections are required for the audit sink. See the [framework capability assessment](../research/go-githubapp.md)
and [implementation ADR](../decisions/0002-implementer-go-githubapp.md).

The core authorization boundary is the gateway operation allowlist, not prompts.
No generic HTTP, GraphQL, Git subprocess or token-export interface is offered over
stdio. The trusted operator alone chooses configuration, transport, authorization,
secret source and audit destination. Caller authentication, secure pipes and isolation
must be implemented and approved by the deployment owner; they are not established
by starting a Go object or labeling a provider. The source tree must be pinned
to reviewed code and must not execute untrusted PR code inside the broker.

Push constructs a tree on the observed parent, with only validated regular-file edits,
then constructs a single-parent commit and updates an exact authorized feature ref.
This prevents a caller from importing a prebuilt commit that changes workflows or
from passing raw Git arguments. PR creation forces Draft state. PR updates inspect
the current GitHub-returned author/scope and permit only title/body. Empty changes
provide a read operation within the same ownership checks. The base must be both
configured as protected and returned as protected by GitHub; that boolean alone is
not evidence that the required human merge rules are correctly enforced.

## Credential custody and permission limits

An external operator supplies an App PEM from a secure source. `go-githubapp` signs
App JWTs; the private key, JWTs and installation tokens stay in the isolated broker.
The CLI reads a PEM from an `env:...` reference; an approved embedding may resolve
`secret://...` through its own secure source. Agents must run in another OS security
context and receive only the operation stream. No live deployment is authorized.

The framework's unscoped `NewInstallationClient` is not used. Its App client issues a
token request through the GitHub API for one configured numeric repository ID with
the operation's minimum permissions. Repository reads request Metadata read;
feature pushes use Contents write; Draft PR creation/edits use Contents read and
Pull requests write; own-PR reads use Pull requests read. Responses and expiry are
validated before use. The installation itself must use selected repositories and
exactly Metadata read, Contents write and Pull requests write.
No Workflows, Actions, Secrets or Administration permission is accepted. The broker
does not issue agents a Git credential; commits are published via REST Git database
endpoints. No credential is put in a URL, Git configuration, audit event or API result.

GitHub Contents write and Pull requests write are broader than these operations.
They cannot themselves restrict all writes to feature branches or exclude merge and
review-approval APIs. Enforcement therefore depends on the broker, credential isolation
and independently verified branch rules. Workflow permission absence is additional
defense; the gateway also rejects workflow edits and ancestor replacement/deletion.
All actual GitHub negative-permission tests remain **NOT RUN**.

## Audit and failure behavior

Each request records `STARTED` before authentication and then `PASSED` or `FAILED`
for the local broker outcome. Final events include work item, provider/tool,
authorization reference, authenticated App actor when available, repository, branch,
commit SHA and PR number when known. Assignment labels are trusted non-secret metadata.
Raw agent payloads, commit messages, changed files, PR text and upstream error content
are excluded. Denied operation names are replaced with `denied`.

`remote_effect` distinguishes an acknowledged mutation (`APPLIED`), successful read
(`NONE`) and an unconfirmed result (`UNKNOWN`). A local `FAILED` result never means
GitHub rolled back. A timeout, response validation failure or post-write audit failure
can leave a commit/ref/PR created. There are no automatic retries; an operator must
reconcile observed SHA/PR state before retrying. Safe response identifiers are retained
in events when known. Starting audit failure blocks GitHub access; completion audit
failure returns a fixed error and must be reconciled from the prior STARTED record.

The JSONL sink requires an owner-only regular file, refuses symlinks and fsyncs each
append. It is not immutable or centrally collected. Operator authentication, retention,
off-host audit collection, disk quotas and tamper resistance remain deployment work.

## Activation evidence required — no activation performed

Before enabling any integration, the Human Gate Owner must approve and record:

| Evidence | Current state |
| --- | --- |
| Exact App ID/name, installation ID, scopes | **NOT RUN**; placeholders only; proposed exact permissions above |
| Selected repository names and numeric IDs | **NOT RUN** for an installation; explicit allowlist mandatory |
| Authenticated bot actor and independent reviewer/human actors | **NOT RUN** for the App; delivery account is `puennaja` |
| PEM source/private-key owner, custody, rotation/revocation, token expiry and OS isolation | **NOT RUN**; external source and approved isolated host required |
| Live positive/negative permission tests and GitHub evidence links | **NOT RUN**; offline tests are separate evidence |
| Audit retention, incident owner and approved rollback | **NOT RUN** for a deployment; procedure below proposed |
| Independent technical review by Wit and Human Gate Owner authorization | **BLOCKED** for activation/merge after Wit `COMMENTED` source/design checkpoint on `c02c08e7`; independent execution and latest-HEAD review **NOT RUN**; human decision **BLOCKED** |

Read-only GitHub inspection on 2026-10-18 observed ruleset `25336376`
(`protected-master`) active on the default branch, requiring one approving review,
dismissing stale reviews and blocking deletion/non-fast-forward updates. It had no
bypass actors and reported `current_user_can_bypass: never`. Source: repository
ruleset REST response; no rules were changed and no push-to-master test was run.
Live rejection of prohibited App operations remains **NOT RUN**.

Bootstrap identity limitation: delivery uses the already authenticated `puennaja`
account, not the unregistered Implementer App. `puennaja` cannot provide an independent
approving review for its own PR. The merge gate is **BLOCKED** until an authorized
independent human reviewer (Wit, if repository access permits) and the Human Gate Owner
provide the required decisions, or approve another compliant identity resolution.
Do not remove/bypass the rule or fabricate a reviewer. No approval/merge attempt is
part of GOV-001A delivery.

## Proposed rollback and known limits

Before activation: stop the operator host, disconnect its private pipes and retain audit
evidence. No credential has been created by this work and nothing is installed to revoke.
After any separately approved activation: stop the host, have the authorized operator
revoke outstanding tokens/suspend the installation and rotate compromised credentials
as appropriate, then reconcile writes against audit/GitHub evidence. Revert source via
a reviewed PR; never reset or push directly to protected `master`. This rollback has
**NOT RUN** in a live environment.

An offline pull-request CI workflow runs Go test, race and vet without App credentials;
its check result is separate from live enforcement evidence. No reviewer App, AI
invocation, automatic review/merge or Ready transition is implemented. File edits
are UTF-8 regular files with bounded paths/sizes; binary
files, executable modes, symlinks and submodules are unsupported. A long operation can
expire mid-flight. Distributed concurrency, durable idempotency and atomic PR-head
preconditions are not provided by these REST endpoints; observed head changes are
rejected where detectable. GitHub can change after a read; server rules remain essential.
GitHub Enterprise/proxies are unsupported in this minimum fixed-origin adapter.
No secure memory erasure guarantee is possible for Go strings or framework internals.

Primary API references: [Git trees](https://docs.github.com/en/rest/git/trees),
[Git references](https://docs.github.com/en/rest/git/refs),
[Pull requests](https://docs.github.com/en/rest/pulls/pulls).
