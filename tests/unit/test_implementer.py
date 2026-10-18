"""Synthetic offline contract tests. Never contact GitHub or issue real tokens."""

from copy import deepcopy
from datetime import datetime, timedelta, timezone
import io
import json
import logging
import os
from pathlib import Path
import sys
import traceback
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
sys.path[:0] = [str(ROOT / "packages/github-auth"), str(ROOT / "apps/implementer")]

from github_auth.boundary import (BoundaryError, Config, EnvironmentJwtSource,
                                  GitHubTransport, PERMISSIONS, TokenBroker)
from implementer.gateway import Authorization, ImplementerGateway
from implementer.host import JsonLineAudit, serve


NOW = datetime(2026, 10, 18, 12, tzinfo=timezone.utc)
REPO = "example/project"
BRANCH = "feature/GOV-001A-implementer-app"
HEAD, TREE, NEW_TREE, COMMIT = (c * 40 for c in "abcd")
JWT = "synthetic-app-jwt-not-a-credential"
TOKEN = "synthetic-installation-token-not-a-credential"
ACTOR = "example-implementer[bot]"


def config():
    return Config(11, 22, ((REPO, 33),), "secret://test/app-jwt")


def grant(**changes):
    args = dict(work_item="GOV-001A", provider="arbitrary-provider", tool="arbitrary-tool",
                authorization_ref="human-GOV-001A", repository=REPO, branch=BRANCH)
    args.update(changes)
    return Authorization(**args)


def pull_request():
    return {"number": 7, "user": {"login": ACTOR, "type": "Bot"}, "state": "open",
            "draft": True, "head": {"ref": BRANCH, "sha": HEAD,
                                       "repo": {"full_name": REPO}},
            "base": {"ref": "master", "repo": {"full_name": REPO}},
            "title": "Implementation", "body": "Evidence"}


class FakeGitHub:
    def __init__(self):
        self.calls = []
        self.app = {"id": 11, "slug": "example-implementer"}
        self.installation = {"id": 22, "app_id": 11, "repository_selection": "selected",
                             "suspended_at": None, "permissions": dict(PERMISSIONS)}
        self.token = {"token": TOKEN, "expires_at": (NOW + timedelta(hours=1)).isoformat(),
                      "permissions": dict(PERMISSIONS), "repository_selection": "selected",
                      "repositories": [{"id": 33, "full_name": REPO}]}
        self.pr = pull_request()
        self.exists = True
        self.protected_feature = False
        self.protected_base = True
        self.head = HEAD
        self.fail = None
        self.on_call = None

    def request(self, method, path, bearer, body=None):
        # Credentials are inspected only inside the synthetic trusted transport.
        self.calls.append((method, path, deepcopy(body)))
        if self.on_call:
            self.on_call(method, path)
        if self.fail:
            raise self.fail
        if path == "/app":
            assert bearer == JWT
            return deepcopy(self.app)
        if path == "/app/installations/22":
            assert bearer == JWT
            return deepcopy(self.installation)
        if path == "/app/installations/22/access_tokens":
            assert bearer == JWT
            assert body == {"repository_ids": [33], "permissions": PERMISSIONS}
            return deepcopy(self.token)
        assert bearer == TOKEN
        assert path.startswith("/repos/" + REPO)
        suffix = path[len("/repos/" + REPO):]
        if suffix == "":
            return {"id": 33, "full_name": REPO, "default_branch": "master", "token": TOKEN}
        if suffix == "/branches/master":
            return {"name": "master", "protected": self.protected_base,
                    "commit": {"sha": HEAD}}
        if suffix.startswith("/branches/feature%2F"):
            if not self.exists:
                raise BoundaryError("github_http_404")
            return {"name": BRANCH, "protected": self.protected_feature,
                    "commit": {"sha": self.head}}
        if suffix == "/git/commits/" + HEAD:
            return {"sha": HEAD, "tree": {"sha": TREE}}
        if suffix == "/git/trees":
            return {"sha": NEW_TREE}
        if suffix == "/git/commits":
            return {"sha": COMMIT}
        if suffix.startswith("/git/refs"):
            return {"ref": "refs/heads/" + BRANCH, "object": {"sha": COMMIT}}
        if suffix in {"/pulls", "/pulls/7"}:
            if method == "PATCH":
                self.pr.update(body)
            return deepcopy(self.pr)
        raise AssertionError("Unexpected fake API request")


