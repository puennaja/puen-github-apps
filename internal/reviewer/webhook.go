package reviewer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/palantir/go-githubapp/githubapp"
)

const maxWebhookBytes = 2 << 20
const maxDeliveries = 2048

var deliveryName = regexp.MustCompile(`^[A-Za-z0-9-]{1,100}$`)

var (
	errPayload = errors.New("reviewer_payload_invalid")
	errDenied  = errors.New("reviewer_identity_denied")
	errQueue   = errors.New("reviewer_queue_full")
)

type Job struct {
	Repository   string
	RepositoryID int64
	Installation int64
	Number       int
	HeadSHA      string
	DeliveryID   string
}

func (j Job) key() string { return j.Repository + "#" + itoa(j.Number) }

type Service struct {
	config Config
	source Source
	engine Engine
	queue  chan Job
	mu     sync.Mutex
	seen   map[string]bool
	order  []string
	latest map[string]string
	state  map[string]string
	web    http.Handler
}

func NewService(c Config, source Source, engine Engine, queueCapacity int) (*Service, error) {
	if err := c.Validate(); err != nil || source == nil || engine == nil || queueCapacity < 1 || queueCapacity > 1024 {
		return nil, errors.New("reviewer_configuration_invalid")
	}
	s := &Service{config: c, source: source, engine: engine, queue: make(chan Job, queueCapacity), seen: map[string]bool{}, latest: map[string]string{}, state: map[string]string{}}
	s.web = githubapp.NewEventDispatcher([]githubapp.EventHandler{s}, c.WebhookSecret,
		githubapp.WithErrorCallback(func(w http.ResponseWriter, _ *http.Request, err error) {
			status := http.StatusBadRequest
			if errors.Is(err, errDenied) {
				status = http.StatusForbidden
			}
			if errors.Is(err, errQueue) {
				status = http.StatusServiceUnavailable
			}
			http.Error(w, http.StatusText(status), status)
		}),
		githubapp.WithResponseCallback(func(w http.ResponseWriter, _ *http.Request, _ string, _ bool) { w.WriteHeader(http.StatusAccepted) }),
	)
	return s, nil
}

// Handler rejects malformed requests before the framework verifies HMAC-SHA256.
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") ||
			!deliveryName.MatchString(r.Header.Get("X-GitHub-Delivery")) ||
			!strings.HasPrefix(r.Header.Get("X-Hub-Signature-256"), "sha256=") || len(r.Header.Get("X-Hub-Signature-256")) != 71 {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBytes)
		s.web.ServeHTTP(w, r)
	})
}

func (s *Service) Handles() []string { return []string{"pull_request"} }

func (s *Service) Handle(_ context.Context, event, delivery string, data []byte) error {
	if event != "pull_request" || !deliveryName.MatchString(delivery) {
		return errPayload
	}
	var p struct {
		Action       string `json:"action"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Repository struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequest struct {
			Number int  `json:"number"`
			Draft  bool `json:"draft"`
			Head   struct {
				SHA string `json:"sha"`
			} `json:"head"`
			Base struct {
				SHA string `json:"sha"`
				Ref string `json:"ref"`
			} `json:"base"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(data, &p); err != nil || p.PullRequest.Number < 1 || !shaName.MatchString(p.PullRequest.Head.SHA) || !shaName.MatchString(p.PullRequest.Base.SHA) {
		return errPayload
	}
	if p.PullRequest.Base.Ref != "master" {
		return errDenied
	}
	if p.Installation.ID != s.config.InstallationID || s.config.Repositories[p.Repository.FullName] != p.Repository.ID || p.Repository.ID <= 0 {
		return errDenied
	}
	if p.Action != "opened" && p.Action != "synchronize" && p.Action != "ready_for_review" && p.Action != "reopened" {
		return nil
	}
	j := Job{p.Repository.FullName, p.Repository.ID, p.Installation.ID, p.PullRequest.Number, strings.ToLower(p.PullRequest.Head.SHA), delivery}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[delivery] {
		return nil
	}
	if p.PullRequest.Draft {
		s.latest[j.key()] = j.HeadSHA
		s.state[j.key()] = "DRAFT_SKIPPED"
		s.remember(delivery)
		return nil
	}
	select {
	case s.queue <- j:
		s.latest[j.key()] = j.HeadSHA
		s.state[j.key()] = "QUEUED"
		s.remember(delivery)
		return nil
	default:
		return errQueue // GitHub can redeliver; do not mark this delivery seen.
	}
}

func (s *Service) remember(id string) {
	s.seen[id] = true
	s.order = append(s.order, id)
	if len(s.order) > maxDeliveries {
		delete(s.seen, s.order[0])
		s.order = s.order[1:]
	}
}

func (s *Service) current(j Job) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.latest[j.key()] == j.HeadSHA
}

func (s *Service) setState(j Job, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest[j.key()] == j.HeadSHA {
		s.state[j.key()] = value
	}
}

func (s *Service) Status(repo string, number int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state[repo+"#"+itoa(number)]
}
