package implementer

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
)

var requiredPermissions = map[string]string{"metadata": "read", "contents": "write", "pull_requests": "write"}
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9_.-]+$`)
var labelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
var branchSuffix = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$`)
var secretRefPattern = regexp.MustCompile(`^(env:[A-Z][A-Z0-9_]*|secret://[A-Za-z0-9_/-]+)$`)
var positiveID = regexp.MustCompile(`^[1-9][0-9]*$`)
var protectedName = regexp.MustCompile(`^[A-Za-z0-9_/-]+$`)

type Config struct {
	AppID             int64
	InstallationID    int64
	Repositories      map[string]int64
	ProtectedBranches []string
	PrivateKeyRef     string
}

type Authorization struct {
	WorkItem         string `json:"work_item"`
	Provider         string `json:"provider"`
	Tool             string `json:"tool"`
	AuthorizationRef string `json:"authorization_ref"`
	Repository       string `json:"repository"`
	Branch           string `json:"branch"`
	Base             string `json:"base"`
}

func fail(code string) error { return errors.New(code) }

func exactPermissions(value map[string]string) bool {
	if len(value) != len(requiredPermissions) {
		return false
	}
	for k, want := range requiredPermissions {
		if value[k] != want {
			return false
		}
	}
	return true
}

func samePermissions(actual, requested map[string]string) bool {
	if len(actual) != len(requested) {
		return false
	}
	for k, want := range requested {
		if actual[k] != want {
			return false
		}
	}
	return true
}

func decodeUniqueObject(raw []byte, dst any) error {
	var entries map[string]json.RawMessage
	d := json.NewDecoder(strings.NewReader(string(raw)))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return fail("configuration_invalid")
	}
	entries = make(map[string]json.RawMessage)
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return fail("configuration_invalid")
		}
		name, ok := key.(string)
		if !ok {
			return fail("configuration_invalid")
		}
		if _, exists := entries[name]; exists {
			return fail("configuration_invalid")
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return fail("configuration_invalid")
		}
		entries[name] = value
	}
	if _, err := d.Token(); err != nil {
		return fail("configuration_invalid")
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return fail("configuration_invalid")
	}
	encoded, _ := json.Marshal(entries)
	if err := json.Unmarshal(encoded, dst); err != nil {
		return fail("configuration_invalid")
	}
	return nil
}

func ConfigFromEnv(env map[string]string) (Config, error) {
	id, err := strconv.ParseInt(env["IMPLEMENTER_APP_ID"], 10, 64)
	if err != nil || id <= 0 || !positiveID.MatchString(env["IMPLEMENTER_APP_ID"]) {
		return Config{}, fail("configuration_invalid")
	}
	installation, err := strconv.ParseInt(env["IMPLEMENTER_INSTALLATION_ID"], 10, 64)
	if err != nil || installation <= 0 || !positiveID.MatchString(env["IMPLEMENTER_INSTALLATION_ID"]) {
		return Config{}, fail("configuration_invalid")
	}
	var repos map[string]int64
	if err := decodeUniqueObject([]byte(env["IMPLEMENTER_REPOSITORIES"]), &repos); err != nil {
		return Config{}, fail("configuration_invalid")
	}
	var perms map[string]string
	if err := decodeUniqueObject([]byte(env["IMPLEMENTER_PERMISSIONS"]), &perms); err != nil || !exactPermissions(perms) {
		return Config{}, fail("configuration_invalid")
	}
	var protected []string
	if err := json.Unmarshal([]byte(env["IMPLEMENTER_PROTECTED_BRANCHES"]), &protected); err != nil {
		return Config{}, fail("configuration_invalid")
	}
	c := Config{id, installation, repos, protected, env["IMPLEMENTER_PRIVATE_KEY_SOURCE"]}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if c.AppID <= 0 || c.InstallationID <= 0 || len(c.Repositories) == 0 || len(c.Repositories) > 100 || !secretRefPattern.MatchString(c.PrivateKeyRef) {
		return fail("configuration_invalid")
	}
	names, ids := map[string]bool{}, map[int64]bool{}
	for repo, id := range c.Repositories {
		parts := strings.Split(repo, "/")
		if !repoPattern.MatchString(repo) || parts[1] == "." || parts[1] == ".." || id <= 0 || names[strings.ToLower(repo)] || ids[id] {
			return fail("configuration_invalid")
		}
		names[strings.ToLower(repo)], ids[id] = true, true
	}
	protected := map[string]bool{}
	for _, branch := range c.ProtectedBranches {
		if !protectedName.MatchString(branch) || protected[branch] {
			return fail("configuration_invalid")
		}
		protected[branch] = true
	}
	if !protected["master"] || !protected["main"] {
		return fail("configuration_invalid")
	}
	return nil
}

func (a Authorization) Validate(c Config) error {
	for _, value := range []string{a.WorkItem, a.Provider, a.Tool, a.AuthorizationRef} {
		if !labelPattern.MatchString(value) {
			return fail("authorization_invalid")
		}
	}
	if _, ok := c.Repositories[a.Repository]; !ok {
		return fail("repository_denied")
	}
	prefix := "feature/" + a.WorkItem + "-"
	if !strings.HasPrefix(a.Branch, prefix) || !branchSuffix.MatchString(strings.TrimPrefix(a.Branch, prefix)) {
		return fail("branch_denied")
	}
	baseOK, branchDenied := false, false
	for _, protected := range c.ProtectedBranches {
		baseOK = baseOK || a.Base == protected
		branchDenied = branchDenied || a.Branch == protected
	}
	if !baseOK || branchDenied {
		return fail("branch_denied")
	}
	return nil
}