class Harness(unittest.TestCase):
    def setUp(self):
        self.api = FakeGitHub()
        self.events = []
        self.time = NOW
        self.broker = TokenBroker(config(), self.api, lambda ref, app: JWT,
                                  clock=lambda: self.time)
        self.gateway = ImplementerGateway(config(), grant(), self.broker, self.events.append)

    def push(self, **changes):
        args = dict(expected_head=HEAD, changes={"src/example.py": "print('example')"},
                    message="[GOV-001A] feat: example")
        args.update(changes)
        return self.gateway.execute("pushFeatureBranch", **args)

    def create(self, **changes):
        args = dict(expected_head=HEAD, title="Implementation", body="Evidence")
        args.update(changes)
        return self.gateway.execute("createDraftPullRequest", **args)

    def assertDeniedBeforeAuth(self, operation, **payload):
        self.api.calls.clear()
        with self.assertRaises(BoundaryError):
            self.gateway.execute(operation, **payload)
        self.assertEqual(self.api.calls, [])
        self.assertEqual(self.events[-1]["outcome"], "FAILED")


class ConfigurationTests(Harness):
    def environment(self):
        return {"IMPLEMENTER_APP_ID": "11", "IMPLEMENTER_INSTALLATION_ID": "22",
                "IMPLEMENTER_REPOSITORIES": json.dumps({REPO: 33}),
                "IMPLEMENTER_PERMISSIONS": json.dumps(PERMISSIONS),
                "IMPLEMENTER_PROTECTED_BRANCHES": '["master","main"]',
                "IMPLEMENTER_JWT_SOURCE": "secret://test/app-jwt"}

    def test_valid_configuration_and_schema(self):
        self.assertEqual(Config.from_env(self.environment()), config())
        schema = json.loads((ROOT / "apps/implementer/config.schema.json").read_text())
        self.assertEqual(set(schema["required"]), set(self.environment()))

    def test_missing_configuration(self):
        for key in self.environment():
            with self.subTest(key=key), self.assertRaisesRegex(BoundaryError, "configuration_invalid"):
                env = self.environment()
                del env[key]
                Config.from_env(env)

    def test_invalid_configuration(self):
        cases = {"IMPLEMENTER_APP_ID": ["", "x", "0", "-1", " 11", "+11"],
                 "IMPLEMENTER_INSTALLATION_ID": ["0", "x"],
                 "IMPLEMENTER_REPOSITORIES": ["null", "[]", "{}", '{"../repo":1}',
                     '{"example/project":true}', '{"example/project":33,"EXAMPLE/project":34}',
                     '{"example/project":33,"other/project":33}',
                     '{"example/project":33,"example/project":34}'],
                 "IMPLEMENTER_PROTECTED_BRANCHES": ['["master"]', 'null', '["master","main",5]'],
                 "IMPLEMENTER_JWT_SOURCE": ["", TOKEN, "http://example.com", "env:bad name"],
                 "IMPLEMENTER_PERMISSIONS": ["{}", "null", '{"contents":"write"}']}
        for key, values in cases.items():
            for value in values:
                with self.subTest(key=key, value=value), self.assertRaises(BoundaryError):
                    Config.from_env(dict(self.environment(), **{key: value}))

    def test_unauthorized_repository(self):
        with self.assertRaisesRegex(BoundaryError, "repository_denied"):
            ImplementerGateway(config(), grant(repository="other/project"), self.broker,
                               self.events.append)
        self.assertEqual(self.api.calls, [])

    def test_unauthorized_branches(self):
        for branch in ["master", "main", "refs/heads/master", "feature/OTHER-1-work",
                       "feature/GOV-001A-../master", "feature/GOV-001A-work.lock",
                       "feature/GOV-001A-work/extra"]:
            with self.subTest(branch=branch), self.assertRaises(BoundaryError):
                grant(branch=branch)
        with self.assertRaises(BoundaryError):
            ImplementerGateway(config(), grant(base="unprotected"), self.broker, self.events.append)


