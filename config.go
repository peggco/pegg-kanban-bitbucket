package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	kindCloud      = "cloud"
	kindDataCenter = "datacenter"

	defaultCloudBaseURL = "https://api.bitbucket.org/2.0"
	defaultWorktreeRoot = ".pegg-worktrees"
	defaultTimeout      = 30
)

type Settings struct {
	Enabled               bool     `json:"enabled"`
	Kind                  string   `json:"kind"`
	BaseURL               string   `json:"base_url"`
	Workspace             string   `json:"workspace"`
	Project               string   `json:"project"`
	Repo                  string   `json:"repo"`
	Email                 string   `json:"email"`
	APIToken              string   `json:"api_token"`
	AccessToken           string   `json:"access_token"`
	Username              string   `json:"username"`
	Password              string   `json:"password"`
	Token                 string   `json:"token"`
	DefaultBase           string   `json:"default_base"`
	WorktreeRoot          string   `json:"worktree_root"`
	IssueProviders        []string `json:"issue_providers"`
	RequestTimeoutSeconds int      `json:"request_timeout_seconds"`
	Workdir               string   `json:"-"`
}

func defaultSettings() Settings {
	wd, _ := os.Getwd()
	return Settings{
		Enabled:               true,
		Kind:                  kindCloud,
		BaseURL:               defaultCloudBaseURL,
		WorktreeRoot:          defaultWorktreeRoot,
		IssueProviders:        []string{"jira"},
		RequestTimeoutSeconds: defaultTimeout,
		Workdir:               wd,
	}
}

func (s *Settings) applyHost(configJSON []byte) {
	if len(configJSON) == 0 {
		return
	}
	var host struct {
		Plugins struct {
			Options map[string]json.RawMessage `json:"options"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(configJSON, &host); err != nil {
		return
	}
	for _, key := range []string{"bitbucket", "pegg-bitbucket", "pegg-kanban-bitbucket", "kanban-bitbucket"} {
		raw := host.Plugins.Options[key]
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		_ = json.Unmarshal(raw, s)
	}
}

func (s *Settings) applyOptions(options json.RawMessage) error {
	if len(options) == 0 || string(options) == "null" {
		return nil
	}
	if err := json.Unmarshal(options, s); err != nil {
		return fmt.Errorf("parse %s options: %w", pluginName, err)
	}
	return nil
}

func (s *Settings) applyEnv() {
	if v := strings.TrimSpace(os.Getenv("PEGG_BITBUCKET_ENABLED")); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			s.Enabled = enabled
		}
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_BITBUCKET_KIND")); v != "" {
		s.Kind = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_BITBUCKET_URL")); v != "" {
		s.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_BITBUCKET_WORKSPACE")); v != "" {
		s.Workspace = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_BITBUCKET_PROJECT")); v != "" {
		s.Project = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_BITBUCKET_REPO")); v != "" {
		s.Repo = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_BITBUCKET_EMAIL")); v != "" {
		s.Email = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_BITBUCKET_API_TOKEN")); v != "" {
		s.APIToken = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_BITBUCKET_ACCESS_TOKEN")); v != "" {
		s.AccessToken = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_BITBUCKET_USERNAME")); v != "" {
		s.Username = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_BITBUCKET_PASSWORD")); v != "" {
		s.Password = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_BITBUCKET_TOKEN")); v != "" {
		s.Token = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_BITBUCKET_DEFAULT_BASE")); v != "" {
		s.DefaultBase = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_BITBUCKET_ISSUE_PROVIDERS")); v != "" {
		s.IssueProviders = parseStringList(v)
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_BITBUCKET_WORKTREE_ROOT")); v != "" {
		s.WorktreeRoot = v
	}
	if v := strings.TrimSpace(os.Getenv("PEGG_BITBUCKET_TIMEOUT_SECONDS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s.RequestTimeoutSeconds = n
		}
	}
	s.normalize()
}

func (s *Settings) normalize() {
	s.Kind = normalizeKind(s.Kind)
	s.BaseURL = normalizeBaseURL(s.Kind, s.BaseURL)
	s.Workspace = strings.TrimSpace(s.Workspace)
	s.Project = strings.TrimSpace(s.Project)
	s.Repo = strings.TrimSpace(s.Repo)
	s.Email = strings.TrimSpace(s.Email)
	s.APIToken = strings.TrimSpace(s.APIToken)
	s.AccessToken = strings.TrimSpace(s.AccessToken)
	s.Username = strings.TrimSpace(s.Username)
	s.Password = strings.TrimSpace(s.Password)
	s.Token = strings.TrimSpace(s.Token)
	s.DefaultBase = strings.TrimSpace(s.DefaultBase)
	s.IssueProviders = cleanStrings(s.IssueProviders)
	if strings.TrimSpace(s.WorktreeRoot) == "" {
		s.WorktreeRoot = defaultWorktreeRoot
	}
	s.WorktreeRoot = expandHome(s.WorktreeRoot)
	if s.RequestTimeoutSeconds <= 0 {
		s.RequestTimeoutSeconds = defaultTimeout
	}
	if strings.TrimSpace(s.Workdir) == "" {
		s.Workdir, _ = os.Getwd()
	}
	s.Workdir = expandHome(s.Workdir)
}

func normalizeKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", kindCloud:
		return kindCloud
	case "datacenter", "data-center", "data_center", "data center", "dc", "server":
		return kindDataCenter
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeBaseURL(kind, value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if kind == kindDataCenter && value == defaultCloudBaseURL {
		return ""
	}
	if value == "" {
		if kind == kindDataCenter {
			return ""
		}
		return defaultCloudBaseURL
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return value
	}
	if kind == kindCloud && strings.Trim(parsed.Path, "/") == "" {
		return parsed.Scheme + "://" + parsed.Host + "/2.0"
	}
	return strings.TrimRight(parsed.String(), "/")
}

func (s Settings) timeout() time.Duration {
	if s.RequestTimeoutSeconds <= 0 {
		return defaultTimeout * time.Second
	}
	return time.Duration(s.RequestTimeoutSeconds) * time.Second
}

func (s Settings) secrets() []string {
	values := []string{s.APIToken, s.AccessToken, s.Password, s.Token}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

func parseStringList(value string) []string {
	var values []string
	if err := json.Unmarshal([]byte(value), &values); err == nil {
		return cleanStrings(values)
	}
	return cleanStrings(strings.Split(value, ","))
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.ToLower(strings.TrimSpace(value)); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func redactSettings(msg string, s Settings) string {
	for _, secret := range s.secrets() {
		msg = strings.ReplaceAll(msg, secret, "[redacted]")
	}
	return msg
}

func redactError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", err.Error())
}
