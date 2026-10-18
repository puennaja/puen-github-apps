package implementer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var pathPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type AuditEvent struct {
	Timestamp        string `json:"timestamp"`
	WorkItem         string `json:"work_item"`
	Provider         string `json:"provider"`
	Tool             string `json:"tool"`
	AuthorizationRef string `json:"authorization_ref"`
	Actor            string `json:"actor,omitempty"`
	Repository       string `json:"repository"`
	Branch           string `json:"branch"`
	CommitSHA        string `json:"commit_sha,omitempty"`
	PullRequest      int    `json:"pull_request,omitempty"`
	Operation        string `json:"operation"`
	Outcome          string `json:"outcome"`
	RemoteEffect     string `json:"remote_effect"`
	ErrorCode        string `json:"error_code,omitempty"`
}

type AuditSink interface{ Append(AuditEvent) error }
type AuditFunc func(AuditEvent) error

func (f AuditFunc) Append(event AuditEvent) error { return f(event) }

type Gateway struct {
	config Config
	grant  Authorization
	broker *Broker
	audit  AuditSink
	now    func() time.Time
}

func NewGateway(c Config, a Authorization, b *Broker, audit AuditSink) (*Gateway, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := a.Validate(c); err != nil {
		return nil, err
	}
	if b == nil || audit == nil {
		return nil, fail("configuration_invalid")
	}
	return &Gateway{c, a, b, audit, time.Now}, nil
}

func (g *Gateway) record(event AuditEvent) error {
	event.Timestamp = g.now().UTC().Format(time.RFC3339Nano)
	if err := g.audit.Append(event); err != nil {
		return fail("audit_unavailable")
	}
	return nil
}

func (g *Gateway) Execute(ctx context.Context, op string, raw json.RawMessage) (result any, err error) {
	valid := map[string]bool{"getRepository": true, "pushFeatureBranch": true, "createDraftPullRequest": true, "updateOwnPullRequest": true}
	name := op
	if !valid[op] {
		name = "denied"
	}
	event := AuditEvent{WorkItem: g.grant.WorkItem, Provider: g.grant.Provider, Tool: g.grant.Tool,
		AuthorizationRef: g.grant.AuthorizationRef, Repository: g.grant.Repository, Branch: g.grant.Branch,
		Operation: name, Outcome: "STARTED", RemoteEffect: "UNKNOWN"}
	if err = g.record(event); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			event.Outcome = "FAILED"
			event.ErrorCode = safeCode(err)
		} else {
			event.Outcome = "PASSED"
		}
		if auditErr := g.record(event); auditErr != nil {
			result = nil
			err = auditErr
		}
		if err != nil {
			err = fail(safeCode(err))
		}
	}()
	if !valid[op] {
		return nil, fail("operation_denied")
	}
	var payload any
	switch op {
	case "getRepository":
		if err = decodeArguments(raw, &struct{}{}); err != nil {
			return nil, err
		}
	case "pushFeatureBranch":
		var v pushArgs
		if err = decodeArguments(raw, &v); err != nil {
			return nil, err
		}
		if err = validatePush(v); err != nil {
			return nil, err
		}
		payload = v
	case "createDraftPullRequest":
		var v createArgs
		if err = decodeArguments(raw, &v); err != nil {
			return nil, err
		}
		if err = validateCreate(v); err != nil {
			return nil, err
		}
		payload = v
	case "updateOwnPullRequest":
		var v updateArgs
		if err = decodeArguments(raw, &v); err != nil {
			return nil, err
		}
		if err = validateUpdate(v); err != nil {
			return nil, err
		}
		payload = v
		event.PullRequest = v.Number
	}
	permissions := map[string]string{"metadata": "read"}
	switch op {
	case "pushFeatureBranch":
		permissions["contents"] = "write"
	case "createDraftPullRequest":
		permissions["contents"] = "read"
		permissions["pull_requests"] = "write"
	case "updateOwnPullRequest":
		if len(payload.(updateArgs).Changes) > 0 {
			permissions["contents"] = "read"
			permissions["pull_requests"] = "write"
		} else {
			permissions["pull_requests"] = "read"
		}
	}
	s, openErr := g.broker.open(ctx, g.grant.Repository, permissions)
	if openErr != nil {
		return nil, openErr
	}
	event.Actor = s.actor
	switch op {
	case "getRepository":
		result, err = g.getRepository(ctx, s)
	case "pushFeatureBranch":
		result, err = g.pushFeatureBranch(ctx, s, payload.(pushArgs), &event)
	case "createDraftPullRequest":
		result, err = g.createDraftPullRequest(ctx, s, payload.(createArgs), &event)
	case "updateOwnPullRequest":
		result, err = g.updateOwnPullRequest(ctx, s, payload.(updateArgs), &event)
	}
	if err == nil {
		if op == "pushFeatureBranch" || op == "createDraftPullRequest" || (op == "updateOwnPullRequest" && len(payload.(updateArgs).Changes) > 0) {
			event.RemoteEffect = "APPLIED"
		} else {
			event.RemoteEffect = "NONE"
		}
	}
	return result, err
}