class AuthenticationTests(Harness):
    def test_metadata_and_narrow_token_request(self):
        result = self.gateway.execute("getRepository")
        self.assertEqual(result, {"id": 33, "repository": REPO, "default_branch": "master",
                                  "actor": ACTOR})
        self.assertEqual(self.api.calls[2][2], {"repository_ids": [33], "permissions": PERMISSIONS})
        self.assertNotIn(TOKEN, repr(result))

    def test_invalid_installation_or_app(self):
        for field, value in [("app_id", 99), ("id", 99), ("suspended_at", "suspended"),
                             ("repository_selection", "all")]:
            with self.subTest(field=field):
                original = deepcopy(self.api.installation)
                self.api.installation[field] = value
                with self.assertRaises(BoundaryError):
                    self.gateway.execute("getRepository")
                self.api.installation = original
        self.api.app["id"] = 99
        with self.assertRaisesRegex(BoundaryError, "app_identity_invalid"):
            self.gateway.execute("getRepository")

    def test_permission_validation_for_installation_and_token(self):
        for target in [self.api.installation, self.api.token]:
            for permissions in [{}, dict(PERMISSIONS, workflows="write"),
                                dict(PERMISSIONS, administration="read"),
                                dict(PERMISSIONS, contents="read"),
                                dict(PERMISSIONS, actions="read")]:
                with self.subTest(permissions=permissions), self.assertRaisesRegex(
                        BoundaryError, "permission_scope_invalid"):
                    target["permissions"] = permissions
                    self.gateway.execute("getRepository")
            target["permissions"] = dict(PERMISSIONS)

    def test_invalid_token_repository_scope(self):
        for field, value in [("repositories", []),
                             ("repositories", [{"id": 99, "full_name": REPO}]),
                             ("repositories", [{"id": 33, "full_name": "other/project"}]),
                             ("repositories", self.api.token["repositories"] * 2),
                             ("repository_selection", "all")]:
            original = deepcopy(self.api.token)
            with self.subTest(field=field), self.assertRaises(BoundaryError):
                self.api.token[field] = value
                self.gateway.execute("getRepository")
            self.api.token = original

    def test_expired_near_expiry_and_overlong_tokens(self):
        for value in [NOW - timedelta(seconds=1), NOW + timedelta(seconds=30),
                      NOW + timedelta(hours=1, seconds=1), NOW.replace(tzinfo=None)]:
            with self.subTest(value=value), self.assertRaises(BoundaryError):
                self.api.token["expires_at"] = value.isoformat()
                self.gateway.execute("getRepository")
        self.assertFalse(any(p.startswith("/repos/") for _, p, _ in self.api.calls))

    def test_token_expiry_rechecked_before_each_request(self):
        def advance(method, path):
            if path.endswith("/git/trees"):
                self.time += timedelta(hours=1)
        self.api.on_call = advance
        with self.assertRaisesRegex(BoundaryError, "token_expired"):
            self.push()
        self.assertFalse(any(m == "PATCH" for m, _, _ in self.api.calls))

    def test_invalid_token_payloads(self):
        for value in [None, "", 123]:
            with self.subTest(value=value), self.assertRaises(BoundaryError):
                self.api.token["token"] = value
                self.gateway.execute("getRepository")

    def test_expiry_window_uses_receipt_time_including_request_latency(self):
        def advance(method, path):
            if path.endswith("/access_tokens"):
                self.time += timedelta(seconds=2)
                self.api.token["expires_at"] = (self.time + timedelta(hours=1)).isoformat()
        self.api.on_call = advance
        self.assertEqual(self.gateway.execute("getRepository")["actor"], ACTOR)

    def test_environment_source_missing_secret_and_external_source_failure(self):
        with patch.dict(os.environ, {}, clear=True), self.assertRaisesRegex(
                BoundaryError, "secret_source_unavailable"):
            EnvironmentJwtSource()("env:IMPLEMENTER_APP_JWT", 11)
        def bad_source(*args):
            raise ValueError(TOKEN)
        broker = TokenBroker(config(), self.api, bad_source, lambda: NOW)
        gateway = ImplementerGateway(config(), grant(), broker, self.events.append)
        with self.assertRaisesRegex(BoundaryError, "authentication_failed"):
            gateway.execute("getRepository")
        self.assertEqual(self.api.calls, [])


