# GOV-001A validation evidence

## Offline developer checks

Run from the repository root; Python 3.11+ standard library only. No dependency
installation, credentials, environment file, GitHub access or AI API is needed:

```bash
python3 -m unittest discover -s tests/unit -v
git diff --check
```

Executed on 2026-10-18 on Linux with Python 3.14.4:

| Check | Actual result |
| --- | --- |
| `python3 -m unittest discover -s tests/unit -v` | **PASSED** — 37 tests |
| `git diff --check` | **PASSED** — no whitespace errors |
| Formal independent behavior verification | **NOT RUN** — developer-owned checks only |
| Independent technical review by Wit | **BLOCKED** — pending review |
| Human Gate Owner authorization to proceed beyond Draft PR | **BLOCKED** — pending human decision |

`unit/test_implementer.py` uses only synthetic App/token fixtures and a recording fake
GitHub transport. HTTP adapter tests replace the opener; no sockets reach GitHub.
The default transport refuses network access. Tests cover:

- Missing and malformed configuration, duplicate/ambiguous repositories and permissions.
- Repository allowlist and exact work-item feature-branch authorization.
- App/installation identity, suspension and selected-repository requirements.
- Exact installation/token permissions, narrow token repository scope and expiry on every request.
- Denied merge/approval/settings/secrets/Ready operations and extra argument injection.
- Workflow writes/deletes, ancestor replacement, path traversal and arbitrary commit/tree injection.
- Allowed feature ref creation/advancement, expected-head checks and non-force writes.
- Draft-only creation, own-PR reads/title/body edits and other-author/fork/base/state denials.
- Non-secret audit metadata, audit unavailability, credential-free errors/logs/stdio responses.
- Private persistent audit files, symlink refusal, stdio size limits and redirect refusal.

These fixtures assert our local request construction and policy. They do **not** prove
GitHub will enforce installation scopes, authenticate a real bot, refuse merge or
protected-branch writes, or permit human approval. Parent GOV-001 is not complete.

## Live evidence matrix

All scenarios below remain **NOT RUN**. No App registration/installation, private-key
generation, real token issuance, live App authentication or permission probes were
performed. Existing operator Git/`gh` authentication was used only for authorized
read-only preparation and feature-branch/Draft-PR delivery; it is not App-test evidence.

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

Read-only ruleset metadata was inspected separately; see
[the architecture evidence record](../docs/architecture/implementer-boundary.md).
Before later authorized live testing, record the exact operation, actual actor,
repository, branch/HEAD, expected/observed outcome, non-secret evidence link,
token custody/expiry and rollback. Never record credential values.
