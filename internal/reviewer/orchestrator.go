package reviewer

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	maxFiles       = 100
	maxPatch       = 64 << 10
	maxDiff        = 512 << 10
	maxContextFile = 32 << 10
	maxContext     = 256 << 10
)

var errStaleReview = errors.New("stale_or_draft")
var errReviewPolicy = errors.New("review_policy_denied")
var errReportUncertain = errors.New("review_report_effect_unknown")

func itoa(v int) string { return strconv.Itoa(v) }

type PullRequest struct {
	Number  int
	HeadSHA string
	BaseSHA string
	BaseRef string
	Draft   bool
	State   string
	Body    string
}
type DiffFile struct{ Path, Patch string }
type Evidence struct {
	Repository string
	Number     int
	HeadSHA    string
	BaseSHA    string
	PRBody     string
	Files      []DiffFile
	Documents  map[string]string
}
type Finding struct {
	Severity, Path    string
	Line              int
	Message, Evidence string
}
type ReviewResult struct {
	Complete   bool
	Findings   []Finding
	Limitation string
}
type Report struct {
	Status, Engine, HeadSHA, Summary string
	Findings                         []Finding
}

// Engine receives bounded, inert source material. It never receives clients or credentials.
type Engine interface {
	ID() string
	Review(context.Context, Evidence) (ReviewResult, error)
}
type UnavailableEngine struct{}

func (UnavailableEngine) ID() string { return "unavailable" }
func (UnavailableEngine) Review(context.Context, Evidence) (ReviewResult, error) {
	return ReviewResult{Limitation: "No approved isolated AI review engine is configured"}, nil
}

// Session and Source remain inside the credential-bearing process. The engine sees neither.
type Session interface {
	GetPullRequest(context.Context, int) (PullRequest, error)
	ListFiles(context.Context, int) ([]DiffFile, error)
	ReadContext(context.Context, string, string) (string, bool, error)
	PublishCheck(context.Context, Report) error
}
type Source interface {
	Open(context.Context, Job) (Session, error)
}

var hunk = regexp.MustCompile(`^@@ -[0-9]+(?:,[0-9]+)? \+([0-9]+)(?:,([0-9]+))? @@`)

func addedLines(patch string) map[int]bool {
	lines := map[int]bool{}
	line, active := 0, false
	for _, row := range strings.Split(patch, "\n") {
		if match := hunk.FindStringSubmatch(row); match != nil {
			line, _ = strconv.Atoi(match[1])
			active = true
			continue
		}
		if !active || len(row) == 0 {
			continue
		}
		switch row[0] {
		case '+':
			lines[line] = true
			line++
		case ' ':
			line++
		case '-':
		default:
			active = false
		}
	}
	return lines
}

func validateResult(r ReviewResult, e Evidence) error {
	if !r.Complete || len(r.Findings) > 20 {
		return errors.New("review_incomplete")
	}
	paths := map[string]map[int]bool{}
	for _, file := range e.Files {
		paths[file.Path] = addedLines(file.Patch)
	}
	for _, f := range r.Findings {
		if f.Severity != "BLOCKER" && f.Severity != "MAJOR" && f.Severity != "MINOR" {
			return errors.New("review_invalid")
		}
		if len(f.Message) == 0 || len(f.Message) > 500 || len(f.Evidence) == 0 || len(f.Evidence) > 1000 {
			return errors.New("review_invalid")
		}
		if f.Path != "" {
			valid, ok := paths[f.Path]
			if !ok || (f.Line != 0 && !valid[f.Line]) {
				return errors.New("review_invalid")
			}
		} else if f.Line != 0 {
			return errors.New("review_invalid")
		}
	}
	return nil
}

func contextPaths(files []DiffFile) []string {
	paths := map[string]bool{"AGENTS.md": true, "docs/architecture/overview.md": true, "tests/README.md": true}
	for _, f := range files {
		if strings.HasSuffix(f.Path, ".md") &&
			(strings.HasPrefix(f.Path, "docs/specs/") || strings.HasPrefix(f.Path, "docs/decisions/") || strings.HasPrefix(f.Path, "docs/architecture/")) {
			paths[f.Path] = true
		}
	}
	ordered := make([]string, 0, len(paths))
	for p := range paths {
		ordered = append(ordered, p)
	}
	sort.Strings(ordered)
	if len(ordered) > 12 {
		ordered = ordered[:12]
	}
	return ordered
}

func (s *Service) collect(ctx context.Context, api Session, j Job) (Evidence, error) {
	pr, err := api.GetPullRequest(ctx, j.Number)
	if err != nil {
		return Evidence{}, err
	}
	if pr.Number != j.Number || strings.ToLower(pr.HeadSHA) != j.HeadSHA || pr.Draft || pr.State != "open" {
		return Evidence{}, errStaleReview
	}
	if !shaName.MatchString(pr.BaseSHA) || pr.BaseRef != "master" {
		return Evidence{}, errReviewPolicy
	}
	if len(pr.Body) > 32<<10 {
		return Evidence{}, errors.New("review_context_limit")
	}
	files, err := api.ListFiles(ctx, j.Number)
	if err != nil {
		return Evidence{}, err
	}
	if len(files) == 0 || len(files) > maxFiles {
		return Evidence{}, errors.New("review_diff_invalid")
	}
	e := Evidence{Repository: j.Repository, Number: j.Number, HeadSHA: j.HeadSHA, BaseSHA: strings.ToLower(pr.BaseSHA), PRBody: pr.Body, Files: files, Documents: map[string]string{}}
	total := 0
	for _, file := range files {
		if !validPath(file.Path) || len(file.Patch) == 0 || len(file.Patch) > maxPatch || !strings.HasPrefix(file.Patch, "@@ ") {
			return Evidence{}, errors.New("review_diff_invalid")
		}
		total += len(file.Patch)
		if total > maxDiff {
			return Evidence{}, errors.New("review_context_limit")
		}
	}
	total = 0
	for _, path := range contextPaths(files) {
		content, present, err := api.ReadContext(ctx, path, e.BaseSHA)
		if err != nil {
			return Evidence{}, err
		}
		if path == "AGENTS.md" && !present {
			return Evidence{}, errors.New("review_evidence_missing")
		}
		if !present {
			continue
		}
		if len(content) > maxContextFile {
			return Evidence{}, errors.New("review_context_limit")
		}
		total += len(content)
		if total > maxContext {
			return Evidence{}, errors.New("review_context_limit")
		}
		e.Documents[path] = content
	}
	return e, nil
}

