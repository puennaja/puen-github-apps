package implementer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testRepo = "example/project"
const testBranch = "feature/GOV-001A-implementer-app"
const testActor = "example-implementer[bot]"
const testToken = "synthetic-installation-token-not-a-credential"
const testJWT = "synthetic-app-jwt-not-a-credential"

var testNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
var headSHA = strings.Repeat("a", 40)
var treeSHA = strings.Repeat("b", 40)
var newTreeSHA = strings.Repeat("c", 40)
var madeSHA = strings.Repeat("d", 40)

func testConfig() Config {
	return Config{11, 22, map[string]int64{testRepo: 33}, []string{"master", "main"}, "secret://test/app-key"}
}
func testGrant() Authorization {
	return Authorization{"GOV-001A", "arbitrary-provider", "arbitrary-tool", "human-GOV-001A", testRepo, testBranch, "master"}
}
func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func encoded(v any) string      { b, _ := json.Marshal(v); return string(b) }
func put(out any, v any) error  { return json.Unmarshal(raw(v), out) }
func defaultPR() map[string]any {
	return map[string]any{"number": 7, "draft": true, "state": "open", "title": "Implementation", "body": "Evidence", "user": map[string]any{"login": testActor, "type": "Bot"}, "head": map[string]any{"ref": testBranch, "sha": headSHA, "repo": map[string]any{"full_name": testRepo}}, "base": map[string]any{"ref": "master", "repo": map[string]any{"full_name": testRepo}}}
}

