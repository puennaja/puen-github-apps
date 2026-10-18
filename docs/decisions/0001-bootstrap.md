# Decision 0001 — Bootstrap GitHub Apps integration repository

- Date: 2026-10-18
- Status: Accepted structure, proposed integrations
- Owner: Human Gate Owner

## Context
Keep GitHub App implementation, shared authentication controls, and audit tests separate from harness runtime and governance policy.

## Decision
Bootstrap `puen-github-apps` with explicit folders for implementer/reviewer Apps, shared auth, architecture, security, and tests. Implementation Agent is provider-neutral. Do not create or install Apps, tokens, runners or paid inference services in this change.

## Trade-offs
Better responsibility boundaries and reusable integration at the cost of one additional repository and setup. GitHub actor identities and human-only merge cannot be asserted until verified with GitHub App credentials and branch rules.

## Follow-up
Protect master, assess permissions, then design bounded authentication and test plan under separate approval. Governing ADR lives in `puen-engineer`.
