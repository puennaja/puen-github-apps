# GitHub authentication boundary

**GOV-001A: offline implementation; live authentication NOT RUN.**

The Go boundary is in `internal/implementer/broker.go`. Its session and framework
clients belong only inside the trusted broker process. The agent-facing host returns
operation results, never App private keys, JWTs or installation tokens.

`ConfigFromEnv` fails closed on missing/invalid IDs, empty/ambiguous repository
allowlists, duplicate JSON keys, unexpected permissions or missing protected bases.
Configuration binds explicit repository names to numeric IDs, App ID, installation
ID and a non-secret private-key-source reference. The required permission map is exactly:

```json
{"metadata":"read","contents":"write","pull_requests":"write"}
```

For each operation, `go-githubapp.NewAppClient` signs and uses an App JWT from a
trusted PEM source. The broker verifies `/app` and `/app/installations/{id}`, then requests
an installation token for **one numeric repository ID** and the minimum permissions
for that operation: Metadata read for repository reads; Contents write for pushes;
Contents read and Pull requests write for Draft PR creation/edits; Pull requests read
for own-PR reads. It rejects suspended/wrong installations, selection of all repositories, broader
or missing permissions, wrong/additional token repositories and invalid expiry.
Tokens must have more than 30 seconds remaining and expire within one hour of receipt.
The expiry buffer is checked before every repository API request. No cache,
refresh retry, raw-token endpoint or global Git/CLI credential configuration exists.
Token/session references are not retained across operations; Go cannot guarantee
secure memory zeroization. Tokens otherwise expire at GitHub; revocation and key
rotation remain operator responsibilities requiring separate approval.

The App actor is derived from the GitHub-authenticated App slug as `<slug>[bot]`.
Own-PR checks require that exact GitHub-returned bot author plus repository/branch/base
and Draft/open status. This mapping is an offline-tested assumption until live actor
evidence is collected; commit author strings never establish authentication.

The framework's default `NewInstallationClient` is **not used**: it requests an
unscoped installation grant. The broker uses `NewAppClient` and its generic REST
request method to issue a one-repository, operation-scoped token, validates the
response, then privately uses `NewTokenClient`. These clients are never returned to
agents. A standard-library middleware fixes HTTPS to `api.github.com`, rejects
redirects, times out and emits fixed errors without upstream bodies/URLs. Network is
disabled by default; enabling network fails closed if proxy variables are present.
No private-key generation, registration, installation or live credential tests are
part of GOV-001A. [Framework assessment](../../docs/research/go-githubapp.md).

GitHub protocol references (primary sources):
[App JWT authentication](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-json-web-token-jwt-for-a-github-app),
[installation token scoping and expiry](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation),
[App REST endpoints](https://docs.github.com/en/rest/apps/apps).