func safeCode(err error) string {
	allowed := map[string]bool{"operation_denied": true, "operation_arguments_invalid": true, "commit_invalid": true, "changes_invalid": true, "path_denied": true, "workflow_denied": true, "pr_invalid": true, "pr_changes_denied": true, "text_invalid": true, "token_expired": true, "repository_denied": true, "app_identity_invalid": true, "installation_invalid": true, "token_repository_scope_invalid": true, "permission_scope_invalid": true, "token_expiry_invalid": true, "token_invalid": true, "token_client_invalid": true, "github_http_404": true, "github_request_failed": true, "github_transport_failed": true, "network_disabled": true, "transport_request_invalid": true, "github_redirect_denied": true, "repository_identity_invalid": true, "branch_identity_invalid": true, "base_unprotected": true, "branch_denied": true, "head_changed": true, "ref_response_invalid": true, "pr_not_owned_draft": true, "audit_unavailable": true}
	if allowed[err.Error()] {
		return err.Error()
	}
	return "operation_failed"
}

func decodeArguments(raw json.RawMessage, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return fail("operation_arguments_invalid")
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fail("operation_arguments_invalid")
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return fail("operation_arguments_invalid")
	}
	return nil
}

type pushArgs struct {
	ExpectedHead string             `json:"expected_head"`
	Changes      map[string]*string `json:"changes"`
	Message      string             `json:"message"`
}
type createArgs struct {
	ExpectedHead string `json:"expected_head"`
	Title        string `json:"title"`
	Body         string `json:"body"`
}
type updateArgs struct {
	Number  int               `json:"number"`
	Changes map[string]string `json:"changes"`
}

func validText(text string, max int, empty bool) bool {
	return len(text) <= max && (empty || strings.TrimSpace(text) != "")
}

func validatePush(v pushArgs) error {
	if !shaPattern.MatchString(v.ExpectedHead) {
		return fail("commit_invalid")
	}
	if !validText(v.Message, 1000, false) {
		return fail("text_invalid")
	}
	if len(v.Changes) == 0 || len(v.Changes) > 100 {
		return fail("changes_invalid")
	}
	for path, content := range v.Changes {
		if len(path) == 0 || len(path) > 240 {
			return fail("path_denied")
		}
		for _, part := range strings.Split(path, "/") {
			if !pathPart.MatchString(part) || part == "." || part == ".." || strings.EqualFold(part, ".git") {
				return fail("path_denied")
			}
		}
		lower := strings.ToLower(path)
		if lower == ".github" || lower == ".github/workflows" || strings.HasPrefix(lower, ".github/workflows/") {
			return fail("workflow_denied")
		}
		if content != nil && !validText(*content, 100000, true) {
			return fail("text_invalid")
		}
	}
	return nil
}
func validateCreate(v createArgs) error {
	if !shaPattern.MatchString(v.ExpectedHead) {
		return fail("commit_invalid")
	}
	if !validText(v.Title, 256, false) || !validText(v.Body, 60000, true) {
		return fail("text_invalid")
	}
	return nil
}
func validateUpdate(v updateArgs) error {
	if v.Number <= 0 {
		return fail("pr_invalid")
	}
	if v.Changes == nil {
		return fail("pr_changes_denied")
	}
	for k, value := range v.Changes {
		if k != "title" && k != "body" {
			return fail("pr_changes_denied")
		}
		if !validText(value, map[string]int{"title": 256, "body": 60000}[k], k == "body") {
			return fail("text_invalid")
		}
	}
	return nil
}

