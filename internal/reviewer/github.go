package reviewer

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/palantir/go-githubapp/githubapp"
)

type requestFunc func(context.Context, string, string, any, any) error

var errNotFound = errors.New("github_not_found")

type FrameworkSource struct {
	config      Config
	app         requestFunc
	tokenClient func(string) (requestFunc, error)
	now         func() time.Time
}

type boundedTransport struct {
	next    http.RoundTripper
	enabled bool
}

func (t boundedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !t.enabled || req.URL.Scheme != "https" || req.URL.Host != "api.github.com" || req.URL.User != nil || req.URL.Fragment != "" ||
		(req.Method != http.MethodGet && req.Method != http.MethodPost) {
		return nil, errors.New("github_transport_denied")
	}
	response, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, errors.New("github_transport_failed")
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		response.Body.Close()
		return nil, errors.New("github_redirect_denied")
	}
	if response.Body != nil {
		response.Body = struct {
			io.Reader
			io.Closer
		}{Reader: io.LimitReader(response.Body, 8<<20), Closer: response.Body}
	}
	return response, nil
}

// NewFrameworkSource creates a private App/token adapter. No executable enables it in GOV-003A.
// networkEnabled must remain false until the separate activation gate is approved.
func NewFrameworkSource(c Config, key []byte, networkEnabled bool) (*FrameworkSource, error) {
	if err := c.Validate(); err != nil || len(key) == 0 {
		return nil, errors.New("reviewer_configuration_invalid")
	}
	if networkEnabled {
		for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
			if os.Getenv(name) != "" {
				return nil, errors.New("reviewer_proxy_denied")
			}
		}
	}
	creator := githubapp.NewClientCreator("https://api.github.com/", "https://api.github.com/graphql", c.AppID, key,
		githubapp.WithClientTimeout(20*time.Second),
		githubapp.WithClientMiddleware(func(next http.RoundTripper) http.RoundTripper { return boundedTransport{next, networkEnabled} }))
	app, err := creator.NewAppClient()
	if err != nil {
		return nil, errors.New("reviewer_app_auth_invalid")
	}
	appRequest := func(ctx context.Context, method, path string, body, out any) error {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		req, err := app.NewRequest(ctx, method, path, body)
		if err != nil {
			return errors.New("github_request_invalid")
		}
		resp, err := app.Do(req, out)
		if err != nil {
			if resp != nil && resp.StatusCode == 404 {
				return errNotFound
			}
			return errors.New("github_request_failed")
		}
		return nil
	}
	return &FrameworkSource{config: c, app: appRequest, tokenClient: func(token string) (requestFunc, error) {
		client, err := creator.NewTokenClient(token)
		if err != nil {
			return nil, errors.New("reviewer_token_client_invalid")
		}
		return func(ctx context.Context, method, path string, body, out any) error {
			ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			req, err := client.NewRequest(ctx, method, path, body)
			if err != nil {
				return errors.New("github_request_invalid")
			}
			resp, err := client.Do(req, out)
			if err != nil {
				if resp != nil && resp.StatusCode == 404 {
					return errNotFound
				}
				return errors.New("github_request_failed")
			}
			return nil
		}, nil
	}, now: time.Now}, nil
}

type reviewerSession struct {
	api        requestFunc
	repository string
	expires    time.Time
	now        func() time.Time
}

func (s *reviewerSession) request(ctx context.Context, method, suffix string, body, out any) error {
	if !s.expires.After(s.now().Add(30 * time.Second)) {
		return errors.New("reviewer_token_expired")
	}
	return s.api(ctx, method, "repos/"+s.repository+suffix, body, out)
}

