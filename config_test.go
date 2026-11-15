package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func clearBitbucketEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"PEGG_BITBUCKET_ENABLED",
		"PEGG_BITBUCKET_KIND",
		"PEGG_BITBUCKET_URL",
		"PEGG_BITBUCKET_WORKSPACE",
		"PEGG_BITBUCKET_PROJECT",
		"PEGG_BITBUCKET_REPO",
		"PEGG_BITBUCKET_EMAIL",
		"PEGG_BITBUCKET_API_TOKEN",
		"PEGG_BITBUCKET_ACCESS_TOKEN",
		"PEGG_BITBUCKET_USERNAME",
		"PEGG_BITBUCKET_PASSWORD",
		"PEGG_BITBUCKET_TOKEN",
		"PEGG_BITBUCKET_DEFAULT_BASE",
		"PEGG_BITBUCKET_WORKTREE_ROOT",
		"PEGG_BITBUCKET_TIMEOUT_SECONDS",
	} {
		t.Setenv(name, "")
	}
}

func TestDefaultSettings(t *testing.T) {
	clearBitbucketEnv(t)
	settings := defaultSettings()
	if !settings.Enabled {
		t.Fatal("enabled should default to true")
	}
	if settings.Kind != kindCloud {
		t.Fatalf("kind = %q", settings.Kind)
	}
	if settings.BaseURL != defaultCloudBaseURL {
		t.Fatalf("base_url = %q", settings.BaseURL)
	}
	if settings.WorktreeRoot != defaultWorktreeRoot {
		t.Fatalf("worktree_root = %q", settings.WorktreeRoot)
	}
	if settings.RequestTimeoutSeconds != 30 {
		t.Fatalf("request_timeout_seconds = %d", settings.RequestTimeoutSeconds)
	}
	if settings.Workdir == "" {
		t.Fatal("workdir should default to the current directory")
	}
}

func TestEnvOverrides(t *testing.T) {
	clearBitbucketEnv(t)
	t.Setenv("PEGG_BITBUCKET_ENABLED", "false")
	t.Setenv("PEGG_BITBUCKET_KIND", "dc")
	t.Setenv("PEGG_BITBUCKET_URL", "https://bb.example.com/")
	t.Setenv("PEGG_BITBUCKET_WORKSPACE", "acme")
	t.Setenv("PEGG_BITBUCKET_PROJECT", "PROJ")
	t.Setenv("PEGG_BITBUCKET_REPO", "widgets")
	t.Setenv("PEGG_BITBUCKET_EMAIL", "bot@example.com")
	t.Setenv("PEGG_BITBUCKET_API_TOKEN", "api-token")
	t.Setenv("PEGG_BITBUCKET_ACCESS_TOKEN", "access-token")
	t.Setenv("PEGG_BITBUCKET_USERNAME", "admin")
	t.Setenv("PEGG_BITBUCKET_PASSWORD", "hunter2")
	t.Setenv("PEGG_BITBUCKET_TOKEN", "dc-token")
	t.Setenv("PEGG_BITBUCKET_DEFAULT_BASE", "develop")
	t.Setenv("PEGG_BITBUCKET_WORKTREE_ROOT", "wt")
	t.Setenv("PEGG_BITBUCKET_TIMEOUT_SECONDS", "45")

	settings := defaultSettings()
	settings.applyEnv()

	if settings.Enabled {
		t.Fatal("enabled should be false")
	}
	if settings.Kind != kindDataCenter {
		t.Fatalf("kind = %q", settings.Kind)
	}
	if settings.BaseURL != "https://bb.example.com" {
		t.Fatalf("base_url = %q", settings.BaseURL)
	}
	if settings.Workspace != "acme" || settings.Project != "PROJ" || settings.Repo != "widgets" {
		t.Fatalf("repo settings = %#v", settings)
	}
	if settings.Email != "bot@example.com" || settings.APIToken != "api-token" || settings.AccessToken != "access-token" {
		t.Fatalf("cloud credentials = %#v", settings)
	}
	if settings.Username != "admin" || settings.Password != "hunter2" || settings.Token != "dc-token" {
		t.Fatalf("data center credentials = %#v", settings)
	}
	if settings.DefaultBase != "develop" || settings.WorktreeRoot != "wt" || settings.RequestTimeoutSeconds != 45 {
		t.Fatalf("defaults = %#v", settings)
	}
}

