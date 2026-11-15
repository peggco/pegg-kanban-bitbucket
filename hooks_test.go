package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	peggplugin "github.com/peggco/pegg/pkg/plugin"
)

func executeHookT(t *testing.T, plugin *Plugin, name string, kind peggplugin.HookKind, value any) *peggplugin.HookResult {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	result, err := plugin.ExecuteHook(peggplugin.HookRequest{Name: name, Kind: kind, Value: raw, TimeoutMS: 5000})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatalf("%s returned nil result", name)
	}
	return result
}

func TestHooksSpecsAndCapability(t *testing.T) {
	plugin := &Plugin{}
	specs, err := plugin.Hooks()
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("specs = %#v", specs)
	}
	want := map[string]peggplugin.HookKind{
		HookRunPrepare:    peggplugin.HookFilter,
		HookCardSucceeded: peggplugin.HookAction,
	}
	for _, spec := range specs {
		kind, ok := want[spec.Name]
		if !ok {
			t.Fatalf("unexpected spec %#v", spec)
		}
		if spec.Kind != kind {
			t.Errorf("%s kind = %q, want %q", spec.Name, spec.Kind, kind)
		}
		if spec.Priority != 10 {
			t.Errorf("%s priority = %d, want 10", spec.Name, spec.Priority)
		}
	}

	capabilities := plugin.Capabilities()
	if len(capabilities) != 1 || capabilities[0] != string(peggplugin.CapabilityHooks) {
		t.Fatalf("capabilities = %#v", capabilities)
	}
	if peggplugin.Supports(capabilities, peggplugin.CapabilityTools) ||
		peggplugin.Supports(capabilities, peggplugin.CapabilitySkills) ||
		peggplugin.Supports(capabilities, peggplugin.CapabilityCommands) ||
		peggplugin.Supports(capabilities, peggplugin.CapabilityTUI) {
		t.Fatalf("hooks-only plugin advertises extra capabilities: %#v", capabilities)
	}
	if plugin.Name() != pluginID {
		t.Fatalf("name = %q", plugin.Name())
	}
}

func TestRunPrepareCreatesWorktree(t *testing.T) {
	repo := initGitRepo(t)
	plugin := &Plugin{settings: Settings{
		Enabled:      true,
		WorktreeRoot: ".pegg-worktrees",
		DefaultBase:  "main",
	}}

	result := executeHookT(t, plugin, HookRunPrepare, peggplugin.HookFilter, RunPrepare{
		Card:       Card{ID: "card-abc", Title: "Fix login"},
		Repo:       repo,
		BaseBranch: "main",
		Context:    RunContext{Kind: "repo", Path: repo},
	})
	if !result.Handled {
		t.Fatal("run prepare was not handled")
	}
	var prepared RunPrepare
	if err := json.Unmarshal(result.Value, &prepared); err != nil {
		t.Fatal(err)
	}
	if prepared.Context.Kind != "worktree" {
		t.Fatalf("context kind = %q", prepared.Context.Kind)
	}
	if !strings.Contains(prepared.Context.Path, ".pegg-worktrees") || prepared.Context.Path == "" {
		t.Fatalf("context path = %q", prepared.Context.Path)
	}
	if prepared.Context.Branch != "pegg/fix-login-abc" {
		t.Fatalf("branch = %q", prepared.Context.Branch)
	}
	if prepared.Context.Base != "main" || prepared.Context.Label != "fix-login-abc" {
		t.Fatalf("context = %#v", prepared.Context)
	}
	if _, err := os.Stat(prepared.Context.Path); err != nil {
		t.Fatalf("worktree missing: %v", err)
	}
}

func TestRunPrepareNoOp(t *testing.T) {
	plugin := &Plugin{settings: Settings{Enabled: false}}
	result := executeHookT(t, plugin, HookRunPrepare, peggplugin.HookFilter, RunPrepare{
		Card: Card{ID: "card-abc", Title: "Fix login"},
		Repo: "/tmp/repo",
	})
	if result.Handled {
		t.Fatal("disabled plugin should not handle run prepare")
	}

	plugin.settings.Enabled = true
	result = executeHookT(t, plugin, HookRunPrepare, peggplugin.HookFilter, RunPrepare{
		Card: Card{ID: "card-abc", Title: "Fix login"},
	})
	if result.Handled {
		t.Fatal("empty repo should not be handled")
	}
}

