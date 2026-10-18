# Proposed Reviewer App permissions — not activated

| Scope | Required | Why |
| --- | --- | --- |
| Metadata | Read | Installation/repository identity |
| Contents | Read | `AGENTS.md`, specs, ADRs and evidence at the protected base SHA |
| Pull requests | Read | Exact head/state and changed-file patches |
| Checks | Write | SHA-bound structured Check Runs and line annotations |

**No** Pull requests write, Contents write, Administration, Actions, Workflows or Secrets permission is selected. A Check Run is an advisory machine outcome, not a GitHub approving review. GitHub allows Check Run creation with `Checks: write`; PR file listing needs `Pull requests: read` ([Checks API](https://docs.github.com/en/rest/checks/runs#create-a-check-run), [PR files API](https://docs.github.com/en/rest/pulls/pulls#list-pull-requests-files)). The App subscribes only to `pull_request` events. All four accepted actions are filtered again in the application after signature verification.

Token request and response must each contain exactly one allowlisted numeric repository and the exact map above. The source rejects a broader installation grant, missing/extra token permissions, wrong repository, suspended installation and invalid expiry. `go-githubapp.NewInstallationClient` is unscoped and forbidden for this boundary. No raw token or unrestricted client reaches an engine or agent. GitHub permissions alone cannot enforce human-only merge or the meaning of an AI Check Run; independent human review, protected branch rules and live negative tests remain mandatory. Actual enforcement is **NOT RUN**.