func TestOptionKeys(t *testing.T) {
	clearBitbucketEnv(t)
	for _, key := range []string{"kanban-bitbucket", "pegg-kanban-bitbucket", "bitbucket", "pegg-bitbucket"} {
		settings := defaultSettings()
		settings.applyHost([]byte(`{"plugins":{"options":{"` + key + `":{"workspace":"acme","repo":"widgets"}}}}`))
		if settings.Workspace != "acme" || settings.Repo != "widgets" {
			t.Errorf("key %q not applied: %#v", key, settings)
		}
	}

	settings := defaultSettings()
	settings.applyHost([]byte(`{"plugins":{"options":{"bitbucket":{"workspace":"legacy"},"kanban-bitbucket":{"workspace":"canonical"}}}}`))
	if settings.Workspace != "canonical" {
		t.Fatalf("canonical key should win: workspace = %q", settings.Workspace)
	}

	settings = defaultSettings()
	settings.applyHost([]byte(`{"plugins":{"options":{"pegg-bitbucket":{"workspace":"legacy"}}}}`))
	if err := settings.applyOptions(json.RawMessage(`{"workspace":"deps"}`)); err != nil {
		t.Fatal(err)
	}
	if settings.Workspace != "deps" {
		t.Fatalf("options should override host: workspace = %q", settings.Workspace)
	}
}

func TestNormalizeKind(t *testing.T) {
	cases := map[string]string{
		"":            kindCloud,
		"cloud":       kindCloud,
		"Cloud":       kindCloud,
		"dc":          kindDataCenter,
		"server":      kindDataCenter,
		"datacenter":  kindDataCenter,
		"data-center": kindDataCenter,
		" DataCenter": kindDataCenter,
	}
	for input, want := range cases {
		if got := normalizeKind(input); got != want {
			t.Errorf("normalizeKind(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	cloudCases := map[string]string{
		"":                                defaultCloudBaseURL,
		"https://api.bitbucket.org/2.0/":  defaultCloudBaseURL,
		"https://api.bitbucket.org":       defaultCloudBaseURL,
		"http://localhost:8080":           "http://localhost:8080/2.0",
		"https://proxy.example.com/2.0":   "https://proxy.example.com/2.0",
		"https://proxy.example.com/v2/":   "https://proxy.example.com/v2",
		"https://bitbucket.example/2.0//": "https://bitbucket.example/2.0",
	}
	for input, want := range cloudCases {
		if got := normalizeBaseURL(kindCloud, input); got != want {
			t.Errorf("normalizeBaseURL(cloud, %q) = %q, want %q", input, got, want)
		}
	}

	dcCases := map[string]string{
		"":                            "",
		"https://bb.example.com/":     "https://bb.example.com",
		"https://bb.example.com":      "https://bb.example.com",
		"https://bb.example.com/rest": "https://bb.example.com/rest",
		defaultCloudBaseURL:           "",
	}
	for input, want := range dcCases {
		if got := normalizeBaseURL(kindDataCenter, input); got != want {
			t.Errorf("normalizeBaseURL(datacenter, %q) = %q, want %q", input, got, want)
		}
	}
}

func TestDataCenterRequiresBaseURL(t *testing.T) {
	clearBitbucketEnv(t)
	settings := defaultSettings()
	settings.Kind = "datacenter"
	settings.normalize()
	if settings.BaseURL != "" {
		t.Fatalf("data center should not inherit the Cloud base url: %q", settings.BaseURL)
	}
	if _, err := newClient(settings); err == nil || !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("error = %v, want a base_url hint", err)
	}
}

func TestNormalizeSettings(t *testing.T) {
	clearBitbucketEnv(t)
	settings := Settings{
		Kind:                  "server",
		BaseURL:               "https://bb.example.com/",
		WorktreeRoot:          "",
		RequestTimeoutSeconds: 0,
	}
	settings.normalize()
	if settings.Kind != kindDataCenter {
		t.Fatalf("kind = %q", settings.Kind)
	}
	if settings.BaseURL != "https://bb.example.com" {
		t.Fatalf("base_url = %q", settings.BaseURL)
	}
	if settings.WorktreeRoot != defaultWorktreeRoot {
		t.Fatalf("worktree_root = %q", settings.WorktreeRoot)
	}
	if settings.RequestTimeoutSeconds != 30 {
		t.Fatalf("timeout = %d", settings.RequestTimeoutSeconds)
	}
}

func TestRedactSettings(t *testing.T) {
	settings := Settings{
		APIToken:    "api-secret",
		AccessToken: "access-secret",
		Password:    "pass-secret",
		Token:       "dc-secret",
	}
	got := redactSettings("api-secret access-secret pass-secret dc-secret", settings)
	want := "[redacted] [redacted] [redacted] [redacted]"
	if got != want {
		t.Fatalf("redacted = %q, want %q", got, want)
	}
	if empty := redactSettings("nothing here", Settings{}); empty != "nothing here" {
		t.Fatalf("empty secrets changed message: %q", empty)
	}
}