type call struct{ method, path, body string }
type fixture struct {
	apiCalls                                       []call
	tokenCalls                                     []call
	now                                            time.Time
	appID, installationID                          int64
	selection                                      string
	suspendedAt                                    any
	installPermissions, tokenPermissions           map[string]string
	tokenRepos                                     []map[string]any
	expires                                        time.Time
	featureExists, featureProtected, baseProtected bool
	featureHead                                    string
	pr                                             map[string]any
	apiError, tokenError                           error
	advance                                        func(method, path string)
	issuedToken                                    string
	lastSession                                    *session
	events                                         []AuditEvent
	gateway                                        *Gateway
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{now: testNow, appID: 11, installationID: 22, selection: "selected", installPermissions: map[string]string{"metadata": "read", "contents": "write", "pull_requests": "write"}, tokenRepos: []map[string]any{{"id": 33, "full_name": testRepo}}, expires: testNow.Add(time.Hour), featureExists: true, baseProtected: true, featureHead: headSHA, pr: defaultPR()}
	b := &Broker{config: testConfig(), app: f.app, tokenClient: f.makeToken, now: func() time.Time { return f.now }}
	g, err := NewGateway(testConfig(), testGrant(), b, AuditFunc(func(e AuditEvent) error { f.events = append(f.events, e); return nil }))
	if err != nil {
		t.Fatal(err)
	}
	f.gateway = g
	return f
}
func (f *fixture) app(ctx context.Context, method, path string, body, out any) error {
	f.apiCalls = append(f.apiCalls, call{method, path, encoded(body)})
	if f.advance != nil {
		f.advance(method, path)
	}
	if f.apiError != nil {
		return f.apiError
	}
	switch path {
	case "app":
		return put(out, map[string]any{"id": f.appID, "slug": "example-implementer"})
	case "app/installations/22":
		return put(out, map[string]any{"id": f.installationID, "app_id": f.appID, "repository_selection": f.selection, "suspended_at": f.suspendedAt, "permissions": f.installPermissions})
	case "app/installations/22/access_tokens":
		f.issuedToken = testToken
		var requested struct {
			Permissions map[string]string `json:"permissions"`
		}
		data, _ := json.Marshal(body)
		json.Unmarshal(data, &requested)
		permissions := f.tokenPermissions
		if permissions == nil {
			permissions = requested.Permissions
		}
		return put(out, map[string]any{"token": testToken, "expires_at": f.expires.Format(time.RFC3339), "repository_selection": f.selection, "repositories": f.tokenRepos, "permissions": permissions})
	}
	return errors.New("unexpected app API")
}
func (f *fixture) makeToken(token string) (requester, error) {
	f.issuedToken = token
	return f.repo, nil
}
func (f *fixture) repo(ctx context.Context, method, path string, body, out any) error {
	f.tokenCalls = append(f.tokenCalls, call{method, path, encoded(body)})
	if f.advance != nil {
		f.advance(method, path)
	}
	if f.tokenError != nil {
		return f.tokenError
	}
	suffix := strings.TrimPrefix(path, "repos/"+testRepo)
	if suffix == path {
		return errors.New("unexpected repository")
	}
	switch {
	case suffix == "":
		return put(out, map[string]any{"id": 33, "full_name": testRepo, "default_branch": "master", "token": testToken})
	case suffix == "/branches/master":
		return put(out, map[string]any{"name": "master", "protected": f.baseProtected, "commit": map[string]any{"sha": headSHA}})
	case strings.HasPrefix(suffix, "/branches/feature%2F"):
		if !f.featureExists {
			return fail("github_http_404")
		}
		return put(out, map[string]any{"name": testBranch, "protected": f.featureProtected, "commit": map[string]any{"sha": f.featureHead}})
	case suffix == "/git/commits/"+headSHA:
		return put(out, map[string]any{"sha": headSHA, "tree": map[string]any{"sha": treeSHA}})
	case suffix == "/git/trees":
		return put(out, map[string]any{"sha": newTreeSHA})
	case suffix == "/git/commits":
		return put(out, map[string]any{"sha": madeSHA})
	case strings.HasPrefix(suffix, "/git/refs"):
		return put(out, map[string]any{"ref": "refs/heads/" + testBranch, "object": map[string]any{"sha": madeSHA}})
	case suffix == "/pulls" || suffix == "/pulls/7":
		if method == "PATCH" {
			var changes map[string]any
			data, _ := json.Marshal(body)
			json.Unmarshal(data, &changes)
			for key, value := range changes {
				f.pr[key] = value
			}
		}
		return put(out, f.pr)
	}
	return errors.New("unexpected scoped API")
}
func (f *fixture) execute(op string, args any) (any, error) {
	return f.gateway.Execute(context.Background(), op, raw(args))
}
func (f *fixture) push() (any, error) {
	return f.execute("pushFeatureBranch", map[string]any{"expected_head": headSHA, "changes": map[string]any{"src/example.go": "package example"}, "message": "[GOV-001A] feat: example"})
}
func (f *fixture) create() (any, error) {
	return f.execute("createDraftPullRequest", map[string]any{"expected_head": headSHA, "title": "Implementation", "body": "Evidence"})
}
func wantErr(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil || err.Error() != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestConfigValidation(t *testing.T) {
	env := map[string]string{"IMPLEMENTER_APP_ID": "11", "IMPLEMENTER_INSTALLATION_ID": "22", "IMPLEMENTER_REPOSITORIES": `{"example/project":33}`, "IMPLEMENTER_PERMISSIONS": `{"metadata":"read","contents":"write","pull_requests":"write"}`, "IMPLEMENTER_PROTECTED_BRANCHES": `["master","main"]`, "IMPLEMENTER_PRIVATE_KEY_SOURCE": "secret://test/app-key"}
	if _, err := ConfigFromEnv(env); err != nil {
		t.Fatal(err)
	}
	for key := range env {
		copy := map[string]string{}
		for k, v := range env {
			copy[k] = v
		}
		delete(copy, key)
		if _, err := ConfigFromEnv(copy); err == nil {
			t.Fatalf("missing %s accepted", key)
		}
	}
	cases := map[string][]string{"IMPLEMENTER_APP_ID": {"0", "-1", " 11", "+11", "x"}, "IMPLEMENTER_INSTALLATION_ID": {"0", "x"}, "IMPLEMENTER_REPOSITORIES": {`{}`, `{"../repo":1}`, `{"example/project":true}`, `{"example/project":33,"EXAMPLE/project":34}`, `{"example/project":33,"other/project":33}`, `{"example/project":33,"example/project":34}`, `{"example/project":33}junk`}, "IMPLEMENTER_PERMISSIONS": {`{}`, `{"metadata":"read","contents":"write","pull_requests":"write","workflows":"write"}`, `{"metadata":"read","contents":"read","pull_requests":"write"}`}, "IMPLEMENTER_PROTECTED_BRANCHES": {`["master"]`, `null`, `["master","main",5]`}, "IMPLEMENTER_PRIVATE_KEY_SOURCE": {"", testToken, "http://example.com", "env:bad name"}}
	for key, values := range cases {
		for _, value := range values {
			copy := map[string]string{}
			for k, v := range env {
				copy[k] = v
			}
			copy[key] = value
			if _, err := ConfigFromEnv(copy); err == nil {
				t.Fatalf("invalid %s accepted", key)
			}
		}
	}
}
func TestAuthorizationDenied(t *testing.T) {
	c := testConfig()
	a := testGrant()
	a.Repository = "other/project"
	wantErr(t, a.Validate(c), "repository_denied")
	for _, branch := range []string{"master", "main", "refs/heads/master", "feature/OTHER-1-work", "feature/GOV-001A-../master", "feature/GOV-001A-work.lock", "feature/GOV-001A-work/extra"} {
		a = testGrant()
		a.Branch = branch
		if a.Validate(c) == nil {
			t.Fatalf("branch %q accepted", branch)
		}
	}
	a = testGrant()
	a.Base = "unprotected"
	wantErr(t, a.Validate(c), "branch_denied")
}
func TestScopeAndIdentity(t *testing.T) {
	f := newFixture(t)
	result, err := f.execute("getRepository", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["actor"] != testActor || strings.Contains(encoded(result), testToken) {
		t.Fatal("actor or secret projection invalid")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(f.apiCalls[2].body), &body); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body["repository_ids"], []any{float64(33)}) || !reflect.DeepEqual(body["permissions"], map[string]any{"metadata": "read"}) {
		t.Fatalf("scope request: %v", body)
	}
}

func TestPerOperationTokenPermissions(t *testing.T) {
	cases := []struct {
		name string
		run  func(*fixture) (any, error)
		want map[string]string
	}{
		{"repository read", func(f *fixture) (any, error) { return f.execute("getRepository", map[string]any{}) }, map[string]string{"metadata": "read"}},
		{"feature push", func(f *fixture) (any, error) { return f.push() }, map[string]string{"metadata": "read", "contents": "write"}},
		{"draft create", func(f *fixture) (any, error) { return f.create() }, map[string]string{"metadata": "read", "contents": "read", "pull_requests": "write"}},
		{"own PR read", func(f *fixture) (any, error) {
			return f.execute("updateOwnPullRequest", map[string]any{"number": 7, "changes": map[string]any{}})
		}, map[string]string{"metadata": "read", "pull_requests": "read"}},
		{"own PR edit", func(f *fixture) (any, error) {
			return f.execute("updateOwnPullRequest", map[string]any{"number": 7, "changes": map[string]any{"title": "Updated"}})
		}, map[string]string{"metadata": "read", "contents": "read", "pull_requests": "write"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if _, err := tc.run(f); err != nil {
				t.Fatal(err)
			}
			var body struct {
				RepositoryIDs []int64           `json:"repository_ids"`
				Permissions   map[string]string `json:"permissions"`
			}
			if err := json.Unmarshal([]byte(f.apiCalls[2].body), &body); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(body.RepositoryIDs, []int64{33}) || !reflect.DeepEqual(body.Permissions, tc.want) {
				t.Fatalf("unexpected token scope: %+v", body)
			}
		})
	}
}

func TestIdentityAndTokenScopeRejections(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*fixture)
	}{
		{"app id", func(f *fixture) { f.appID = 99 }}, {"installation id", func(f *fixture) { f.installationID = 99 }},
		{"selection", func(f *fixture) { f.selection = "all" }}, {"no repositories", func(f *fixture) { f.tokenRepos = nil }},
		{"suspended", func(f *fixture) { f.suspendedAt = "2026-10-18T12:00:00Z" }},
		{"wrong repository", func(f *fixture) { f.tokenRepos = []map[string]any{{"id": 99, "full_name": testRepo}} }},
		{"extra repository", func(f *fixture) {
			f.tokenRepos = append(f.tokenRepos, map[string]any{"id": 44, "full_name": "other/project"})
		}},
		{"extra install permission", func(f *fixture) { f.installPermissions["workflows"] = "write" }},
		{"missing install permission", func(f *fixture) { delete(f.installPermissions, "contents") }},
		{"extra token permission", func(f *fixture) { f.tokenPermissions = map[string]string{"metadata": "read", "administration": "read"} }},
		{"missing token permission", func(f *fixture) { f.tokenPermissions = map[string]string{"contents": "read"} }},
		{"expired", func(f *fixture) { f.expires = f.now.Add(-time.Second) }},
		{"near expiry", func(f *fixture) { f.expires = f.now.Add(30 * time.Second) }},
		{"too long", func(f *fixture) { f.expires = f.now.Add(time.Hour + time.Second) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.mutate(f)
			if _, err := f.execute("getRepository", map[string]any{}); err == nil {
				t.Fatal("scope or identity accepted")
			}
			if len(f.tokenCalls) != 0 {
				t.Fatal("repository API reached")
			}
		})
	}
}
func TestExpiryBeforeEachRequest(t *testing.T) {
	f := newFixture(t)
	f.advance = func(method, path string) {
		if path == "repos/"+testRepo+"/git/trees" {
			f.now = f.now.Add(time.Hour)
		}
	}
	_, err := f.push()
	wantErr(t, err, "token_expired")
	for _, call := range f.tokenCalls {
		if call.method == "PATCH" {
			t.Fatal("ref updated after expiry")
		}
	}
}
func TestTokenExpiryReceiptTime(t *testing.T) {
	f := newFixture(t)
	f.advance = func(method, path string) {
		if path == "app/installations/22/access_tokens" {
			f.now = f.now.Add(2 * time.Second)
			f.expires = f.now.Add(time.Hour)
		}
	}
	if _, err := f.execute("getRepository", map[string]any{}); err != nil {
		t.Fatal(err)
	}
}