class OperationTests(Harness):
    def test_disallowed_operations_and_extra_arguments(self):
        for operation in ["mergePullRequest", "approvePullRequest", "request", "deleteBranch",
                          "changeSettings", "updateSecrets", "createReview", "markReady"]:
            self.assertDeniedBeforeAuth(operation)
        for extra in [{"repository": "other/project"}, {"branch": "master"},
                      {"url": "https://example.com"}, {"token": TOKEN}]:
            self.assertDeniedBeforeAuth("getRepository", **extra)
        self.assertDeniedBeforeAuth("createDraftPullRequest", expected_head=HEAD,
                                    title="title", body="body", draft=False)

    def test_workflow_changes_including_ancestor_and_traversal_denied(self):
        for path in [".github/workflows/test.yml", ".github/workflows", ".github",
                     ".GITHUB/WORKFLOWS/test.yml", "src/../.github/workflows/test.yml",
                     "/absolute", "src//x", ".git/config", "src/./x", "src\\x"]:
            for content in ["untrusted workflow", None]:
                self.assertDeniedBeforeAuth("pushFeatureBranch", expected_head=HEAD,
                                            changes={path: content}, message="example")

    def test_push_updates_only_feature_ref_with_constructed_commit(self):
        result = self.push(changes={"src/example.py": "example", "removed.txt": None})
        self.assertEqual(result["commit_sha"], COMMIT)
        mutations = [(m, p, b) for m, p, b in self.api.calls if m != "GET"]
        self.assertEqual(mutations[-1], ("PATCH", "/repos/" + REPO + "/git/refs/heads/" + BRANCH,
                                         {"sha": COMMIT, "force": False}))
        commit_body = next(b for m, p, b in mutations if p.endswith("/git/commits"))
        self.assertEqual(commit_body["parents"], [HEAD])
        tree_body = next(b for m, p, b in mutations if p.endswith("/git/trees"))
        self.assertEqual(tree_body["base_tree"], TREE)
        self.assertEqual(tree_body["tree"][1]["sha"], None)
        self.assertTrue(all(e["mode"] == "100644" for e in tree_body["tree"]))

    def test_push_creates_new_feature_ref_from_base(self):
        self.api.exists = False
        self.push()
        self.assertEqual(self.api.calls[-1], ("POST", "/repos/" + REPO + "/git/refs",
                                              {"ref": "refs/heads/" + BRANCH, "sha": COMMIT}))

    def test_protected_feature_and_unprotected_base(self):
        self.api.protected_feature = True
        with self.assertRaisesRegex(BoundaryError, "branch_denied"):
            self.push()
        self.api.protected_feature = False
        self.api.protected_base = False
        with self.assertRaisesRegex(BoundaryError, "base_unprotected"):
            self.create()

    def test_head_mismatch_and_concurrent_change(self):
        with self.assertRaisesRegex(BoundaryError, "head_changed"):
            self.push(expected_head=COMMIT)
        def advance(method, path):
            if path.endswith("/git/commits") and method == "POST":
                self.api.head = COMMIT
        self.api.on_call = advance
        with self.assertRaisesRegex(BoundaryError, "head_changed"):
            self.push()
        self.assertFalse(any("/git/refs" in p for _, p, _ in self.api.calls))

    def test_cannot_push_arbitrary_existing_commit_or_tree(self):
        self.assertDeniedBeforeAuth("pushFeatureBranch", expected_head=HEAD, changes={},
                                    message="example", sha=COMMIT)
        self.assertDeniedBeforeAuth("pushFeatureBranch", expected_head=HEAD,
                                    changes={"x": {"sha": COMMIT}}, message="example")

    def test_allowed_draft_pr_creation(self):
        result = self.create()
        self.assertTrue(result["draft"])
        self.assertEqual(result["number"], 7)
        self.assertEqual(self.api.calls[-1], ("POST", "/repos/" + REPO + "/pulls",
            {"head": BRANCH, "base": "master", "title": "Implementation", "body": "Evidence",
             "draft": True, "maintainer_can_modify": False}))

    def test_read_and_update_own_pr(self):
        read = self.gateway.execute("updateOwnPullRequest", number=7, changes={})
        self.assertEqual(read["title"], "Implementation")
        result = self.gateway.execute("updateOwnPullRequest", number=7,
                                      changes={"title": "Updated", "body": "New evidence"})
        self.assertEqual(result["title"], "Updated")
        self.assertEqual(self.api.calls[-1][2], {"title": "Updated", "body": "New evidence"})

    def test_ownership_draft_fork_base_and_branch_checks(self):
        cases = [("user", {"login": "human", "type": "User"}), ("draft", False),
                 ("state", "closed"), ("head", dict(self.api.pr["head"], ref="other")),
                 ("head", dict(self.api.pr["head"], repo={"full_name": "fork/project"})),
                 ("base", dict(self.api.pr["base"], ref="main")), ("number", 8)]
        for key, value in cases:
            self.api.pr = pull_request()
            self.api.pr[key] = value
            before = len(self.api.calls)
            with self.subTest(key=key), self.assertRaises(BoundaryError):
                self.gateway.execute("updateOwnPullRequest", number=7, changes={"title": "update"})
            self.assertFalse(any(m == "PATCH" for m, _, _ in self.api.calls[before:]))

    def test_update_cannot_close_retarget_merge_or_mark_ready(self):
        for changes in [{"state": "closed"}, {"base": "main"}, {"draft": False},
                        {"merge": True}, {"maintainer_can_modify": True}]:
            self.assertDeniedBeforeAuth("updateOwnPullRequest", number=7, changes=changes)


