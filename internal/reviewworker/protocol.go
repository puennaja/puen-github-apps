// Package reviewworker defines the inert, bounded boundary between the GitHub
// reviewer and a separately isolated analysis worker. It contains no credentials.
package reviewworker

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const Version = "review.v1"
const MaxRequestBytes = 1 << 20
const MaxResponseBytes = 64 << 10

var sha = regexp.MustCompile(`^[a-f0-9]{40}$`)
var repository = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9_.-]+$`)
var path = regexp.MustCompile(`^[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*$`)
var reference = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,99}$`)

var ErrInvalid = errors.New("review_worker_invalid")
var ErrUnavailable = errors.New("review_worker_unavailable")

// Diff and requirements are data. Trusted requirements must be fetched by the
// caller from the protected base SHA; this protocol cannot prove their origin.
type File struct {
	Path  string `json:"path"`
	Patch string `json:"patch"`
}
type Requirement struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}
type Limits struct {
	TimeoutMS int `json:"timeout_ms"`
	MemoryMiB int `json:"memory_mib"`
}
type Request struct {
	Version       string        `json:"version"`
	Repository    string        `json:"repository"`
	PRNumber      int           `json:"pr_number"`
	HeadSHA       string        `json:"head_sha"`
	BaseSHA       string        `json:"base_sha"`
	WorkItem      string        `json:"work_item"`
	Authorization string        `json:"authorization"`
	PRBody        string        `json:"pr_body"`
	Files         []File        `json:"files"`
	Requirements  []Requirement `json:"requirements"`
	Limits        Limits        `json:"limits"`
}
type Finding struct {
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
	Evidence string `json:"evidence"`
}
type Response struct {
	Version     string    `json:"version"`
	Complete    bool      `json:"complete"`
	ReviewedSHA string    `json:"reviewed_sha"`
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	Findings    []Finding `json:"findings"`
}

func validPath(s string) bool {
	if !path.MatchString(s) || strings.HasPrefix(s, ".git/") {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

func safeText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) && c != '\n' && c != '\r' && c != '\t' {
			return false
		}
	}
	return true
}

func (r Request) Validate() error {
	if r.Version != Version || !repository.MatchString(r.Repository) || strings.HasSuffix(r.Repository, "/.") || strings.HasSuffix(r.Repository, "/..") || r.PRNumber < 1 || !sha.MatchString(r.HeadSHA) || !sha.MatchString(r.BaseSHA) ||
		!reference.MatchString(r.WorkItem) || !reference.MatchString(r.Authorization) || r.Limits.TimeoutMS < 100 || r.Limits.TimeoutMS > 30000 ||
		r.Limits.MemoryMiB < 32 || r.Limits.MemoryMiB > 2048 || len(r.PRBody) > 32<<10 || !safeText(r.PRBody) || len(r.Files) == 0 || len(r.Files) > 100 || len(r.Requirements) == 0 || len(r.Requirements) > 12 {
		return ErrInvalid
	}
	totalDiff, totalRequirements, agents := 0, 0, false
	seen := map[string]bool{}
	for _, f := range r.Files {
		if !validPath(f.Path) || seen[f.Path] || len(f.Patch) == 0 || len(f.Patch) > 64<<10 || !strings.HasPrefix(f.Patch, "@@ ") || !safeText(f.Patch) {
			return ErrInvalid
		}
		seen[f.Path] = true
		totalDiff += len(f.Patch)
	}
	seen = map[string]bool{}
	for _, req := range r.Requirements {
		if !validPath(req.Path) || seen[req.Path] || len(req.Content) == 0 || len(req.Content) > 32<<10 || !safeText(req.Content) {
			return ErrInvalid
		}
		seen[req.Path] = true
		if req.Path == "AGENTS.md" {
			agents = true
		}
		totalRequirements += len(req.Content)
	}
	if !agents || totalDiff > 512<<10 || totalRequirements > 256<<10 {
		return ErrInvalid
	}
	encoded, err := json.Marshal(r)
	if err != nil || len(encoded) > MaxRequestBytes {
		return ErrInvalid
	}
	return nil
}

func EncodeRequest(r Request) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(r)
}

func DecodeRequest(data []byte) (Request, error) {
	var r Request
	if len(data) == 0 || len(data) > MaxRequestBytes || strict(data, &r) != nil || r.Validate() != nil {
		return Request{}, ErrInvalid
	}
	return r, nil
}

func DecodeResponse(data []byte, request Request, provider, model string) (Response, error) {
	var r Response
	if len(data) == 0 || len(data) > MaxResponseBytes || strict(data, &r) != nil || r.Validate(request, provider, model) != nil {
		return Response{}, ErrInvalid
	}
	return r, nil
}

func strict(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	if err := d.Decode(out); err != nil {
		return err
	}
	var tail any
	if err := d.Decode(&tail); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

func rejectDuplicateKeys(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return ErrInvalid
				}
				seen[name] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err := d.Token()
			return err
		case '[':
			for d.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err := d.Token()
			return err
		default:
			return ErrInvalid
		}
	}
	return walk()
}

func (r Response) Validate(req Request, provider, model string) error {
	if r.Version != Version || !r.Complete || r.ReviewedSHA != req.HeadSHA || r.Provider != provider || r.Model != model ||
		!reference.MatchString(r.Provider) || !reference.MatchString(r.Model) || len(r.Findings) > 20 {
		return ErrInvalid
	}
	lines := make(map[string]map[int]bool, len(req.Files))
	for _, f := range req.Files {
		lines[f.Path] = addedLines(f.Patch)
	}
	for _, f := range r.Findings {
		if f.Severity != "BLOCKER" && f.Severity != "MAJOR" && f.Severity != "MINOR" {
			return ErrInvalid
		}
		if f.Line <= 0 || !lines[f.Path][f.Line] || len(f.Message) == 0 || len(f.Message) > 500 || len(f.Evidence) == 0 || len(f.Evidence) > 1000 ||
			!safeText(f.Message) || !safeText(f.Evidence) {
			return ErrInvalid
		}
	}
	return nil
}

var hunk = regexp.MustCompile(`^@@ -[0-9]+(?:,[0-9]+)? \+([0-9]+)(?:,[0-9]+)? @@`)

func addedLines(patch string) map[int]bool {
	result := map[int]bool{}
	line, active := 0, false
	for _, row := range strings.Split(patch, "\n") {
		if m := hunk.FindStringSubmatch(row); m != nil {
			line = decimal(m[1])
			active = true
			continue
		}
		if !active || row == "" {
			continue
		}
		switch row[0] {
		case '+':
			result[line] = true
			line++
		case ' ':
			line++
		case '-':
		default:
			active = false
		}
	}
	return result
}
func decimal(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
