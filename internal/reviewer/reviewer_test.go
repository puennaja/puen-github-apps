package reviewer

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testSecret = "synthetic-webhook-secret-000000000000"
const shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const baseSHA = "cccccccccccccccccccccccccccccccccccccccc"

func testConfig() Config { return Config{7, 11, map[string]int64{"owner/repo": 17}, testSecret} }

type fakeSource struct {
	mu      sync.Mutex
	session *fakeSession
	openErr error
	opens   int
}

func (f *fakeSource) Open(context.Context, Job) (Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens++
	if f.openErr != nil {
		return nil, f.openErr
	}
	return f.session, nil
}

type fakeSession struct {
	mu                         sync.Mutex
	pr                         PullRequest
	files                      []DiffFile
	docs                       map[string]string
	refs                       []string
	reports                    []Report
	getErr, fileErr, reportErr error
	gets                       int
}

func (f *fakeSession) GetPullRequest(context.Context, int) (PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	return f.pr, f.getErr
}
func (f *fakeSession) ListFiles(context.Context, int) ([]DiffFile, error) { return f.files, f.fileErr }
func (f *fakeSession) ReadContext(_ context.Context, path, ref string) (string, bool, error) {
	f.refs = append(f.refs, ref)
	value, ok := f.docs[path]
	return value, ok, nil
}
func (f *fakeSession) PublishCheck(_ context.Context, r Report) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reportErr != nil {
		return f.reportErr
	}
	f.reports = append(f.reports, r)
	return nil
}

type fakeEngine struct {
	result ReviewResult
	err    error
	calls  int
}

func (e *fakeEngine) ID() string { return "synthetic-test-engine" }
func (e *fakeEngine) Review(_ context.Context, _ Evidence) (ReviewResult, error) {
	e.calls++
	return e.result, e.err
}

