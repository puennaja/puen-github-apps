# GitHub authentication boundary

**GOV-001A: offline implementation; live authentication NOT RUN.**

`github_auth.boundary` belongs inside the trusted broker process. Its internal
session capability and transport must never be exported as agent tools or imported
into an agent execution process. The agent-facing host returns operation results,
never App JWTs or installation tokens.

`Config.from_env` fails closed on missing/invalid IDs, empty/ambiguous repository
allowlists, duplicate JSON keys, unexpected permissions or missing protected bases.
Configuration binds explicit repository names to numeric IDs, App ID, installation
ID and a non-secret JWT-source reference. The required permission map is exactly:

```json
{"metadata":"read","contents":"write","pull_requests":"write"}
```

For each operation, the internal broker obtains an existing short-lived App JWT
from its trusted source, verifies `/app` and `/app/installations/{id}`, then requests
an installation token for **one numeric repository ID** and the exact permission
map. It rejects suspended/wrong installations, selection of all repositories, broader
or missing permissions, wrong/additional token repositories and invalid expiry.
Tokens must have more than 30 seconds remaining and expire within one hour of receipt.
The expiry buffer is checked before every repository API request. No cache,
refresh retry, raw-token endpoint or global Git/CLI credential configuration exists.
Token/session references are not retained across operations; Python cannot guarantee
secure memory zeroization. Tokens otherwise expire at GitHub; revocation and key
rotation remain operator responsibilities requiring separate approval.

The App actor is derived from the GitHub-authenticated App slug as `<slug>[bot]`.
Own-PR checks require that exact GitHub-returned bot author plus repository/branch/base
and Draft/open status. This mapping is an offline-tested assumption until live actor
evidence is collected; commit author strings never establish authentication.

`GitHubTransport` fixes HTTPS to `api.github.com`, disables redirects, ignores
environment proxy configuration, limits response size, times out and emits fixed
errors without response bodies, headers or URLs. It is network-disabled by default.
It is a trusted internal primitive, not an agent-facing general HTTP interface.
An external JWT source may resolve `secret://...` through a trusted injected callback;
the included CLI only resolves `env:...`. No private-key generation, storage, signer,
registration, installation or live credential tests are part of GOV-001A.

GitHub protocol references (primary sources):
[App JWT authentication](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-json-web-token-jwt-for-a-github-app),
[installation token scoping and expiry](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation),
[App REST endpoints](https://docs.github.com/en/rest/apps/apps).