class AuditAndLeakageTests(Harness):
    def test_audit_metadata(self):
        self.create()
        event = self.events[-1]
        for key, value in {"work_item": "GOV-001A", "provider": "arbitrary-provider",
                           "tool": "arbitrary-tool", "authorization_ref": "human-GOV-001A",
                           "actor": ACTOR, "repository": REPO, "branch": BRANCH,
                           "commit_sha": HEAD, "pull_request": 7,
                           "operation": "createDraftPullRequest", "outcome": "PASSED"}.items():
            self.assertEqual(event[key], value)
        self.assertEqual(self.events[0]["outcome"], "STARTED")
        self.assertIn("timestamp", event)

    def test_audit_failure_blocks_before_authentication(self):
        def unavailable(event):
            raise RuntimeError(TOKEN)
        gateway = ImplementerGateway(config(), grant(), self.broker, unavailable)
        with self.assertRaisesRegex(BoundaryError, "audit_unavailable"):
            gateway.execute("getRepository")
        self.assertEqual(self.api.calls, [])

    def test_errors_and_logs_never_expose_credentials(self):
        stream = io.StringIO()
        handler = logging.StreamHandler(stream)
        logging.getLogger().addHandler(handler)
        try:
            for error in [RuntimeError(TOKEN + JWT), BoundaryError(TOKEN + JWT)]:
                self.api.fail = error
                try:
                    self.gateway.execute("getRepository")
                except BoundaryError as safe:
                    # Includes chained traceback rendering, not just str(error).
                    rendered = "".join(traceback.format_exception(safe))
                evidence = rendered + stream.getvalue() + json.dumps(self.events)
                self.assertNotIn(TOKEN, evidence)
                self.assertNotIn(JWT, evidence)
                self.assertNotIn(TOKEN, repr(self.broker))
        finally:
            logging.getLogger().removeHandler(handler)

    def test_denied_payload_not_logged_and_failure_not_success(self):
        self.assertDeniedBeforeAuth("getRepository", password=TOKEN)
        self.assertNotIn(TOKEN, json.dumps(self.events))
        self.assertIsNone(self.events[-1]["actor"])
        self.assertEqual(self.events[-1]["remote_effect"], "UNKNOWN")