func fixtureSession() *fakeSession {
	return &fakeSession{pr: PullRequest{Number: 1, HeadSHA: shaA, BaseSHA: baseSHA, BaseRef: "master", State: "open", Body: "synthetic evidence"},
		files: []DiffFile{{"main.go", "@@ -0,0 +1,2 @@\n+first\n+second"}},
		docs:  map[string]string{"AGENTS.md": "synthetic requirements", "tests/README.md": "synthetic test evidence"}}
}
func newTestService(t *testing.T, capacity int, engine Engine, session *fakeSession) (*Service, *fakeSource) {
	t.Helper()
	source := &fakeSource{session: session}
	s, err := newServiceWithEngine(testConfig(), source, engine, capacity)
	if err != nil {
		t.Fatal(err)
	}
	return s, source
}
func payload(action, sha string, draft bool, repoID, installation int64) []byte {
	data, _ := json.Marshal(map[string]any{"action": action,
		"installation": map[string]any{"id": installation},
		"repository":   map[string]any{"id": repoID, "full_name": "owner/repo"},
		"pull_request": map[string]any{"number": 1, "draft": draft, "head": map[string]any{"sha": sha}, "base": map[string]any{"sha": baseSHA, "ref": "master"}}})
	return data
}
func sendWebhook(s *Service, delivery, event string, body []byte, signed bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-GitHub-Event", event)
	r.Header.Set("X-GitHub-Delivery", delivery)
	if signed {
		mac := hmac.New(sha256.New, []byte(testSecret))
		mac.Write(body)
		r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestWebhookSignatureActionsAndAllowlists(t *testing.T) {
	s, _ := newTestService(t, 2, UnavailableEngine{}, fixtureSession())
	valid := payload("opened", shaA, false, 17, 11)
	if got := sendWebhook(s, "missing", "pull_request", valid, false).Code; got != 400 {
		t.Fatalf("missing signature: %d", got)
	}
	if got := sendWebhook(s, "bad", "pull_request", []byte(`{"bad":true}`), true).Code; got != 400 {
		t.Fatalf("malformed payload: %d", got)
	}
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte("different"))
	r := httptest.NewRequest("POST", "/webhook", strings.NewReader(string(valid)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-GitHub-Delivery", "wrong")
	r.Header.Set("X-GitHub-Event", "pull_request")
	r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("invalid hmac: %d", w.Code)
	}
	for _, tc := range []struct {
		id, event string
		body      []byte
		want      int
	}{
		{"other-event", "issues", valid, 202},
		{"unsupported", "pull_request", payload("closed", shaA, false, 17, 11), 202},
		{"repo", "pull_request", payload("opened", shaA, false, 18, 11), 403},
		{"installation", "pull_request", payload("opened", shaA, false, 17, 12), 403},
		{"base", "pull_request", []byte(strings.Replace(string(valid), `"master"`, `"other"`, 1)), 403},
		{"draft", "pull_request", payload("opened", shaA, true, 17, 11), 202},
		{"valid", "pull_request", valid, 202},
	} {
		if got := sendWebhook(s, tc.id, tc.event, tc.body, true).Code; got != tc.want {
			t.Errorf("%s: %d", tc.id, got)
		}
	}
	if got := sendWebhook(s, "valid", "pull_request", valid, true).Code; got != 202 {
		t.Fatalf("duplicate: %d", got)
	}
	if len(s.queue) != 1 {
		t.Fatalf("expected one queued review, got %d", len(s.queue))
	}
}

func TestQueueOverflowAndRetry(t *testing.T) {
	s, _ := newTestService(t, 1, UnavailableEngine{}, fixtureSession())
	if sendWebhook(s, "one", "pull_request", payload("opened", shaA, false, 17, 11), true).Code != 202 {
		t.Fatal("first enqueue")
	}
	if sendWebhook(s, "two", "pull_request", payload("synchronize", shaB, false, 17, 11), true).Code != 503 {
		t.Fatal("queue should reject")
	}
	if !s.ProcessOne(context.Background()) {
		t.Fatal("missing job")
	}
	if sendWebhook(s, "two", "pull_request", payload("synchronize", shaB, false, 17, 11), true).Code != 202 {
		t.Fatal("redelivery not accepted")
	}
}

func TestDraftReadyAndStaleSuppression(t *testing.T) {
	engine := &fakeEngine{result: ReviewResult{Complete: true}}
	f := fixtureSession()
	s, _ := newTestService(t, 3, engine, f)
	sendWebhook(s, "first", "pull_request", payload("opened", shaA, true, 17, 11), true)
	if s.ProcessOne(context.Background()) {
		t.Fatal("draft reviewed")
	}
	sendWebhook(s, "ready", "pull_request", payload("ready_for_review", shaA, false, 17, 11), true)
	sendWebhook(s, "newhead", "pull_request", payload("synchronize", shaB, false, 17, 11), true)
	f.pr.HeadSHA = shaB
	s.ProcessOne(context.Background())
	s.ProcessOne(context.Background())
	if engine.calls != 1 || len(f.reports) != 1 || f.reports[0].HeadSHA != shaB || f.reports[0].Status != "READY_FOR_HUMAN_REVIEW" {
		t.Fatalf("stale result published: %+v", f.reports)
	}
	for _, ref := range f.refs {
		if ref != baseSHA {
			t.Fatalf("requirements read from untrusted head: %s", ref)
		}
	}
}

func TestFindingsAndHumanOnlyBoundary(t *testing.T) {
	for _, tc := range []struct{ severity, want string }{{"BLOCKER", "BLOCKED"}, {"MAJOR", "BLOCKED"}, {"MINOR", "READY_FOR_HUMAN_REVIEW"}} {
		t.Run(tc.severity, func(t *testing.T) {
			f := fixtureSession()
			e := &fakeEngine{result: ReviewResult{Complete: true, Findings: []Finding{{tc.severity, "main.go", 2, "Synthetic finding", "Synthetic added line"}}}}
			s, _ := newTestService(t, 1, e, f)
			sendWebhook(s, tc.severity, "pull_request", payload("opened", shaA, false, 17, 11), true)
			s.ProcessOne(context.Background())
			if len(f.reports) != 1 || f.reports[0].Status != tc.want {
				t.Fatalf("report: %+v", f.reports)
			}
			body := f.reports[0].checkBody()
			encoded, _ := json.Marshal(body)
			if strings.Contains(string(encoded), "APPROVE") || strings.Contains(string(encoded), "merge") {
				t.Fatal("human-only operation appeared")
			}
			if body["head_sha"] != shaA {
				t.Fatal("wrong checked SHA")
			}
		})
	}
}

func TestUnavailableMissingEvidenceAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*fakeSession)
		engine Engine
		want   string
	}{
		{"unavailable", func(*fakeSession) {}, UnavailableEngine{}, "REVIEW_UNAVAILABLE"},
		{"limited-analysis", func(*fakeSession) {}, &fakeEngine{result: ReviewResult{Complete: true, Limitation: "incomplete context"}}, "REVIEW_UNAVAILABLE"},
		{"missing-agents", func(f *fakeSession) { delete(f.docs, "AGENTS.md") }, &fakeEngine{result: ReviewResult{Complete: true}}, "REVIEW_UNAVAILABLE"},
		{"missing-patch", func(f *fakeSession) { f.files[0].Patch = "" }, &fakeEngine{result: ReviewResult{Complete: true}}, "REVIEW_UNAVAILABLE"},
		{"api-failure", func(f *fakeSession) { f.fileErr = errors.New("synthetic-private-token") }, &fakeEngine{result: ReviewResult{Complete: true}}, "REVIEW_UNAVAILABLE"},
		{"bad-finding", func(*fakeSession) {}, &fakeEngine{result: ReviewResult{Complete: true, Findings: []Finding{{"MAJOR", "main.go", 999, "bad", "bad"}}}}, "REVIEW_UNAVAILABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixtureSession()
			tc.mutate(f)
			s, source := newTestService(t, 1, tc.engine, f)
			sendWebhook(s, tc.name, "pull_request", payload("opened", shaA, false, 17, 11), true)
			s.ProcessOne(context.Background())
			if len(f.reports) != 1 || f.reports[0].Status != tc.want || source.opens > 4 {
				t.Fatalf("status/retries: %+v, opens %d", f.reports, source.opens)
			}
			if got := f.reports[0].checkBody()["conclusion"]; got != "action_required" {
				t.Fatalf("unavailable check could satisfy a required check: %v", got)
			}
			encoded, _ := json.Marshal(f.reports)
			if strings.Contains(string(encoded), "synthetic-private-token") {
				t.Fatal("credential leak")
			}
		})
	}
	f := fixtureSession()
	f.reportErr = errors.New("synthetic-private-token")
	s, _ := newTestService(t, 1, UnavailableEngine{}, f)
	sendWebhook(s, "report-fail", "pull_request", payload("opened", shaA, false, 17, 11), true)
	s.ProcessOne(context.Background())
	if s.Status("owner/repo", 1) != "REPORT_FAILED" {
		t.Fatal("report failure not visible")
	}
	f = fixtureSession()
	f.pr.BaseRef = "other"
	s, _ = newTestService(t, 1, UnavailableEngine{}, f)
	sendWebhook(s, "base-changed", "pull_request", payload("opened", shaA, false, 17, 11), true)
	s.ProcessOne(context.Background())
	if s.Status("owner/repo", 1) != "POLICY_DENIED" || len(f.reports) != 0 {
		t.Fatal("base policy failure was reported as review")
	}
}

func TestScopeAndExpiryFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		scope   map[string]string
		expiry  time.Duration
		repoID  int64
		wantErr bool
	}{
		{"exact", requiredPermissions, 40 * time.Minute, 17, false},
		{"wider", map[string]string{"metadata": "read", "contents": "write", "pull_requests": "read", "checks": "write"}, 40 * time.Minute, 17, true},
		{"expired", requiredPermissions, -time.Minute, 17, true},
		{"wrong-repo", requiredPermissions, 40 * time.Minute, 18, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testConfig()
			now := time.Now().UTC().Truncate(time.Second)
			secret := "synthetic-private-token"
			f := &FrameworkSource{config: c, now: func() time.Time { return now }, tokenClient: func(token string) (requestFunc, error) {
				if token != secret {
					t.Fatal("wrong token")
				}
				return func(context.Context, string, string, any, any) error { return nil }, nil
			}}
			f.app = func(_ context.Context, method, path string, body, out any) error {
				var fixture any
				switch path {
				case "app":
					fixture = map[string]any{"id": 7, "slug": "puen-reviewer"}
				case "app/installations/11":
					fixture = map[string]any{"id": 11, "app_id": 7, "repository_selection": "selected", "permissions": requiredPermissions}
				case "app/installations/11/access_tokens":
					if method != "POST" {
						t.Fatal("wrong verb")
					}
					b, _ := json.Marshal(body)
					if !strings.Contains(string(b), `"repository_ids":[17]`) {
						t.Fatal("unscoped token request")
					}
					fixture = map[string]any{"token": secret, "expires_at": now.Add(tc.expiry).Format(time.RFC3339), "repository_selection": "selected", "permissions": tc.scope, "repositories": []map[string]any{{"id": tc.repoID, "full_name": "owner/repo"}}}
				default:
					return fmt.Errorf("unexpected path")
				}
				b, _ := json.Marshal(fixture)
				return json.Unmarshal(b, out)
			}
			_, err := f.Open(context.Background(), Job{"owner/repo", 17, 11, 1, shaA, "delivery"})
			if (err != nil) != tc.wantErr {
				t.Fatalf("open error=%v", err)
			}
			if err != nil && strings.Contains(err.Error(), secret) {
				t.Fatal("token leaked")
			}
		})
	}
}

