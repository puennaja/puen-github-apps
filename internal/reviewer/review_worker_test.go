package reviewer

import (
	"context"
	"errors"
	"testing"

	"github.com/puennaja/puen-github-apps/internal/reviewworker"
)

type fixtureAdapter struct {
	response reviewworker.Response
	err      error
}

func (fixtureAdapter) Provider() string { return "synthetic" }
func (fixtureAdapter) Model() string    { return "fixture" }
func (a fixtureAdapter) Review(context.Context, reviewworker.Request) (reviewworker.Response, error) {
	return a.response, a.err
}

func TestWorkerBridgeRejectsStaleAndIncomplete(t *testing.T) {
	head := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	base := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	e := Evidence{Repository: "puennaja/puen-github-apps", Number: 3, HeadSHA: head, BaseSHA: base,
		Files: []DiffFile{{Path: "safe.go", Patch: "@@ -0,0 +1 @@\n+unsafe()\n"}}, Documents: map[string]string{"AGENTS.md": "human approval required"}}
	good := reviewworker.Response{Version: reviewworker.Version, Complete: true, ReviewedSHA: head, Provider: "synthetic", Model: "fixture",
		Findings: []reviewworker.Finding{{Severity: "BLOCKER", Path: "safe.go", Line: 1, Message: "Unsafe call", Evidence: "Added line 1"}}}
	w := workerEngine{adapter: fixtureAdapter{response: good}, workItem: "GOV-003B", authorization: "owner/GOV-003B"}
	r, err := w.Review(context.Background(), e)
	if err != nil || !r.Complete || len(r.Findings) != 1 {
		t.Fatalf("valid bridge: %v", err)
	}
	bad := good
	bad.ReviewedSHA = base
	w.adapter = fixtureAdapter{response: bad}
	if _, err = w.Review(context.Background(), e); !errors.Is(err, reviewworker.ErrUnavailable) {
		t.Fatal("stale result accepted")
	}
	bad = good
	bad.Complete = false
	w.adapter = fixtureAdapter{response: bad}
	if _, err = w.Review(context.Background(), e); !errors.Is(err, reviewworker.ErrUnavailable) {
		t.Fatal("incomplete result accepted")
	}
	w.adapter = fixtureAdapter{err: errors.New("synthetic-secret")}
	if _, err = w.Review(context.Background(), e); !errors.Is(err, reviewworker.ErrUnavailable) || err.Error() == "synthetic-secret" {
		t.Fatal("engine error escaped")
	}
	// The only exported production constructor still installs UnavailableEngine.
	s, err := NewService(testConfig(), &fakeSource{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.engine.(UnavailableEngine); !ok {
		t.Fatal("real review engine activated")
	}
}