func (f *FrameworkSource) Open(ctx context.Context, j Job) (Session, error) {
	if f.config.Repositories[j.Repository] != j.RepositoryID || j.RepositoryID <= 0 || j.Installation != f.config.InstallationID ||
		j.Number < 1 || !shaName.MatchString(j.HeadSHA) {
		return nil, errors.New("reviewer_identity_denied")
	}
	var app struct {
		ID   int64  `json:"id"`
		Slug string `json:"slug"`
	}
	if err := f.app(ctx, "GET", "app", nil, &app); err != nil {
		return nil, errors.New("reviewer_app_request_failed")
	}
	if app.ID != f.config.AppID || app.Slug == "" {
		return nil, errors.New("reviewer_app_identity_invalid")
	}
	base := fmt.Sprintf("app/installations/%d", f.config.InstallationID)
	var installation struct {
		ID          int64             `json:"id"`
		AppID       int64             `json:"app_id"`
		Selection   string            `json:"repository_selection"`
		SuspendedAt *string           `json:"suspended_at"`
		Permissions map[string]string `json:"permissions"`
	}
	if err := f.app(ctx, "GET", base, nil, &installation); err != nil {
		return nil, errors.New("reviewer_installation_request_failed")
	}
	if installation.ID != f.config.InstallationID || installation.AppID != f.config.AppID || installation.Selection != "selected" ||
		installation.SuspendedAt != nil || !exactPermissions(installation.Permissions, requiredPermissions) {
		return nil, errors.New("reviewer_installation_invalid")
	}
	var issued struct {
		Token        string            `json:"token"`
		ExpiresAt    string            `json:"expires_at"`
		Selection    string            `json:"repository_selection"`
		Permissions  map[string]string `json:"permissions"`
		Repositories []struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repositories"`
	}
	body := struct {
		RepositoryIDs []int64           `json:"repository_ids"`
		Permissions   map[string]string `json:"permissions"`
	}{[]int64{j.RepositoryID}, requiredPermissions}
	if err := f.app(ctx, "POST", base+"/access_tokens", body, &issued); err != nil {
		return nil, errors.New("reviewer_token_request_failed")
	}
	if issued.Selection != "selected" || len(issued.Repositories) != 1 || issued.Repositories[0].ID != j.RepositoryID ||
		issued.Repositories[0].FullName != j.Repository || !exactPermissions(issued.Permissions, requiredPermissions) || issued.Token == "" {
		return nil, errors.New("reviewer_token_scope_invalid")
	}
	now := f.now()
	expires, err := time.Parse(time.RFC3339, issued.ExpiresAt)
	if err != nil || !expires.After(now.Add(30*time.Second)) || expires.After(now.Add(time.Hour)) {
		return nil, errors.New("reviewer_token_expiry_invalid")
	}
	api, err := f.tokenClient(issued.Token)
	if err != nil {
		return nil, errors.New("reviewer_token_client_invalid")
	}
	return &reviewerSession{api: api, repository: j.Repository, expires: expires, now: f.now}, nil
}

func (s *reviewerSession) GetPullRequest(ctx context.Context, number int) (PullRequest, error) {
	if number < 1 {
		return PullRequest{}, errors.New("reviewer_pr_invalid")
	}
	var raw struct {
		Number int    `json:"number"`
		Draft  bool   `json:"draft"`
		State  string `json:"state"`
		Body   string `json:"body"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"base"`
	}
	if err := s.request(ctx, "GET", "/pulls/"+itoa(number), nil, &raw); err != nil {
		return PullRequest{}, err
	}
	return PullRequest{Number: raw.Number, HeadSHA: raw.Head.SHA, BaseSHA: raw.Base.SHA, BaseRef: raw.Base.Ref, Draft: raw.Draft, State: raw.State, Body: raw.Body}, nil
}

func (s *reviewerSession) ListFiles(ctx context.Context, number int) ([]DiffFile, error) {
	var raw []struct {
		Filename string `json:"filename"`
		Patch    string `json:"patch"`
	}
	path := "/pulls/" + itoa(number) + "/files?per_page=100&page=1"
	if err := s.request(ctx, "GET", path, nil, &raw); err != nil {
		return nil, err
	}
	if len(raw) > maxFiles {
		return nil, errors.New("review_diff_limit")
	}
	if len(raw) == maxFiles {
		var next []struct {
			Filename string `json:"filename"`
		}
		if err := s.request(ctx, "GET", "/pulls/"+itoa(number)+"/files?per_page=1&page=101", nil, &next); err != nil {
			return nil, err
		}
		if len(next) != 0 {
			return nil, errors.New("review_diff_limit")
		}
	}
	files := make([]DiffFile, len(raw))
	for i, file := range raw {
		files[i] = DiffFile{file.Filename, file.Patch}
	}
	return files, nil
}

func (s *reviewerSession) ReadContext(ctx context.Context, path, sha string) (string, bool, error) {
	if !validPath(path) || !shaName.MatchString(sha) {
		return "", false, errors.New("reviewer_context_invalid")
	}
	var raw struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		Size     int    `json:"size"`
	}
	err := s.request(ctx, "GET", "/contents/"+path+"?ref="+sha, nil, &raw)
	if errors.Is(err, errNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if raw.Type != "file" || raw.Encoding != "base64" || raw.Size > maxContextFile || len(raw.Content) > 2*maxContextFile {
		return "", false, errors.New("reviewer_context_invalid")
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(raw.Content, "\n", ""))
	if err != nil || len(content) > maxContextFile || len(content) != raw.Size || !utf8.Valid(content) {
		return "", false, errors.New("reviewer_context_invalid")
	}
	return string(content), true, nil
}

func (s *reviewerSession) PublishCheck(ctx context.Context, report Report) error {
	if !shaName.MatchString(report.HeadSHA) || (report.Status != "BLOCKED" && report.Status != "READY_FOR_HUMAN_REVIEW" && report.Status != "REVIEW_UNAVAILABLE") {
		return errors.New("reviewer_report_invalid")
	}
	return s.request(ctx, "POST", "/check-runs", report.checkBody(), nil)
}