func TestOperationAllowlistAndPayloads(t *testing.T) {
	f := newFixture(t)
	for _, op := range []string{"mergePullRequest", "approvePullRequest", "request", "deleteBranch", "changeSettings", "updateSecrets", "createReview", "markReady"} {
		_, err := f.execute(op, map[string]any{})
		wantErr(t, err, "operation_denied")
	}
	for _, args := range []any{map[string]any{"repository": "other/project"}, map[string]any{"branch": "master"}, map[string]any{"token": testToken}} {
		_, err := f.execute("getRepository", args)
		wantErr(t, err, "operation_arguments_invalid")
	}
	_, err := f.execute("createDraftPullRequest", map[string]any{"expected_head": headSHA, "title": "title", "body": "body", "draft": false})
	wantErr(t, err, "operation_arguments_invalid")
	if len(f.apiCalls) != 0 {
		t.Fatal("API reached for rejected request")
	}
}
func TestWorkflowAndPathDenials(t *testing.T) {
	for _, path := range []string{".github/workflows/test.yml", ".github/workflows", ".github", ".GITHUB/WORKFLOWS/test.yml", "src/../.github/workflows/test.yml", "/absolute", "src//x", ".git/config", "src/./x", "src\\x"} {
		for _, value := range []any{"untrusted", nil} {
			f := newFixture(t)
			_, err := f.execute("pushFeatureBranch", map[string]any{"expected_head": headSHA, "changes": map[string]any{path: value}, "message": "example"})
			if err == nil || len(f.apiCalls) != 0 {
				t.Fatalf("path %q accepted", path)
			}
		}
	}
}
func TestArbitraryCommitInjectionDenied(t *testing.T) {
	f := newFixture(t)
	_, err := f.execute("pushFeatureBranch", map[string]any{"expected_head": headSHA, "changes": map[string]any{}, "message": "example", "sha": madeSHA})
	wantErr(t, err, "operation_arguments_invalid")
	_, err = f.execute("pushFeatureBranch", map[string]any{"expected_head": headSHA, "changes": map[string]any{"file": map[string]any{"sha": madeSHA}}, "message": "example"})
	wantErr(t, err, "operation_arguments_invalid")
}

