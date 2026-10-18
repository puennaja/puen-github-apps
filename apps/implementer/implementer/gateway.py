"""Four-operation interface; instantiated only by a trusted, isolated broker host."""

from collections.abc import Callable
from dataclasses import dataclass
from datetime import datetime, timezone
import re
from urllib.parse import quote

from github_auth.boundary import BoundaryError, Config, TokenBroker, require


SHA = re.compile(r"[0-9a-f]{40}\Z")
LABEL = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,99}\Z")


@dataclass(frozen=True)
class Authorization:
    """Human-approved scope, supplied by the host, never by the operation caller."""

    work_item: str
    provider: str
    tool: str
    authorization_ref: str
    repository: str
    branch: str
    base: str = "master"

    def __post_init__(self):
        for value in (self.work_item, self.provider, self.tool, self.authorization_ref):
            require(isinstance(value, str) and bool(LABEL.fullmatch(value)),
                    "authorization_invalid")
        require(isinstance(self.branch, str)
                and bool(re.fullmatch(r"feature/" + re.escape(self.work_item)
                                      + r"-[A-Za-z0-9][A-Za-z0-9_-]{0,99}", self.branch)),
                "branch_denied")


class ImplementerGateway:
    OPERATIONS = frozenset({"getRepository", "pushFeatureBranch", "createDraftPullRequest",
                            "updateOwnPullRequest"})

    def __init__(self, config: Config, authorization: Authorization, broker: TokenBroker,
                 audit: Callable[[dict], None]):
        config.repository_id(authorization.repository)
        require(authorization.branch not in config.protected_branches
                and authorization.base in config.protected_branches, "branch_denied")
        self._config, self._grant, self._broker, self._audit = config, authorization, broker, audit

    def execute(self, operation: str, **payload) -> dict:
        # Only fixed operation labels and trusted grant metadata enter the audit log.
        event = {"work_item": self._grant.work_item, "provider": self._grant.provider,
                 "tool": self._grant.tool, "authorization_ref": self._grant.authorization_ref,
                 "actor": None, "repository": self._grant.repository,
                 "branch": self._grant.branch, "commit_sha": None, "pull_request": None,
                 "operation": operation if isinstance(operation, str)
                 and operation in self.OPERATIONS else "denied",
                 "remote_effect": "UNKNOWN"}
        self._record(event, "STARTED")  # Audit failure prevents any GitHub request.
        result = None
        code = None
        try:
            require(isinstance(operation, str) and operation in self.OPERATIONS,
                    "operation_denied")
            self._validate(operation, payload)
            if operation == "updateOwnPullRequest":
                event["pull_request"] = payload["number"]
            session = self._broker._open(self._grant.repository)
            event["actor"] = session.actor
            result = getattr(self, "_" + operation)(session, payload, event)
            event["remote_effect"] = ("APPLIED" if operation in {
                "pushFeatureBranch", "createDraftPullRequest"} or
                (operation == "updateOwnPullRequest" and payload["changes"]) else "NONE")
        except BoundaryError as error:
            # Error codes originate only in this trusted implementation.
            code = str(error)
        except Exception:
            code = "operation_failed"
        # A failed/timeout write can have reached GitHub: never imply rollback.
        self._record(event, "PASSED" if code is None else "FAILED", code)
        if code is not None:
            raise BoundaryError(code) from None
        return result

    def _record(self, event, outcome, code=None):
        try:
            self._audit(dict(event, timestamp=datetime.now(timezone.utc).isoformat(),
                             outcome=outcome, error_code=code))
        except Exception:
            raise BoundaryError("audit_unavailable") from None

    def _validate(self, operation, payload):
        keys = {"getRepository": set(),
                "pushFeatureBranch": {"expected_head", "changes", "message"},
                "createDraftPullRequest": {"expected_head", "title", "body"},
                "updateOwnPullRequest": {"number", "changes"}}
        require(set(payload) == keys[operation], "operation_arguments_invalid")
        if "expected_head" in payload:
            require(isinstance(payload["expected_head"], str)
                    and bool(SHA.fullmatch(payload["expected_head"])), "commit_invalid")
        if operation == "pushFeatureBranch":
            self._text(payload["message"], 1000)
            changes = payload["changes"]
            require(type(changes) is dict and 0 < len(changes) <= 100, "changes_invalid")
            for path, content in changes.items():
                require(isinstance(path, str) and 0 < len(path) <= 240
                        and all(re.fullmatch(r"[A-Za-z0-9_.-]+", part)
                                and part not in ("", ".", "..")
                                and part.lower() != ".git" for part in path.split("/")),
                        "path_denied")
                normalized = path.lower()
                require(normalized not in {".github", ".github/workflows"}
                        and not normalized.startswith(".github/workflows/"), "workflow_denied")
                require(content is None or isinstance(content, str), "changes_invalid")
                if content is not None:
                    self._text(content, 100_000, empty=True)
        if operation == "createDraftPullRequest":
            self._text(payload["title"], 256)
            self._text(payload["body"], 60_000, empty=True)
        if operation == "updateOwnPullRequest":
            require(type(payload["number"]) is int and payload["number"] > 0, "pr_invalid")
            changes = payload["changes"]
            require(type(changes) is dict and set(changes) <= {"title", "body"},
                    "pr_changes_denied")
            for key, value in changes.items():
                self._text(value, 256 if key == "title" else 60_000, empty=key == "body")

    @staticmethod
    def _text(value, limit, empty=False):
        require(isinstance(value, str) and (empty or bool(value.strip()))
                and len(value.encode("utf-8")) <= limit, "text_invalid")

    def _getRepository(self, session, payload, event):
        repo = session.request("GET")
        require(repo["id"] == self._config.repository_id(self._grant.repository)
                and repo["full_name"] == self._grant.repository, "repository_identity_invalid")
        return {"id": repo["id"], "repository": repo["full_name"],
                "default_branch": repo["default_branch"], "actor": session.actor}

    def _branch(self, session, branch):
        result = session.request("GET", "/branches/" + quote(branch, safe=""))
        require(result["name"] == branch, "branch_identity_invalid")
        require(isinstance(result["commit"]["sha"], str)
                and bool(SHA.fullmatch(result["commit"]["sha"])), "commit_invalid")
        return result

    def _base(self, session):
        branch = self._branch(session, self._grant.base)
        require(branch["protected"] is True, "base_unprotected")
        return branch

    def _feature(self, session):
        branch = self._branch(session, self._grant.branch)
        require(branch["protected"] is False, "branch_denied")
        return branch

    def _pushFeatureBranch(self, session, payload, event):
        base = self._base(session)
        new_branch = False
        try:
            branch = self._feature(session)
        except BoundaryError as error:
            if str(error) != "github_http_404":
                raise
            branch, new_branch = base, True
        head = branch["commit"]["sha"]
        require(head == payload["expected_head"], "head_changed")
        commit = session.request("GET", "/git/commits/" + head)
        require(commit["sha"] == head and bool(SHA.fullmatch(commit["tree"]["sha"])),
                "commit_invalid")
        tree = []
        for path, content in payload["changes"].items():
            entry = {"path": path, "mode": "100644", "type": "blob"}
            entry.update({"sha": None} if content is None else {"content": content})
            tree.append(entry)
        created_tree = session.request("POST", "/git/trees",
                                       {"base_tree": commit["tree"]["sha"], "tree": tree})
        require(bool(SHA.fullmatch(created_tree["sha"])), "commit_invalid")
        created_commit = session.request("POST", "/git/commits",
                                        {"message": payload["message"],
                                         "tree": created_tree["sha"], "parents": [head]})
        sha = created_commit["sha"]
        require(bool(SHA.fullmatch(sha)), "commit_invalid")
        event["commit_sha"] = sha
        # Recheck protected status and head before the ref write. force=False also
        # rejects a divergent concurrent update at GitHub (no automatic retries).
        self._base(session)
        if not new_branch:
            require(self._feature(session)["commit"]["sha"] == head, "head_changed")
        if new_branch:
            ref = session.request("POST", "/git/refs",
                                  {"ref": "refs/heads/" + self._grant.branch, "sha": sha})
        else:
            ref = session.request("PATCH", "/git/refs/heads/" + self._grant.branch,
                                  {"sha": sha, "force": False})
        require(ref["ref"] == "refs/heads/" + self._grant.branch
                and ref["object"]["sha"] == sha, "ref_response_invalid")
        return {"branch": self._grant.branch, "commit_sha": sha, "actor": session.actor}

    def _own(self, pr, session):
        require(pr["user"]["login"] == session.actor and pr["user"]["type"] == "Bot"
                and pr["state"] == "open" and pr["draft"] is True
                and pr["head"]["ref"] == self._grant.branch
                and pr["base"]["ref"] == self._grant.base
                and pr["head"]["repo"]["full_name"] == self._grant.repository
                and pr["base"]["repo"]["full_name"] == self._grant.repository,
                "pr_not_owned_draft")
        require(bool(SHA.fullmatch(pr["head"]["sha"])) and type(pr["number"]) is int
                and pr["number"] > 0, "pr_invalid")

    @staticmethod
    def _observe_pr(pr, event):
        # Capture safe identifiers even if a write response fails later validation.
        if type(pr) is dict:
            if type(pr.get("number")) is int and pr["number"] > 0:
                event["pull_request"] = pr["number"]
            head = pr.get("head")
            if type(head) is dict and isinstance(head.get("sha"), str) and SHA.fullmatch(head["sha"]):
                event["commit_sha"] = head["sha"]

    @staticmethod
    def _pr_result(pr, session, event):
        event.update(commit_sha=pr["head"]["sha"], pull_request=pr["number"])
        return {"number": pr["number"], "draft": pr["draft"], "commit_sha": pr["head"]["sha"],
                "actor": session.actor, "title": pr["title"], "body": pr["body"]}

    def _createDraftPullRequest(self, session, payload, event):
        self._base(session)
        feature = self._feature(session)
        require(feature["commit"]["sha"] == payload["expected_head"], "head_changed")
        pr = session.request("POST", "/pulls",
                             {"head": self._grant.branch, "base": self._grant.base,
                              "title": payload["title"], "body": payload["body"],
                              "draft": True, "maintainer_can_modify": False})
        self._observe_pr(pr, event)
        self._own(pr, session)
        require(pr["head"]["sha"] == payload["expected_head"], "head_changed")
        return self._pr_result(pr, session, event)

    def _updateOwnPullRequest(self, session, payload, event):
        path = "/pulls/" + str(payload["number"])
        pr = session.request("GET", path)
        self._observe_pr(pr, event)
        self._own(pr, session)
        require(pr["number"] == payload["number"], "pr_invalid")
        self._pr_result(pr, session, event)
        if payload["changes"]:
            self._base(session)
            self._feature(session)
            pr = session.request("PATCH", path, payload["changes"])
            self._observe_pr(pr, event)
            self._own(pr, session)
            require(pr["number"] == payload["number"], "pr_invalid")
        return self._pr_result(pr, session, event)