func TestCardSucceededCloudCreatesPullRequest(t *testing.T) {
	var lookedUp, created bool
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("bot@example.com:api-token"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != basic {
			t.Errorf("authorization = %q, want %q", got, basic)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("accept = %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/repositories/acme/widgets":
			lookedUp = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"mainbranch":{"name":"main"}}`))
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/repositories/acme/widgets/pullrequests":
			created = true
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("content type = %q", got)
			}
			var body struct {
				Title       string `json:"title"`
				Description string `json:"description"`
				Source      struct {
					Branch struct {
						Name string `json:"name"`
					} `json:"branch"`
				} `json:"source"`
				Destination struct {
					Branch struct {
						Name string `json:"name"`
					} `json:"branch"`
				} `json:"destination"`
				CloseSourceBranch bool `json:"close_source_branch"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Title != "Fix widget" {
				t.Errorf("title = %q", body.Title)
			}
			if body.Description != "Fixes the widget.\n\nAll tests passed." {
				t.Errorf("description = %q", body.Description)
			}
			if body.Source.Branch.Name != "pegg/fix-widget" {
				t.Errorf("source = %q", body.Source.Branch.Name)
			}
			if body.Destination.Branch.Name != "main" {
				t.Errorf("destination = %q", body.Destination.Branch.Name)
			}
			if body.CloseSourceBranch {
				t.Error("close_source_branch should be false")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":42,"links":{"html":{"href":"https://bitbucket.org/acme/widgets/pull-requests/42"}}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	plugin := &Plugin{settings: Settings{
		Enabled:   true,
		Kind:      kindCloud,
		BaseURL:   server.URL,
		Workspace: "acme",
		Repo:      "widgets",
		Email:     "bot@example.com",
		APIToken:  "api-token",
	}}

	result := executeHookT(t, plugin, HookCardSucceeded, peggplugin.HookAction, CardSucceeded{
		Card: Card{
			ID:    "card-1",
			Title: "Fix widget",
			Body:  "Fixes the widget.",
			Issue: &IssueRef{Provider: "jira", Repo: "PROJ", Number: 12, Key: "PROJ-12"},
		},
		Context:             &RunContext{Kind: "worktree", Branch: "pegg/fix-widget"},
		Summary:             "All tests passed.",
		CreateChangeRequest: true,
	})
	if !result.Handled {
		t.Fatal("card succeeded was not handled")
	}
	if !lookedUp || !created {
		t.Fatalf("lookedUp = %t, created = %t", lookedUp, created)
	}
	var pr pullRequestResult
	if err := json.Unmarshal(result.Value, &pr); err != nil {
		t.Fatal(err)
	}
	if pr.PullRequest != 42 || pr.PullRequestURL != "https://bitbucket.org/acme/widgets/pull-requests/42" {
		t.Fatalf("result = %#v", pr)
	}
}

func TestCardSucceededDataCenterCreatesPullRequest(t *testing.T) {
	var lookedUp, created bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer dc-token" {
			t.Errorf("authorization = %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/rest/api/1.0/projects/PROJ/repos/widgets/default-branch":
			lookedUp = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"displayId":"main","id":"refs/heads/main"}`))
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/rest/api/1.0/projects/PROJ/repos/widgets/pull-requests":
			created = true
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("content type = %q", got)
			}
			var body struct {
				Title       string `json:"title"`
				Description string `json:"description"`
				FromRef     struct {
					ID         string `json:"id"`
					Repository struct {
						Slug    string `json:"slug"`
						Project struct {
							Key string `json:"key"`
						} `json:"project"`
					} `json:"repository"`
				} `json:"fromRef"`
				ToRef struct {
					ID         string `json:"id"`
					Repository struct {
						Slug    string `json:"slug"`
						Project struct {
							Key string `json:"key"`
						} `json:"project"`
					} `json:"repository"`
				} `json:"toRef"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Title != "Fix widget" || body.Description != "Fixes the widget." {
				t.Errorf("title/description = %q / %q", body.Title, body.Description)
			}
			if body.FromRef.ID != "refs/heads/pegg/fix-widget" {
				t.Errorf("fromRef = %q", body.FromRef.ID)
			}
			if body.ToRef.ID != "refs/heads/main" {
				t.Errorf("toRef = %q", body.ToRef.ID)
			}
			if body.FromRef.Repository.Slug != "widgets" || body.FromRef.Repository.Project.Key != "PROJ" {
				t.Errorf("fromRef repository = %#v", body.FromRef.Repository)
			}
			if body.ToRef.Repository.Slug != "widgets" || body.ToRef.Repository.Project.Key != "PROJ" {
				t.Errorf("toRef repository = %#v", body.ToRef.Repository)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":7,"links":{"self":[{"href":"https://bb.example.com/projects/PROJ/repos/widgets/pull-requests/7"}]}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	plugin := &Plugin{settings: Settings{
		Enabled:  true,
		Kind:     kindDataCenter,
		BaseURL:  server.URL,
		Project:  "PROJ",
		Repo:     "widgets",
		Token:    "dc-token",
		Username: "admin",
		Password: "never-used",
	}}

	result := executeHookT(t, plugin, HookCardSucceeded, peggplugin.HookAction, CardSucceeded{
		Card: Card{
			ID:    "card-2",
			Title: "Fix widget",
			Body:  "Fixes the widget.",
			Issue: &IssueRef{Provider: "jira", Repo: "PROJ", Number: 7},
		},
		Worktree:            &WorktreeEntry{Path: t.TempDir(), Branch: "pegg/fix-widget"},
		CreateChangeRequest: true,
	})
	if !result.Handled {
		t.Fatal("card succeeded was not handled")
	}
	if !lookedUp || !created {
		t.Fatalf("lookedUp = %t, created = %t", lookedUp, created)
	}
	var pr pullRequestResult
	if err := json.Unmarshal(result.Value, &pr); err != nil {
		t.Fatal(err)
	}
	if pr.PullRequest != 7 || pr.PullRequestURL != "https://bb.example.com/projects/PROJ/repos/widgets/pull-requests/7" {
		t.Fatalf("result = %#v", pr)
	}
}