func TestConstructedFeaturePush(t *testing.T) {
	f := newFixture(t)
	result, err := f.execute("pushFeatureBranch", map[string]any{"expected_head": headSHA, "changes": map[string]any{"src/example.go": "package example", "removed.txt": nil}, "message": "[GOV-001A] feat: example"})
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["commit_sha"] != madeSHA {
		t.Fatal("wrong commit")
	}
	last := f.tokenCalls[len(f.tokenCalls)-1]
	if last.method != "PATCH" || last.path != "repos/"+testRepo+"/git/refs/heads/"+testBranch || !strings.Contains(last.body, `"force":false`) {
		t.Fatalf("ref request: %+v", last)
	}
	for _, call := range f.tokenCalls {
		if call.path == "repos/"+testRepo+"/git/trees" {
			var body map[string]any
			json.Unmarshal([]byte(call.body), &body)
			if body["base_tree"] != treeSHA {
				t.Fatal("wrong base tree")
			}
			found := false
			for _, entry := range body["tree"].([]any) {
				e := entry.(map[string]any)
				if e["path"] == "removed.txt" {
					_, has := e["sha"]
					if !has || e["sha"] != nil {
						t.Fatal("deletion must use null SHA")
					}
					found = true
				}
				if e["mode"] != "100644" {
					t.Fatal("non-regular mode")
				}
			}
			if !found {
				t.Fatal("missing deletion")
			}
		}
	}
}
func TestNewFeatureRef(t *testing.T) {
	f := newFixture(t)
	f.featureExists = false
	if _, err := f.push(); err != nil {
		t.Fatal(err)
	}
	last := f.tokenCalls[len(f.tokenCalls)-1]
	if last.method != "POST" || last.path != "repos/"+testRepo+"/git/refs" {
		t.Fatalf("wrong new ref: %+v", last)
	}
}
func TestProtectedAndConcurrentBranches(t *testing.T) {
	f := newFixture(t)
	f.featureProtected = true
	_, err := f.push()
	wantErr(t, err, "branch_denied")
	f = newFixture(t)
	f.baseProtected = false
	_, err = f.create()
	wantErr(t, err, "base_unprotected")
	f = newFixture(t)
	f.advance = func(method, path string) {
		if method == "POST" && path == "repos/"+testRepo+"/git/commits" {
			f.featureHead = madeSHA
		}
	}
	_, err = f.push()
	wantErr(t, err, "head_changed")
	for _, call := range f.tokenCalls {
		if strings.Contains(call.path, "/git/refs") {
			t.Fatal("updated changed ref")
		}
	}
}