type branchResponse struct {
	Name      string `json:"name"`
	Protected *bool  `json:"protected"`
	Commit    struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}
type pullResponse struct {
	Number int    `json:"number"`
	Draft  *bool  `json:"draft"`
	State  string `json:"state"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	User   struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
	Head struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref  string `json:"ref"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"base"`
}

func (g *Gateway) getRepository(ctx context.Context, s *session) (any, error) {
	var repo struct {
		ID            int64  `json:"id"`
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := s.request(ctx, "GET", "", nil, &repo); err != nil {
		return nil, err
	}
	if repo.ID != g.config.Repositories[g.grant.Repository] || repo.FullName != g.grant.Repository {
		return nil, fail("repository_identity_invalid")
	}
	return map[string]any{"id": repo.ID, "repository": repo.FullName, "default_branch": repo.DefaultBranch, "actor": s.actor}, nil
}
func (g *Gateway) branch(ctx context.Context, s *session, name string) (branchResponse, error) {
	var v branchResponse
	if err := s.request(ctx, "GET", "/branches/"+url.PathEscape(name), nil, &v); err != nil {
		return v, err
	}
	if v.Name != name || !shaPattern.MatchString(v.Commit.SHA) || v.Protected == nil {
		return v, fail("branch_identity_invalid")
	}
	return v, nil
}
func (g *Gateway) base(ctx context.Context, s *session) (branchResponse, error) {
	v, err := g.branch(ctx, s, g.grant.Base)
	if err != nil {
		return v, err
	}
	if !*v.Protected {
		return v, fail("base_unprotected")
	}
	return v, nil
}
func (g *Gateway) feature(ctx context.Context, s *session) (branchResponse, error) {
	v, err := g.branch(ctx, s, g.grant.Branch)
	if err != nil {
		return v, err
	}
	if *v.Protected {
		return v, fail("branch_denied")
	}
	return v, nil
}

func (g *Gateway) pushFeatureBranch(ctx context.Context, s *session, v pushArgs, event *AuditEvent) (any, error) {
	base, err := g.base(ctx, s)
	if err != nil {
		return nil, err
	}
	feature, err := g.feature(ctx, s)
	newBranch := false
	if err != nil {
		if err.Error() != "github_http_404" {
			return nil, err
		}
		feature = base
		newBranch = true
	}
	head := feature.Commit.SHA
	if head != v.ExpectedHead {
		return nil, fail("head_changed")
	}
	var commit struct {
		SHA  string `json:"sha"`
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := s.request(ctx, "GET", "/git/commits/"+head, nil, &commit); err != nil {
		return nil, err
	}
	if commit.SHA != head || !shaPattern.MatchString(commit.Tree.SHA) {
		return nil, fail("commit_invalid")
	}
	tree := make([]map[string]any, 0, len(v.Changes))
	for path, content := range v.Changes {
		entry := map[string]any{"path": path, "mode": "100644", "type": "blob"}
		if content == nil {
			entry["sha"] = nil
		} else {
			entry["content"] = *content
		}
		tree = append(tree, entry)
	}
	var madeTree struct {
		SHA string `json:"sha"`
	}
	if err := s.request(ctx, "POST", "/git/trees", map[string]any{"base_tree": commit.Tree.SHA, "tree": tree}, &madeTree); err != nil {
		return nil, err
	}
	if !shaPattern.MatchString(madeTree.SHA) {
		return nil, fail("commit_invalid")
	}
	var madeCommit struct {
		SHA string `json:"sha"`
	}
	if err := s.request(ctx, "POST", "/git/commits", map[string]any{"message": v.Message, "tree": madeTree.SHA, "parents": []string{head}}, &madeCommit); err != nil {
		return nil, err
	}
	if !shaPattern.MatchString(madeCommit.SHA) {
		return nil, fail("commit_invalid")
	}
	event.CommitSHA = madeCommit.SHA
	if _, err := g.base(ctx, s); err != nil {
		return nil, err
	}
	if !newBranch {
		current, err := g.feature(ctx, s)
		if err != nil {
			return nil, err
		}
		if current.Commit.SHA != head {
			return nil, fail("head_changed")
		}
	}
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if newBranch {
		err = s.request(ctx, "POST", "/git/refs", map[string]any{"ref": "refs/heads/" + g.grant.Branch, "sha": madeCommit.SHA}, &ref)
	} else {
		err = s.request(ctx, "PATCH", "/git/refs/heads/"+g.grant.Branch, map[string]any{"sha": madeCommit.SHA, "force": false}, &ref)
	}
	if err != nil {
		return nil, err
	}
	if ref.Ref != "refs/heads/"+g.grant.Branch || ref.Object.SHA != madeCommit.SHA {
		return nil, fail("ref_response_invalid")
	}
	return map[string]any{"branch": g.grant.Branch, "commit_sha": madeCommit.SHA, "actor": s.actor}, nil
}

func (g *Gateway) own(pr pullResponse, s *session) error {
	if pr.User.Login != s.actor || pr.User.Type != "Bot" || pr.Draft == nil || !*pr.Draft || pr.State != "open" || pr.Head.Ref != g.grant.Branch || pr.Base.Ref != g.grant.Base || pr.Head.Repo.FullName != g.grant.Repository || pr.Base.Repo.FullName != g.grant.Repository {
		return fail("pr_not_owned_draft")
	}
	if pr.Number <= 0 || !shaPattern.MatchString(pr.Head.SHA) {
		return fail("pr_invalid")
	}
	return nil
}
func observePR(pr pullResponse, event *AuditEvent) {
	if pr.Number > 0 {
		event.PullRequest = pr.Number
	}
	if shaPattern.MatchString(pr.Head.SHA) {
		event.CommitSHA = pr.Head.SHA
	}
}
func prResult(pr pullResponse, s *session) any {
	return map[string]any{"number": pr.Number, "draft": *pr.Draft, "commit_sha": pr.Head.SHA, "actor": s.actor, "title": pr.Title, "body": pr.Body}
}

func (g *Gateway) createDraftPullRequest(ctx context.Context, s *session, v createArgs, event *AuditEvent) (any, error) {
	if _, err := g.base(ctx, s); err != nil {
		return nil, err
	}
	feature, err := g.feature(ctx, s)
	if err != nil {
		return nil, err
	}
	if feature.Commit.SHA != v.ExpectedHead {
		return nil, fail("head_changed")
	}
	var pr pullResponse
	body := map[string]any{"head": g.grant.Branch, "base": g.grant.Base, "title": v.Title, "body": v.Body, "draft": true, "maintainer_can_modify": false}
	if err := s.request(ctx, "POST", "/pulls", body, &pr); err != nil {
		return nil, err
	}
	observePR(pr, event)
	if err := g.own(pr, s); err != nil {
		return nil, err
	}
	if pr.Head.SHA != v.ExpectedHead {
		return nil, fail("head_changed")
	}
	return prResult(pr, s), nil
}
func (g *Gateway) updateOwnPullRequest(ctx context.Context, s *session, v updateArgs, event *AuditEvent) (any, error) {
	path := fmt.Sprintf("/pulls/%d", v.Number)
	var pr pullResponse
	if err := s.request(ctx, "GET", path, nil, &pr); err != nil {
		return nil, err
	}
	observePR(pr, event)
	if err := g.own(pr, s); err != nil {
		return nil, err
	}
	if pr.Number != v.Number {
		return nil, fail("pr_invalid")
	}
	if len(v.Changes) > 0 {
		if _, err := g.base(ctx, s); err != nil {
			return nil, err
		}
		if _, err := g.feature(ctx, s); err != nil {
			return nil, err
		}
		if err := s.request(ctx, "PATCH", path, v.Changes, &pr); err != nil {
			return nil, err
		}
		observePR(pr, event)
		if err := g.own(pr, s); err != nil {
			return nil, err
		}
		if pr.Number != v.Number {
			return nil, fail("pr_invalid")
		}
	}
	return prResult(pr, s), nil
}
