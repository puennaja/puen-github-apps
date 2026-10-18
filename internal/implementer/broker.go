package implementer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/palantir/go-githubapp/githubapp"
)

const apiURL = "https://api.github.com/"
const graphqlURL = "https://api.github.com/graphql"

type requester func(context.Context, string, string, any, any) error

type Broker struct {
	config      Config
	app         requester
	tokenClient func(string) (requester, error)
	now         func() time.Time
}

type session struct {
	api               requester
	expires           time.Time
	now               func() time.Time
	actor, repository string
}

func (s *session) request(ctx context.Context, method, suffix string, body, out any) error {
	if !s.expires.After(s.now().Add(30 * time.Second)) {
		return fail("token_expired")
	}
	return s.api(ctx, method, "repos/"+s.repository+suffix, body, out)
}

type guardedTransport struct {
	next    http.RoundTripper
	enabled bool
}

func (g guardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !g.enabled {
		return nil, fail("network_disabled")
	}
	if req.URL.Scheme != "https" || req.URL.Host != "api.github.com" || req.URL.User != nil || req.URL.RawQuery != "" || req.URL.Fragment != "" {
		return nil, fail("transport_request_invalid")
	}
	if req.Method != http.MethodGet && req.Method != http.MethodPost && req.Method != http.MethodPatch {
		return nil, fail("transport_request_invalid")
	}
	resp, err := g.next.RoundTrip(req)
	if err != nil {
		return nil, fail("github_transport_failed")
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		resp.Body.Close()
		return nil, fail("github_redirect_denied")
	}
	if resp.Body != nil {
		resp.Body = struct {
			io.Reader
			io.Closer
		}{Reader: io.LimitReader(resp.Body, 8*1024*1024), Closer: resp.Body}
	}
	return resp, nil
}

func NewFrameworkBroker(c Config, key []byte, networkEnabled bool) (*Broker, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if len(key) == 0 {
		return nil, fail("secret_source_unavailable")
	}
	if networkEnabled {
		for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
			if os.Getenv(name) != "" {
				return nil, fail("proxy_environment_denied")
			}
		}
	}
	creator := githubapp.NewClientCreator(apiURL, graphqlURL, c.AppID, key,
		githubapp.WithClientTimeout(20*time.Second),
		githubapp.WithClientMiddleware(func(next http.RoundTripper) http.RoundTripper {
			return guardedTransport{next: next, enabled: networkEnabled}
		}))
	app, err := creator.NewAppClient()
	if err != nil {
		return nil, fail("app_authentication_invalid")
	}
	makeRequester := func(method string, token string) (requester, error) {
		if method == "app" {
			return func(ctx context.Context, verb, path string, body, out any) error {
				ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
				defer cancel()
				req, err := app.NewRequest(ctx, verb, path, body)
				if err != nil {
					return fail("transport_request_invalid")
				}
				resp, err := app.Do(req, out)
				if err != nil {
					if resp != nil && resp.StatusCode == 404 {
						return fail("github_http_404")
					}
					return fail("github_request_failed")
				}
				return nil
			}, nil
		}
		client, err := creator.NewTokenClient(token)
		if err != nil {
			return nil, fail("token_client_invalid")
		}
		return func(ctx context.Context, verb, path string, body, out any) error {
			ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			req, err := client.NewRequest(ctx, verb, path, body)
			if err != nil {
				return fail("transport_request_invalid")
			}
			resp, err := client.Do(req, out)
			if err != nil {
				if resp != nil && resp.StatusCode == 404 {
					return fail("github_http_404")
				}
				return fail("github_request_failed")
			}
			return nil
		}, nil
	}
	appReq, _ := makeRequester("app", "")
	return &Broker{c, appReq, func(token string) (requester, error) { return makeRequester("token", token) }, time.Now}, nil
}

func (b *Broker) open(ctx context.Context, repository string, permissions map[string]string) (*session, error) {
	repoID, ok := b.config.Repositories[repository]
	if !ok {
		return nil, fail("repository_denied")
	}
	if len(permissions) == 0 || permissions["metadata"] != "read" {
		return nil, fail("permission_scope_invalid")
	}
	for name, level := range permissions {
		if requiredPermissions[name] == "" || (requiredPermissions[name] == "read" && level != "read") || (requiredPermissions[name] == "write" && level != "read" && level != "write") {
			return nil, fail("permission_scope_invalid")
		}
	}
	var app struct {
		ID   int64  `json:"id"`
		Slug string `json:"slug"`
	}
	if err := b.app(ctx, "GET", "app", nil, &app); err != nil {
		return nil, err
	}
	if app.ID != b.config.AppID || !regexpSlug.MatchString(app.Slug) {
		return nil, fail("app_identity_invalid")
	}
	path := fmt.Sprintf("app/installations/%d", b.config.InstallationID)
	var installation struct {
		ID          int64             `json:"id"`
		AppID       int64             `json:"app_id"`
		Selection   string            `json:"repository_selection"`
		SuspendedAt *string           `json:"suspended_at"`
		Permissions map[string]string `json:"permissions"`
	}
	if err := b.app(ctx, "GET", path, nil, &installation); err != nil {
		return nil, err
	}
	if installation.ID != b.config.InstallationID || installation.AppID != b.config.AppID || installation.Selection != "selected" || installation.SuspendedAt != nil || !exactPermissions(installation.Permissions) {
		return nil, fail("installation_invalid")
	}
	var issued struct {
		Token        string `json:"token"`
		ExpiresAt    string `json:"expires_at"`
		Selection    string `json:"repository_selection"`
		Repositories []struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repositories"`
		Permissions map[string]string `json:"permissions"`
	}
	body := struct {
		RepositoryIDs []int64           `json:"repository_ids"`
		Permissions   map[string]string `json:"permissions"`
	}{[]int64{repoID}, permissions}
	if err := b.app(ctx, "POST", path+"/access_tokens", body, &issued); err != nil {
		return nil, err
	}
	received := b.now()
	if issued.Selection != "selected" || len(issued.Repositories) != 1 || issued.Repositories[0].ID != repoID || issued.Repositories[0].FullName != repository {
		return nil, fail("token_repository_scope_invalid")
	}
	if !samePermissions(issued.Permissions, permissions) {
		return nil, fail("permission_scope_invalid")
	}
	expires, err := time.Parse(time.RFC3339, issued.ExpiresAt)
	if err != nil || !expires.After(received.Add(30*time.Second)) || expires.After(received.Add(time.Hour)) {
		return nil, fail("token_expiry_invalid")
	}
	if issued.Token == "" {
		return nil, fail("token_invalid")
	}
	api, err := b.tokenClient(issued.Token)
	if err != nil {
		return nil, fail("token_client_invalid")
	}
	return &session{api: api, expires: expires, now: b.now, actor: app.Slug + "[bot]", repository: repository}, nil
}

var regexpSlug = regexp.MustCompile(`^[a-z0-9-]+$`)

// Only the operator may resolve this reference; it is never accepted from an agent request.
func EnvironmentKey(ref string) ([]byte, error) {
	if !strings.HasPrefix(ref, "env:") {
		return nil, fail("secret_source_unavailable")
	}
	value := os.Getenv(strings.TrimPrefix(ref, "env:"))
	if value == "" {
		return nil, fail("secret_source_unavailable")
	}
	return []byte(value), nil
}
