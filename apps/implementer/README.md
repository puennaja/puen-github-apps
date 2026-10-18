# Implementation Agent GitHub App

**GOV-001A: implemented offline; not registered, installed or enabled.**
Logical role: `Implementation Agent`, usable with Codex, Claude Code or another
authorized provider/tool. No provider-specific SDK, AI API or billing dependency.
Suggested App name `puen-implementer` remains unregistered and availability unverified.

`internal/implementer.Gateway.Execute` exposes exactly four operations.
Every gateway is constructed by the trusted host with a human-approved immutable
`Authorization`: work item, provider, tool, authorization reference, repository,
exact `feature/<work-item>-<description>` branch and protected base. Callers cannot
replace this scope in request arguments. The host must authenticate the caller and
bind that caller's private pipes to this authorization before accepting requests.
Task metadata itself does not authenticate an agent.

| Operation | Exact arguments | Behavior |
| --- | --- | --- |
| `getRepository` | none | Project repository ID, name, default branch and App actor; no raw API response |
| `pushFeatureBranch` | `expected_head`, `changes`, `message` | Construct one commit from UTF-8 file contents (or `null` for deletion); create feature ref from the approved base or advance its current head without force |
| `createDraftPullRequest` | `expected_head`, `title`, `body` | Same-repository, protected-base PR; always Draft, maintainer modification disabled |
| `updateOwnPullRequest` | `number`, `changes` | Read with `{}`; otherwise edit only title/body of an open Draft owned by this App on this exact repository/branch/base |

No arbitrary commit/tree imports, Git commands, force pushes, branch deletions,
direct protected-branch writes, merge, approval, ready-for-review transition,
settings/secrets/permissions changes or workflow file changes. File changes cannot
target `.github/workflows`, descendants or the `.github` ancestor. Paths are
canonical relative paths; only regular blobs can be created. No execution of
submitted files occurs. Pushes require expected-head equality, recheck immediately
before writing refs, and use `force: false`. A concurrent ref change can still reject
the write; there are no automatic retries.

## Operator host and configuration

Go 1.27.2 is used with `go-githubapp` v0.48.0 as the only directly selected
third-party GitHub integration framework. The in-house policy, config, audit, HTTP
guards and tests use the Go standard library. Run from the repository root.
Configuration lives in [config.schema.json](config.schema.json), validated at runtime
by `Config.from_env`; [.env.example](../../.env.example) contains placeholders only.
The application does not read `.env` files. Supply configuration only in the trusted
broker's environment. The CLI reads an existing App PEM from an `env:VARIABLE_NAME`
reference. A trusted embedding can resolve `secret://...` externally. The framework
signs App JWTs; no separate signer, key generation or secret-manager adapter is bundled.

The operator-started host uses one bounded JSON request per line on stdin/stdout:

```json
{"operation":"getRepository","arguments":{}}
```

Responses contain `{"result": ...}` or a fixed non-secret `{"error": "code"}`.
The host accepts `--authorization-file` (operator-owned JSON with the `Authorization`
fields) and `--audit-file` (private JSONL file). `go run ./cmd/implementer -h`
describes the arguments. Network access defaults to disabled. The `--allow-network`
flag is solely for a separately approved operator activation; it is never part of
the agent request schema. No host has been started against GitHub for this change.

The agent must receive only private request/response pipes from an authenticated
operator/supervisor bridge. It must not launch this host, supply its startup flags,
read its authorization/configuration, share its OS account, or execute code in the
credential-bearing process. Go package privacy does not provide process
isolation. Deployment/supervisor authentication and OS isolation are **not supplied
or verified** by this library. Do not enable it in a shared agent environment.

Audit events contain only trusted assignment metadata, fixed operation/outcome codes,
App actor, repository, branch, commit SHA and PR number. No titles, bodies, file
contents, request payloads, bearer headers or credential values are logged.
See [boundary architecture and activation gate](../../docs/architecture/implementer-boundary.md).
