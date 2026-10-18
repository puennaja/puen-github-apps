package reviewer

import (
	"errors"
	"regexp"
	"strings"
)

var (
	repoName = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9_.-]+$`)
	shaName  = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)
	pathName = regexp.MustCompile(`^[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*$`)
)

var requiredPermissions = map[string]string{
	"metadata": "read", "contents": "read", "pull_requests": "read", "checks": "write",
}

// Config is operator-owned. The webhook secret and App key never enter a review job.
type Config struct {
	AppID, InstallationID int64
	Repositories          map[string]int64
	WebhookSecret         string
}

func (c Config) Validate() error {
	if c.AppID <= 0 || c.InstallationID <= 0 || len(c.Repositories) == 0 || len(c.Repositories) > 100 || len(c.WebhookSecret) < 32 {
		return errors.New("reviewer_configuration_invalid")
	}
	seen := map[int64]bool{}
	names := map[string]bool{}
	for name, id := range c.Repositories {
		parts := strings.Split(name, "/")
		if !repoName.MatchString(name) || parts[1] == "." || parts[1] == ".." || id <= 0 || seen[id] || names[strings.ToLower(name)] {
			return errors.New("reviewer_configuration_invalid")
		}
		seen[id] = true
		names[strings.ToLower(name)] = true
	}
	return nil
}

func exactPermissions(actual, want map[string]string) bool {
	if len(actual) != len(want) {
		return false
	}
	for key, level := range want {
		if actual[key] != level {
			return false
		}
	}
	return true
}

func validPath(path string) bool {
	if !pathName.MatchString(path) || strings.HasPrefix(path, ".git/") || strings.Contains(path, "/../") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}
