# GOV-003B review worker boundary

Status: **offline prototype; real providers disabled**. Governance follows [puen-engineer ADR-0002](https://github.com/puennaja/puen-engineer/blob/master/decisions/0002-provider-neutral-pr-governance.md). GOV-003A's `NewService` still selects `UnavailableEngine`; this change does not activate an AI model or App.

```text
signed webhook -> exact-head orchestrator -> protected-base requirements
  -> review.v1 bounded JSON request -> separately isolated worker [future]
  -> provider adapter -> bounded review.v1 JSON response
  -> schema, model, evidence, added-line and exact-SHA validation
  -> SHA-bound advisory Check Run -> human approval/merge
```

`internal/reviewworker` owns the versioned request/response contract. A request carries repository, PR number, exact head/base SHAs, operator-owned work item and authorization reference, untrusted PR body/file patches, base-SHA requirements and time/memory budgets. The caller must prove the requirements came from the protected base; the protocol cannot authenticate their provenance. The current private `workerEngine` bridge converts GOV-003A evidence but cannot be selected via the exported production constructor. No agent receives a GitHub client or token.

Requests are at most 1 MiB: 100 files, 64 KiB per patch, 512 KiB combined diff, 32 KiB PR body, 12 requirements, 32 KiB each, 256 KiB combined requirements. `AGENTS.md` is mandatory. Responses are at most 64 KiB with at most 20 findings. Each finding must have BLOCKER/MAJOR/MINOR severity, nonempty evidence and a line added by the supplied diff. A completed marker, exact reviewed SHA and expected provider/model are mandatory. Missing, truncated, malformed or stale output becomes `REVIEW_UNAVAILABLE`. A clean completed review could only become advisory `READY_FOR_HUMAN_REVIEW` after a future, separately approved activation; BLOCKER/MAJOR yields `BLOCKED`. Neither status approves or merges.

The private process transport starts a fixed executable with fixed arguments and no shell; it supplies only bounded JSON on stdin, captures bounded stdout/stderr, clears inherited environment and extra files, and kills the process group on timeout/cancel. Its tests use only the test binary as a synthetic worker. This transport is **not** an OS sandbox. Memory budget is protocol metadata and is **not enforced**. It does not prevent filesystem reads, network access, child processes, dynamic library loading or access through inherited OS resources outside those handled by Go. Real provider adapters return `review_worker_unavailable` unconditionally. No provider command is launched by this change.

The reviewer remains the credential-bearing process. A later approved deployment needs a separate worker container/VM or equivalent independently verified filesystem, network, PID, user, memory and credential boundaries; authenticated one-way evidence transfer; process provenance and pinned binary versions; job-level cancellation; operational limits and audit retention. The App process must never execute PR code or run a provider CLI in its own filesystem/OS context. A rollout must also close GOV-003A durability and live App permission gaps. Restart-safe queue/dedup, exact actor/token evidence, race reconciliation and rollback remain open.

Provider compatibility findings as checked 2026-10-18:

| Provider | Documented noninteractive mode | Local observation | Activation gap |
| --- | --- | --- | --- |
| Codex CLI | [Official OpenAI Cookbook](https://developers.openai.com/cookbook/examples/codex/build_iterative_repair_loops_with_codex) shows `codex exec` with stdin and JSON schema; local `codex exec --help` lists `--sandbox read-only`, `--output-schema`, `-` stdin and `--ephemeral`. | `codex-cli 0.162.1` present; help only, no inference. | Command template, actual read-only/OS isolation, auth custody, unattended terms and cost unverified. |
| Claude Code CLI | [Official Claude headless docs](https://code.claude.com/docs/en/headless) show `claude -p`, `--bare`, JSON schema/output; bare mode requires separate API credentials and does not use subscription login. | `claude` executable absent; invocation **NOT RUN**. | Version/flags, isolation, auth, entitlement and cost unverified. |

The documentation shows capabilities, not authorization to automate either subscription. No executable template is enabled, because CLI process isolation and account rights are unresolved. Future activation must pin a reviewed CLI version and a fixed argument template, pass data only on stdin, allow no unapproved network or tool execution, and record real provider/model metadata and costs.
