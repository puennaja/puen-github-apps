# Initial GitHub App permission proposal

**Status: unverified; do not register/install yet without separate approval.**

GOV-001A implements offline validation of the Implementer column as an exact
permission map and a single-repository token request. See the
[authentication boundary](../../packages/github-auth/README.md) and
[activation evidence gate](../architecture/implementer-boundary.md). Offline policy
tests are **PASSED**; actual GitHub permission enforcement remains **NOT RUN**.

| Permission | Implementer | Reviewer | Human |
| --- | --- | --- | --- |
| Metadata | Read | Read | GitHub account |
| Contents | Read/write (feature branches only via rules) | Read | Owner |
| Pull requests | Read/write (draft/edit) | Read | Final decision |
| Checks | None | Write (advisory SHA-bound result) | Owner |
| Administration | None | None | Owner |
| Workflows, Actions, Secrets | None | None | Owner |

For GOV-003A the Reviewer proposal uses **Pull requests: read** and **Checks:
write**, with no PR write. See [reviewer-permissions.md](reviewer-permissions.md).
Neither App's scopes are registered or enforced live.

GitHub App permission scopes are broader than fine-grained operations. **Contents write is not inherently feature-branch-only**, and PR write does not inherently prevent merging. Require verified base-branch rulesets, operation allowlists in broker, and negative permission tests. Never assume documented intent is GitHub enforcement.

Prefer separate App installations or identities per role, selected repositories only, no long-lived agent credentials. Provider-neutral task metadata is not authentication.

Acceptance evidence required: GitHub actor attribution for push/PR/review, denied prohibited operations, branch protection status, tested human-only merge gate, token expiry/revocation, and no secrets in logs.