func TestCardSucceededResolvesRemote(t *testing.T) {
	repo := initGitRepo(t)
	mustGit(t, repo, "remote", "add", "origin", "git@bitbucket.org:acme/widgets.git")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.EscapedPath() == "/repositories/acme/widgets/pullrequests" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":5,"links":{"html":{"href":"https://bitbucket.org/acme/widgets/pull-requests/5"}}}`))
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	plugin := &Plugin{settings: Settings{
		Enabled:     true,
		Kind:        kindCloud,
		BaseURL:     server.URL,
		Email:       "bot@example.com",
		APIToken:    "api-token",
		DefaultBase: "main",
	}}

	result := executeHookT(t, plugin, HookCardSucceeded, peggplugin.HookAction, CardSucceeded{
		Card:                Card{ID: "card-3", Title: "Remote resolution"},
		Context:             &RunContext{Kind: "worktree", Path: repo, Branch: "pegg/remote-resolution"},
		CreateChangeRequest: true,
	})
	if !result.Handled {
		t.Fatal("card succeeded was not handled")
	}
}

func TestCardSucceededUnhandled(t *testing.T) {
	settings := Settings{
		Enabled:   true,
		Kind:      kindCloud,
		BaseURL:   "http://127.0.0.1:1",
		Workspace: "acme",
		Repo:      "widgets",
		Email:     "bot@example.com",
		APIToken:  "api-token",
	}
	payload := CardSucceeded{
		Card:                Card{ID: "card-1", Title: "Fix widget"},
		Context:             &RunContext{Branch: "pegg/fix-widget"},
		CreateChangeRequest: true,
	}

	cases := []struct {
		name   string
		mutate func(*Settings, *CardSucceeded)
	}{
		{
			name: "disabled",
			mutate: func(settings *Settings, _ *CardSucceeded) {
				settings.Enabled = false
			},
		},
		{
			name: "change request disabled",
			mutate: func(_ *Settings, payload *CardSucceeded) {
				payload.CreateChangeRequest = false
			},
		},
		{
			name: "no branch",
			mutate: func(_ *Settings, payload *CardSucceeded) {
				payload.Context = nil
				payload.Worktree = nil
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := settings
			payload := payload
			tc.mutate(&settings, &payload)
			plugin := &Plugin{settings: settings}
			result := executeHookT(t, plugin, HookCardSucceeded, peggplugin.HookAction, payload)
			if result.Handled {
				t.Fatal("hook should not be handled")
			}
			if len(result.Value) != 0 {
				t.Fatalf("unexpected value: %s", result.Value)
			}
		})
	}
}

func TestCardSucceededRedactsSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"message":"bad credentials dc-secret-token super-secret-password"}]}`))
	}))
	t.Cleanup(server.Close)

	plugin := &Plugin{settings: Settings{
		Enabled:  true,
		Kind:     kindDataCenter,
		BaseURL:  server.URL,
		Project:  "PROJ",
		Repo:     "widgets",
		Token:    "dc-secret-token",
		Username: "admin",
		Password: "super-secret-password",
	}}

	raw, err := json.Marshal(CardSucceeded{
		Card:                Card{ID: "card-1", Title: "Fix widget"},
		Context:             &RunContext{Branch: "pegg/fix-widget", Base: "main"},
		CreateChangeRequest: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = plugin.ExecuteHook(peggplugin.HookRequest{Name: HookCardSucceeded, Kind: peggplugin.HookAction, Value: raw, TimeoutMS: 5000})
	if err == nil {
		t.Fatal("expected an authentication error")
	}
	message := err.Error()
	if !strings.Contains(message, "[redacted]") {
		t.Fatalf("error was not redacted: %q", message)
	}
	for _, secret := range []string{"dc-secret-token", "super-secret-password"} {
		if strings.Contains(message, secret) {
			t.Fatalf("error leaked %q: %q", secret, message)
		}
	}
	if !strings.Contains(message, "HTTP 401") {
		t.Fatalf("error missing status: %q", message)
	}
}

func TestAuthPrecedence(t *testing.T) {
	cloudBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte("bot@example.com:api-token"))
	dcBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:hunter2"))

	cases := []struct {
		name     string
		settings Settings
		want     string
	}{
		{
			name: "cloud access token wins",
			settings: Settings{
				Kind:        kindCloud,
				AccessToken: "access-token",
				Email:       "bot@example.com",
				APIToken:    "api-token",
			},
			want: "Bearer access-token",
		},
		{
			name:     "cloud basic",
			settings: Settings{Kind: kindCloud, Email: "bot@example.com", APIToken: "api-token"},
			want:     cloudBasic,
		},
		{
			name: "data center token wins",
			settings: Settings{
				Kind:     kindDataCenter,
				BaseURL:  "https://bb.example.com",
				Token:    "dc-token",
				Username: "admin",
				Password: "hunter2",
			},
			want: "Bearer dc-token",
		},
		{
			name:     "data center basic",
			settings: Settings{Kind: kindDataCenter, BaseURL: "https://bb.example.com", Username: "admin", Password: "hunter2"},
			want:     dcBasic,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := newClient(tc.settings)
			if err != nil {
				t.Fatal(err)
			}
			if client.auth != tc.want {
				t.Fatalf("auth = %q, want %q", client.auth, tc.want)
			}
		})
	}
}

func TestAuthErrorsAreActionable(t *testing.T) {
	cases := []struct {
		name     string
		settings Settings
		want     string
	}{
		{
			name:     "cloud missing credentials",
			settings: Settings{Kind: kindCloud},
			want:     "PEGG_BITBUCKET_ACCESS_TOKEN",
		},
		{
			name:     "data center missing base url",
			settings: Settings{Kind: kindDataCenter, Token: "dc-token"},
			want:     "base_url",
		},
		{
			name:     "data center missing credentials",
			settings: Settings{Kind: kindDataCenter, BaseURL: "https://bb.example.com"},
			want:     "PEGG_BITBUCKET_TOKEN",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newClient(tc.settings)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want mention of %q", err, tc.want)
			}
		})
	}
}

func TestChangeRequestBranchAndBasePrecedence(t *testing.T) {
	payload := CardSucceeded{
		Card:       Card{Worktree: &WorktreeEntry{Branch: "pegg/card", Base: "card-base"}},
		Worktree:   &WorktreeEntry{Branch: "pegg/worktree", Base: "worktree-base"},
		Context:    &RunContext{Branch: "pegg/context", Base: "context-base"},
		BaseBranch: "payload-base",
	}
	if got := changeRequestBranch(payload); got != "pegg/context" {
		t.Fatalf("branch = %q", got)
	}
	if got := changeRequestBase(payload, Settings{DefaultBase: "default-base"}); got != "context-base" {
		t.Fatalf("base = %q", got)
	}

	payload.Context = nil
	if got := changeRequestBase(payload, Settings{DefaultBase: "default-base"}); got != "worktree-base" {
		t.Fatalf("worktree base = %q", got)
	}

	payload.Worktree = nil
	if got := changeRequestBase(payload, Settings{DefaultBase: "default-base"}); got != "card-base" {
		t.Fatalf("card worktree base = %q", got)
	}

	payload.Card.Worktree = nil
	if got := changeRequestBase(payload, Settings{DefaultBase: "default-base"}); got != "payload-base" {
		t.Fatalf("payload base = %q", got)
	}

	payload.BaseBranch = ""
	if got := changeRequestBase(payload, Settings{DefaultBase: "default-base"}); got != "default-base" {
		t.Fatalf("default base = %q", got)
	}

	if got := changeRequestBase(payload, Settings{}); got != "" {
		t.Fatalf("empty base = %q", got)
	}

	payload.Card.Worktree = nil
	if got := changeRequestBranch(payload); got != "" {
		t.Fatalf("branch after clearing worktrees = %q", got)
	}
}