func TestInvalidConfigAndRequestBounds(t *testing.T) {
	c := testConfig()
	c.WebhookSecret = ""
	if _, err := NewService(c, &fakeSource{}, 1); err == nil {
		t.Fatal("accepted missing secret")
	}
	c = testConfig()
	c.Repositories = map[string]int64{"owner/repo": 17, "other/repo": 17}
	if c.Validate() == nil {
		t.Fatal("accepted duplicate repo ID")
	}
	s, _ := newTestService(t, 1, UnavailableEngine{}, fixtureSession())
	big := make([]byte, maxWebhookBytes+1)
	if sendWebhook(s, "big", "pull_request", big, true).Code != 400 {
		t.Fatal("oversize accepted")
	}
	if _, err := NewFrameworkSource(testConfig(), nil, false); err == nil {
		t.Fatal("accepted missing App key")
	}
	called := false
	transport := boundedTransport{next: roundTripFunc(func(*http.Request) (*http.Response, error) { called = true; return nil, nil }), enabled: false}
	request := httptest.NewRequest("GET", "https://api.github.com/app", nil)
	if _, err := transport.RoundTrip(request); err == nil || called {
		t.Fatal("network guard did not fail closed")
	}
}

func TestListFilesPaginationBoundary(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		first, second        int
		secondFails          bool
		wantCount, wantCalls int
		wantError            bool
	}{
		{"below-limit", 99, 0, false, 99, 1, false},
		{"exact-limit", 100, 0, false, 100, 2, false},
		{"one-over", 100, 1, false, 0, 2, true},
		{"two-hundred", 100, 100, false, 0, 2, true},
		{"second-page-fails", 100, 0, true, 0, 2, true},
		{"oversize-response", 101, 0, false, 0, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			session := &reviewerSession{repository: "owner/repo", expires: time.Now().Add(40 * time.Minute), now: time.Now}
			session.api = func(_ context.Context, method, path string, _, out any) error {
				calls++
				if method != "GET" {
					t.Fatalf("unexpected verb %s", method)
				}
				count := 0
				switch path {
				case "repos/owner/repo/pulls/1/files?per_page=100&page=1":
					count = tc.first
				case "repos/owner/repo/pulls/1/files?per_page=100&page=2":
					if tc.secondFails {
						return errors.New("synthetic page failure")
					}
					count = tc.second
				default:
					t.Fatalf("unexpected pagination path %s", path)
				}
				items := make([]map[string]string, count)
				for i := range items {
					items[i] = map[string]string{"filename": fmt.Sprintf("file-%03d.go", i), "patch": "@@ -0,0 +1 @@\n+x"}
				}
				encoded, _ := json.Marshal(items)
				return json.Unmarshal(encoded, out)
			}
			files, err := session.ListFiles(context.Background(), 1)
			if len(files) != tc.wantCount || calls != tc.wantCalls || (err != nil) != tc.wantError {
				t.Fatalf("files=%d calls=%d err=%v", len(files), calls, err)
			}
		})
	}
}

func TestProductionConstructorCannotDeclareReady(t *testing.T) {
	f := fixtureSession()
	s, err := NewService(testConfig(), &fakeSource{session: f}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if code := sendWebhook(s, "production", "pull_request", payload("opened", shaA, false, 17, 11), true).Code; code != 202 {
		t.Fatalf("webhook %d", code)
	}
	if !s.ProcessOne(context.Background()) {
		t.Fatal("review was not processed")
	}
	if len(f.reports) != 1 || f.reports[0].Status != "REVIEW_UNAVAILABLE" || f.reports[0].checkBody()["conclusion"] != "action_required" {
		t.Fatalf("production service claimed AI readiness: %+v", f.reports)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