func TestDraftPRAndOwnUpdates(t *testing.T) {
	f := newFixture(t)
	result, err := f.create()
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["draft"] != true {
		t.Fatal("not draft")
	}
	last := f.tokenCalls[len(f.tokenCalls)-1]
	if last.method != "POST" || !strings.Contains(last.body, `"draft":true`) {
		t.Fatalf("wrong create: %+v", last)
	}
	result, err = f.execute("updateOwnPullRequest", map[string]any{"number": 7, "changes": map[string]any{}})
	if err != nil || result.(map[string]any)["title"] != "Implementation" {
		t.Fatal("own PR read failed", err)
	}
	result, err = f.execute("updateOwnPullRequest", map[string]any{"number": 7, "changes": map[string]any{"title": "Updated", "body": "New evidence"}})
	if err != nil || result.(map[string]any)["title"] != "Updated" {
		t.Fatal("own PR update failed", err)
	}
}
func TestOwnPRRestrictions(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"human author", func(p map[string]any) { p["user"] = map[string]any{"login": "human", "type": "User"} }},
		{"ready", func(p map[string]any) { p["draft"] = false }}, {"closed", func(p map[string]any) { p["state"] = "closed" }},
		{"other head", func(p map[string]any) { p["head"].(map[string]any)["ref"] = "other" }},
		{"fork", func(p map[string]any) {
			p["head"].(map[string]any)["repo"] = map[string]any{"full_name": "fork/project"}
		}},
		{"other base", func(p map[string]any) { p["base"].(map[string]any)["ref"] = "main" }},
		{"other number", func(p map[string]any) { p["number"] = 8 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.mutate(f.pr)
			_, err := f.execute("updateOwnPullRequest", map[string]any{"number": 7, "changes": map[string]any{"title": "update"}})
			if err == nil {
				t.Fatal("foreign PR accepted")
			}
			for _, call := range f.tokenCalls {
				if call.method == "PATCH" {
					t.Fatal("patched foreign PR")
				}
			}
		})
	}
	for _, changes := range []map[string]any{{"state": "closed"}, {"base": "main"}, {"draft": false}, {"merge": true}, {"maintainer_can_modify": true}} {
		f := newFixture(t)
		_, err := f.execute("updateOwnPullRequest", map[string]any{"number": 7, "changes": changes})
		if err == nil || (err.Error() != "pr_changes_denied" && err.Error() != "operation_arguments_invalid") {
			t.Fatalf("disallowed PR edit accepted: %v", err)
		}
		if len(f.apiCalls) != 0 {
			t.Fatal("API reached")
		}
	}
}
func TestAuditAndLeakage(t *testing.T) {
	f := newFixture(t)
	if _, err := f.create(); err != nil {
		t.Fatal(err)
	}
	event := f.events[len(f.events)-1]
	if event.WorkItem != "GOV-001A" || event.Provider != "arbitrary-provider" || event.Tool != "arbitrary-tool" || event.Actor != testActor || event.Repository != testRepo || event.Branch != testBranch || event.CommitSHA != headSHA || event.PullRequest != 7 || event.Operation != "createDraftPullRequest" || event.Outcome != "PASSED" || event.RemoteEffect != "APPLIED" {
		t.Fatalf("incomplete audit event: %+v", event)
	}
	if f.events[0].Outcome != "STARTED" || event.Timestamp == "" {
		t.Fatal("missing audit start/timestamp")
	}
	f = newFixture(t)
	f.apiError = errors.New(testToken + testJWT)
	_, err := f.execute("getRepository", map[string]any{})
	if err == nil || strings.Contains(err.Error(), testToken) || strings.Contains(encoded(f.events), testToken) {
		t.Fatal("credential leaked")
	}
	f = newFixture(t)
	f.gateway.audit = AuditFunc(func(AuditEvent) error { return errors.New(testToken) })
	_, err = f.execute("getRepository", map[string]any{})
	wantErr(t, err, "audit_unavailable")
	if len(f.apiCalls) != 0 {
		t.Fatal("API reached before audit")
	}
}
func TestInvalidPayloadAndDeniedAudit(t *testing.T) {
	f := newFixture(t)
	_, err := f.execute("getRepository", map[string]any{"password": testToken})
	wantErr(t, err, "operation_arguments_invalid")
	if strings.Contains(encoded(f.events), testToken) {
		t.Fatal("payload logged")
	}
	_, err = f.execute("mergePullRequest", map[string]any{"password": testToken})
	wantErr(t, err, "operation_denied")
	if f.events[len(f.events)-1].Operation != "denied" {
		t.Fatal("untrusted op logged")
	}
}
func TestGuardedTransport(t *testing.T) {
	called := false
	next := roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: 302, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	req, _ := http.NewRequest("GET", "https://api.github.com/app", nil)
	if _, err := (guardedTransport{next: next, enabled: false}).RoundTrip(req); err == nil || called {
		t.Fatal("network default allowed")
	}
	if _, err := (guardedTransport{next: next, enabled: true}).RoundTrip(req); err == nil || !called {
		t.Fatal("redirect allowed")
	}
	for _, target := range []string{"http://api.github.com/app", "https://example.com/app", "https://api.github.com/app?token=" + testToken} {
		req, _ := http.NewRequest("GET", target, nil)
		if _, err := (guardedTransport{next: next, enabled: true}).RoundTrip(req); err == nil {
			t.Fatal("unsafe target allowed")
		}
	}
}