func (s *Service) review(ctx context.Context, j Job) error {
	if !s.current(j) {
		return errStaleReview
	}
	api, err := s.source.Open(ctx, j)
	if err != nil {
		return err
	}
	e, err := s.collect(ctx, api, j)
	if err != nil {
		return err
	}
	r, err := s.engine.Review(ctx, e)
	if err != nil {
		return err
	}
	status := "REVIEW_UNAVAILABLE"
	summary := "Review unavailable or incomplete; human review readiness is not established."
	if r.Complete && r.Limitation == "" {
		if err := validateResult(r, e); err != nil {
			return err
		}
		status = "READY_FOR_HUMAN_REVIEW"
		summary = "No BLOCKER or MAJOR findings in completed analysis. Human approval is still required."
		for _, finding := range r.Findings {
			if finding.Severity == "BLOCKER" || finding.Severity == "MAJOR" {
				status = "BLOCKED"
				summary = "Blocking findings require rework and a new review."
				break
			}
		}
	}
	if len(r.Limitation) > 500 {
		return errors.New("review_invalid")
	}
	if status == "REVIEW_UNAVAILABLE" {
		r.Findings = nil
	}
	if r.Limitation != "" {
		summary += " Limitation: " + r.Limitation
	}
	return s.publishCurrent(ctx, api, j, Report{Status: status, Engine: s.engine.ID(), HeadSHA: j.HeadSHA, Summary: summary, Findings: r.Findings})
}

func (s *Service) publishCurrent(ctx context.Context, api Session, j Job, report Report) error {
	if !s.current(j) {
		return errors.New("stale")
	}
	pr, err := api.GetPullRequest(ctx, j.Number)
	if err != nil {
		return err
	}
	if pr.Number != j.Number || strings.ToLower(pr.HeadSHA) != j.HeadSHA || pr.Draft || pr.State != "open" {
		return errStaleReview
	}
	if pr.BaseRef != "master" {
		return errReviewPolicy
	}
	if !s.current(j) {
		return errStaleReview
	}
	if err := api.PublishCheck(ctx, report); err != nil {
		return errReportUncertain
	}
	s.setState(j, report.Status)
	return nil
}

func (s *Service) process(ctx context.Context, j Job) {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = s.review(ctx, j)
		if err == nil {
			return
		}
		if errors.Is(err, errStaleReview) {
			s.setState(j, "STALE")
			return
		}
		if errors.Is(err, errReviewPolicy) {
			s.setState(j, "POLICY_DENIED")
			return
		}
		if errors.Is(err, errReportUncertain) {
			s.setState(j, "REPORT_FAILED")
			return
		}
		if !s.current(j) {
			s.setState(j, "STALE")
			return
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(attempt+1) * 20 * time.Millisecond):
			}
		}
	}
	// Even infrastructure failure may be reportable. Never promote it to ready.
	api, openErr := s.source.Open(ctx, j)
	if openErr == nil {
		if pubErr := s.publishCurrent(ctx, api, j, Report{Status: "REVIEW_UNAVAILABLE", Engine: s.engine.ID(), HeadSHA: j.HeadSHA, Summary: "Review unavailable after bounded retries; no readiness claim."}); pubErr == nil {
			return
		}
	}
	s.setState(j, "REPORT_FAILED")
}

// ProcessOne is an offline-friendly worker step; Run uses the same path.
func (s *Service) ProcessOne(ctx context.Context) bool {
	select {
	case j := <-s.queue:
		s.process(ctx, j)
		return true
	default:
		return false
	}
}
func (s *Service) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-s.queue:
			s.process(ctx, j)
		}
	}
}

func (r Report) checkBody() map[string]any {
	conclusion := "action_required"
	if r.Status == "READY_FOR_HUMAN_REVIEW" {
		conclusion = "success"
	}
	if r.Status == "BLOCKED" {
		conclusion = "failure"
	}
	var annotations []map[string]any
	var details strings.Builder
	for _, f := range r.Findings {
		fmt.Fprintf(&details, "- **%s** %s:%d — %s. Evidence: %s\n", f.Severity, f.Path, f.Line, f.Message, f.Evidence)
		if f.Path != "" && f.Line > 0 {
			level := "notice"
			if f.Severity == "BLOCKER" {
				level = "failure"
			} else if f.Severity == "MAJOR" {
				level = "warning"
			}
			annotations = append(annotations, map[string]any{"path": f.Path, "start_line": f.Line, "end_line": f.Line, "annotation_level": level, "message": f.Message})
		}
	}
	return map[string]any{"name": "puen-reviewer / AI review", "head_sha": r.HeadSHA, "status": "completed", "conclusion": conclusion,
		"completed_at": time.Now().UTC().Format(time.RFC3339),
		"output":       map[string]any{"title": r.Status, "summary": fmt.Sprintf("Reviewed head: %s\nEngine: %s\n%s", r.HeadSHA, r.Engine, r.Summary), "text": details.String(), "annotations": annotations}}
}
