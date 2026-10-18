"""Trusted broker internals. Never import these into an agent execution process."""

from collections.abc import Callable, Mapping
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
import json
import os
import re
import urllib.error
import urllib.request


PERMISSIONS = {"metadata": "read", "contents": "write", "pull_requests": "write"}
REPO = re.compile(r"[A-Za-z0-9-]+/[A-Za-z0-9_.-]+\Z")


class BoundaryError(Exception):
    """Only fixed, non-secret error codes may cross the agent boundary."""

    CODES = frozenset({
        "permission_scope_invalid", "configuration_invalid", "repository_denied",
        "secret_source_unavailable", "network_disabled", "transport_request_invalid",
        "github_response_invalid", "github_transport_failed", "token_expired",
        "app_authentication_invalid", "app_identity_invalid", "installation_invalid",
        "token_repository_scope_invalid", "token_expiry_invalid", "token_invalid",
        "authentication_failed", "authorization_invalid", "branch_denied",
        "operation_denied", "operation_arguments_invalid", "commit_invalid",
        "changes_invalid", "path_denied", "workflow_denied", "pr_invalid",
        "pr_changes_denied", "text_invalid", "repository_identity_invalid",
        "branch_identity_invalid", "base_unprotected", "head_changed",
        "ref_response_invalid", "pr_not_owned_draft", "operation_failed", "audit_unavailable",
    })

    def __init__(self, code):
        safe = isinstance(code, str) and (code in self.CODES
                or re.fullmatch(r"github_http_[45][0-9]{2}", code))
        super().__init__(code if safe else "operation_failed")


def require(condition: bool, code: str) -> None:
    if not condition:
        raise BoundaryError(code)


def validate_permissions(value: object) -> None:
    require(value == PERMISSIONS, "permission_scope_invalid")


def _unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "configuration_invalid")
        result[key] = value
    return result


@dataclass(frozen=True, repr=False)
class Config:
    app_id: int
    installation_id: int
    repositories: tuple[tuple[str, int], ...]
    jwt_source: str
    protected_branches: tuple[str, ...] = ("master", "main")

    def __post_init__(self):
        require(type(self.app_id) is int and self.app_id > 0, "configuration_invalid")
        require(type(self.installation_id) is int and self.installation_id > 0,
                "configuration_invalid")
        require(type(self.repositories) is tuple and 0 < len(self.repositories) <= 100,
                "configuration_invalid")
        names, ids = set(), set()
        for entry in self.repositories:
            require(type(entry) is tuple and len(entry) == 2, "configuration_invalid")
            name, repo_id = entry
            require(isinstance(name, str) and bool(REPO.fullmatch(name))
                    and name.split("/")[1] not in (".", ".."), "configuration_invalid")
            require(type(repo_id) is int and repo_id > 0, "configuration_invalid")
            names.add(name.lower())
            ids.add(repo_id)
        require(len(names) == len(ids) == len(self.repositories), "configuration_invalid")
        require(isinstance(self.jwt_source, str) and bool(re.fullmatch(
            r"(?:env:[A-Z][A-Z0-9_]*|secret://[A-Za-z0-9_/-]+)", self.jwt_source)),
            "configuration_invalid")
        require(type(self.protected_branches) is tuple
                and {"master", "main"}.issubset(self.protected_branches)
                and all(isinstance(b, str) and re.fullmatch(r"[A-Za-z0-9_/-]+", b)
                        for b in self.protected_branches), "configuration_invalid")

    @classmethod
    def from_env(cls, env: Mapping[str, str]):
        try:
            for key in ("IMPLEMENTER_APP_ID", "IMPLEMENTER_INSTALLATION_ID"):
                require(bool(re.fullmatch(r"[1-9][0-9]*", env[key])), "configuration_invalid")
            repos = json.loads(env["IMPLEMENTER_REPOSITORIES"], object_pairs_hook=_unique_object)
            require(type(repos) is dict, "configuration_invalid")
            validate_permissions(json.loads(env["IMPLEMENTER_PERMISSIONS"],
                                             object_pairs_hook=_unique_object))
            protected = json.loads(env["IMPLEMENTER_PROTECTED_BRANCHES"])
            require(type(protected) is list, "configuration_invalid")
            return cls(int(env["IMPLEMENTER_APP_ID"]), int(env["IMPLEMENTER_INSTALLATION_ID"]),
                       tuple(repos.items()), env["IMPLEMENTER_JWT_SOURCE"], tuple(protected))
        except Exception:
            raise BoundaryError("configuration_invalid") from None

    def repository_id(self, repository: str) -> int:
        for name, repo_id in self.repositories:
            if name == repository:
                return repo_id
        raise BoundaryError("repository_denied")