func TestTransportResponseIsBounded(t *testing.T) {
	next := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 9*1024*1024)))}, nil
	})
	req, _ := http.NewRequest("GET", "https://api.github.com/app", nil)
	resp, err := (guardedTransport{next: next, enabled: true}).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	read, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || len(read) != 8*1024*1024 {
		t.Fatalf("response limit: %d, %v", len(read), err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestHostAndAuditFile(t *testing.T) {
	f := newFixture(t)
	input := strings.NewReader(`{"operation":"getRepository","arguments":{}}` + "\n" + `{"operation":"mergePullRequest","arguments":{"token":"` + testToken + `"}}` + "\n" + `{"operation":"getRepository","arguments":{},"config":"` + testToken + `"}` + "\n")
	var output bytes.Buffer
	if err := Serve(context.Background(), f.gateway, input, &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], testActor) || !strings.Contains(lines[1], "operation_denied") || !strings.Contains(lines[2], "request_invalid") || strings.Contains(output.String(), testToken) {
		t.Fatalf("unsafe host output: %s", output.String())
	}
	var oversized bytes.Buffer
	Serve(context.Background(), f.gateway, strings.NewReader(strings.Repeat("x", 1_048_577)), &oversized)
	if !strings.Contains(oversized.String(), "request_invalid") {
		t.Fatal("oversized input accepted")
	}
	largeRequest := raw(map[string]any{"operation": "createDraftPullRequest", "arguments": map[string]any{"expected_head": headSHA, "title": "Implementation", "body": strings.Repeat("e", 8192)}})
	var largeOutput bytes.Buffer
	if err := Serve(context.Background(), newFixture(t).gateway, bytes.NewReader(append(largeRequest, '\n')), &largeOutput); err != nil || !strings.Contains(largeOutput.String(), `"result"`) {
		t.Fatalf("bounded request over 4K failed: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	audit, err := OpenJSONLAudit(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.Append(f.events[0]); err != nil {
		t.Fatal(err)
	}
	audit.Close()
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("audit file not private")
	}
	linked := filepath.Join(dir, "linked.jsonl")
	if err := os.Symlink(path, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJSONLAudit(linked); err == nil {
		t.Fatal("symlink allowed")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJSONLAudit(path); err == nil {
		t.Fatal("public audit allowed")
	}
}

func TestFrameworkKeyErrorsAreSanitized(t *testing.T) {
	_, err := NewFrameworkBroker(testConfig(), nil, false)
	wantErr(t, err, "secret_source_unavailable")
	_, err = NewFrameworkBroker(testConfig(), []byte(testToken), false)
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatal("invalid key error leaked or accepted")
	}
}