class TransportTests(unittest.TestCase):
    def test_network_disabled_by_default(self):
        transport = GitHubTransport()
        with patch("urllib.request.OpenerDirector.open") as open_request:
            with self.assertRaisesRegex(BoundaryError, "network_disabled"):
                transport.request("GET", "/app", JWT)
            open_request.assert_not_called()

    def test_http_transport_with_mocked_socket_boundary(self):
        transport = GitHubTransport(allow_network=True)
        response = unittest.mock.MagicMock()
        response.__enter__.return_value.read.return_value = b'{"id":11}'
        with patch.object(transport._opener, "open", return_value=response) as send:
            self.assertEqual(transport.request("GET", "/app", JWT), {"id": 11})
            request = send.call_args.args[0]
            self.assertEqual(request.full_url, "https://api.github.com/app")
            self.assertEqual(request.headers["Authorization"], "Bearer " + JWT)

    def test_transport_sanitizes_http_and_network_errors(self):
        import urllib.error
        transport = GitHubTransport(allow_network=True)
        for error, code in [(urllib.error.HTTPError("https://api.github.com/" + TOKEN, 403,
                                                   TOKEN, {}, io.BytesIO(TOKEN.encode())),
                             "github_http_403"), (RuntimeError(TOKEN), "github_transport_failed")]:
            with patch.object(transport._opener, "open", side_effect=error):
                with self.assertRaisesRegex(BoundaryError, code) as caught:
                    transport.request("GET", "/app", JWT)
                self.assertNotIn(TOKEN, str(caught.exception))

    def test_redirect_handler_rejects_cross_origin_redirect(self):
        from github_auth.boundary import _NoRedirect
        import urllib.request
        handler = _NoRedirect()
        request = urllib.request.Request("https://api.github.com/app")
        self.assertIsNone(handler.redirect_request(request, None, 302, "redirect", {},
                                                  "https://example.com/collect"))

    def test_transport_rejects_arbitrary_endpoints_and_methods(self):
        transport = GitHubTransport(allow_network=True)
        for method, path in [("DELETE", "/repos/example/project"), ("GET", "//example.com"),
                             ("GET", "https://example.com"), ("GET", "/app?token=" + TOKEN)]:
            with patch.object(transport._opener, "open") as send:
                with self.assertRaises(BoundaryError):
                    transport.request(method, path, JWT)
                send.assert_not_called()


class HostTests(Harness):
    def test_stdio_only_returns_safe_results_or_codes(self):
        requests = [dict(operation="getRepository", arguments={}),
                    dict(operation="mergePullRequest", arguments={"token": TOKEN}),
                    dict(operation="getRepository", arguments={}, config=TOKEN)]
        source = io.StringIO("\n".join(json.dumps(r) for r in requests) + "\n" + TOKEN + "\n")
        target = io.StringIO()
        serve(self.gateway, source, target)
        responses = [json.loads(line) for line in target.getvalue().splitlines()]
        self.assertEqual(responses[0]["result"]["actor"], ACTOR)
        self.assertEqual(responses[1]["error"], "operation_denied")
        self.assertEqual(responses[2]["error"], "operation_arguments_invalid")
        self.assertEqual(responses[3]["error"], "request_invalid")
        self.assertNotIn(TOKEN, target.getvalue())

    def test_oversized_request_terminates_without_authentication(self):
        source = io.StringIO("x" * 1_048_577)
        target = io.StringIO()
        serve(self.gateway, source, target)
        self.assertEqual(json.loads(target.getvalue()), {"error": "request_invalid"})
        self.assertEqual(self.api.calls, [])

    def test_persistent_audit_is_private_and_rejects_symlinks(self):
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "audit.jsonl"
            sink = JsonLineAudit(path)
            try:
                sink({"outcome": "PASSED", "work_item": "GOV-001A"})
            finally:
                sink.close()
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(json.loads(path.read_text())["work_item"], "GOV-001A")
            linked = Path(directory) / "linked.jsonl"
            linked.symlink_to(path)
            with self.assertRaisesRegex(BoundaryError, "audit_unavailable"):
                JsonLineAudit(linked)
            path.chmod(0o644)
            with self.assertRaisesRegex(BoundaryError, "audit_unavailable"):
                JsonLineAudit(path)


if __name__ == "__main__":
    unittest.main()