class EnvironmentJwtSource:
    """An operator supplies an already signed, short-lived App JWT, never a PAT."""

    def __call__(self, reference: str, app_id: int) -> str:
        require(reference.startswith("env:"), "secret_source_unavailable")
        value = os.environ.get(reference[4:])
        require(isinstance(value, str) and bool(value), "secret_source_unavailable")
        return value


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class GitHubTransport:
    """Trusted-only HTTPS adapter: fixed origin, no redirects/proxies/debug/retries."""

    def __init__(self, *, allow_network: bool = False):
        self._allow_network = allow_network
        self._opener = urllib.request.build_opener(
            urllib.request.ProxyHandler({}), _NoRedirect())

    def request(self, method: str, path: str, bearer: str, body=None):
        require(self._allow_network, "network_disabled")
        require(method in {"GET", "POST", "PATCH"} and path.startswith("/")
                and not path.startswith("//") and "?" not in path and "#" not in path,
                "transport_request_invalid")
        try:
            request = urllib.request.Request(
                "https://api.github.com" + path,
                data=None if body is None else json.dumps(body).encode(),
                headers={"Authorization": "Bearer " + bearer,
                         "Accept": "application/vnd.github+json",
                         "Content-Type": "application/json",
                         "X-GitHub-Api-Version": "2022-11-28",
                         "User-Agent": "puen-implementer-broker"}, method=method)
            with self._opener.open(request, timeout=20) as response:
                raw = response.read(8 * 1024 * 1024 + 1)
                require(len(raw) <= 8 * 1024 * 1024, "github_response_invalid")
                return json.loads(raw)
        except urllib.error.HTTPError as error:
            # Preserve only a status code, never response bodies, headers or URLs.
            raise BoundaryError("github_http_" + str(error.code)) from None
        except Exception:
            raise BoundaryError("github_transport_failed") from None


@dataclass(repr=False)
class _Session:
    _transport: object = field(repr=False)
    _token: str = field(repr=False)
    _expires_at: datetime
    _clock: Callable
    actor: str
    repository: str

    def request(self, method, suffix="", body=None):
        require(self._expires_at > self._clock() + timedelta(seconds=30), "token_expired")
        return self._transport.request(method, "/repos/" + self.repository + suffix,
                                       self._token, body)


class TokenBroker:
    def __init__(self, config: Config, transport, jwt_source: Callable,
                 clock=lambda: datetime.now(timezone.utc)):
        self._config, self._transport = config, transport
        self._jwt_source, self._clock = jwt_source, clock

    def _open(self, repository: str) -> _Session:
        """Internal capability, never an agent-facing token endpoint."""
        repo_id = self._config.repository_id(repository)
        try:
            jwt = self._jwt_source(self._config.jwt_source, self._config.app_id)
            require(isinstance(jwt, str) and bool(jwt), "app_authentication_invalid")
            app = self._transport.request("GET", "/app", jwt)
            require(app["id"] == self._config.app_id, "app_identity_invalid")
            slug = app["slug"]
            require(isinstance(slug, str) and bool(re.fullmatch(r"[a-z0-9-]+", slug)),
                    "app_identity_invalid")
            path = f"/app/installations/{self._config.installation_id}"
            installation = self._transport.request("GET", path, jwt)
            require(installation["id"] == self._config.installation_id
                    and installation["app_id"] == self._config.app_id
                    and installation["repository_selection"] == "selected"
                    and installation["suspended_at"] is None, "installation_invalid")
            validate_permissions(installation["permissions"])
            token = self._transport.request("POST", path + "/access_tokens", jwt,
                                            {"repository_ids": [repo_id],
                                             "permissions": dict(PERMISSIONS)})
            received = self._clock()
            validate_permissions(token["permissions"])
            require(token["repository_selection"] == "selected"
                    and len(token["repositories"]) == 1
                    and token["repositories"][0]["id"] == repo_id
                    and token["repositories"][0]["full_name"] == repository,
                    "token_repository_scope_invalid")
            expires = datetime.fromisoformat(token["expires_at"].replace("Z", "+00:00"))
            require(expires.tzinfo is not None and received + timedelta(seconds=30) < expires
                    <= received + timedelta(hours=1), "token_expiry_invalid")
            require(isinstance(token["token"], str) and bool(token["token"]),
                    "token_invalid")
            return _Session(self._transport, token["token"], expires, self._clock,
                            slug + "[bot]", repository)
        except BoundaryError:
            raise
        except Exception:
            raise BoundaryError("authentication_failed") from None
