package reviewer

import (
	"context"
	"sort"

	"github.com/puennaja/puen-github-apps/internal/reviewworker"
)

// workerEngine is not reachable through NewService. Production retains the
// unavailable engine until isolation and provider authorization are reviewed.
type workerEngine struct {
	adapter                 reviewworker.Adapter
	workItem, authorization string
}

func (w workerEngine) ID() string { return w.adapter.Provider() + "/" + w.adapter.Model() }

func (w workerEngine) Review(ctx context.Context, evidence Evidence) (ReviewResult, error) {
	req := reviewworker.Request{
		Version: reviewworker.Version, Repository: evidence.Repository, PRNumber: evidence.Number,
		HeadSHA: evidence.HeadSHA, BaseSHA: evidence.BaseSHA, WorkItem: w.workItem,
		Authorization: w.authorization, PRBody: evidence.PRBody,
		Limits: reviewworker.Limits{TimeoutMS: 30000, MemoryMiB: 512},
	}
	for _, f := range evidence.Files {
		req.Files = append(req.Files, reviewworker.File{Path: f.Path, Patch: f.Patch})
	}
	keys := make([]string, 0, len(evidence.Documents))
	for key := range evidence.Documents {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		req.Requirements = append(req.Requirements, reviewworker.Requirement{Path: key, Content: evidence.Documents[key]})
	}
	if err := req.Validate(); err != nil {
		return ReviewResult{}, reviewworker.ErrUnavailable
	}
	response, err := w.adapter.Review(ctx, req)
	if err != nil || response.Validate(req, w.adapter.Provider(), w.adapter.Model()) != nil {
		return ReviewResult{}, reviewworker.ErrUnavailable
	}
	result := ReviewResult{Complete: true}
	for _, f := range response.Findings {
		result.Findings = append(result.Findings, Finding{Severity: f.Severity, Path: f.Path, Line: f.Line, Message: f.Message, Evidence: f.Evidence})
	}
	return result, nil
}
