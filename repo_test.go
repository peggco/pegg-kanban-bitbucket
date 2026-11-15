package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRemote(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want repoRef
	}{
		{
			name: "cloud https",
			raw:  "https://bitbucket.org/acme/widgets.git",
			want: repoRef{Kind: kindCloud, Workspace: "acme", Repo: "widgets"},
		},
		{
			name: "cloud https without git suffix",
			raw:  "https://bitbucket.org/acme/widgets",
			want: repoRef{Kind: kindCloud, Workspace: "acme", Repo: "widgets"},
		},
		{
			name: "cloud ssh",
			raw:  "git@bitbucket.org:acme/widgets.git",
			want: repoRef{Kind: kindCloud, Workspace: "acme", Repo: "widgets"},
		},
		{
			name: "cloud ssh url",
			raw:  "ssh://git@bitbucket.org/acme/widgets.git",
			want: repoRef{Kind: kindCloud, Workspace: "acme", Repo: "widgets"},
		},
		{
			name: "data center https",
			raw:  "https://bitbucket.example.com/scm/PROJ/widgets.git",
			want: repoRef{Kind: kindDataCenter, BaseURL: "https://bitbucket.example.com", Project: "PROJ", Repo: "widgets"},
		},
		{
			name: "data center http",
			raw:  "http://bb.internal/scm/TEAM/app.git",
			want: repoRef{Kind: kindDataCenter, BaseURL: "http://bb.internal", Project: "TEAM", Repo: "app"},
		},
		{
			name: "data center ssh with port",
			raw:  "ssh://git@bitbucket.example.com:7999/PROJ/widgets.git",
			want: repoRef{Kind: kindDataCenter, BaseURL: "https://bitbucket.example.com", Project: "PROJ", Repo: "widgets"},
		},
		{
			name: "data center ssh without port",
			raw:  "ssh://git@bitbucket.example.com/PROJ/widgets.git",
			want: repoRef{Kind: kindDataCenter, BaseURL: "https://bitbucket.example.com", Project: "PROJ", Repo: "widgets"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseRemote(tc.raw)
			if !ok {
				t.Fatalf("parseRemote(%q) failed", tc.raw)
			}
			if got != tc.want {
				t.Fatalf("parseRemote(%q) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseRemoteRejectsGarbage(t *testing.T) {
	for _, raw := range []string{"", "   ", "https://bitbucket.org/onlyworkspace.git", "not-a-remote"} {
		if ref, ok := parseRemote(raw); ok {
			t.Errorf("parseRemote(%q) = %#v, want failure", raw, ref)
		}
	}
}

func TestResolveTargetFromSettings(t *testing.T) {
	clearBitbucketEnv(t)
	settings := defaultSettings()
	settings.Workspace = "acme"
	settings.Repo = "widgets"

	dir := initGitRepo(t)
	mustGit(t, dir, "remote", "add", "origin", "https://bitbucket.org/other/other.git")

	ref, err := resolveTarget(settings, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Kind != kindCloud || ref.Workspace != "acme" || ref.Repo != "widgets" {
		t.Fatalf("settings should win: %#v", ref)
	}

	settings.Kind = kindDataCenter
	settings.Project = "PROJ"
	settings.BaseURL = "https://bb.example.com"
	ref, err = resolveTarget(settings, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Kind != kindDataCenter || ref.Project != "PROJ" || ref.Repo != "widgets" || ref.BaseURL != "https://bb.example.com" {
		t.Fatalf("data center settings = %#v", ref)
	}
}

func TestResolveTargetFromRemote(t *testing.T) {
	clearBitbucketEnv(t)
	dir := initGitRepo(t)
	mustGit(t, dir, "remote", "add", "origin", "git@bitbucket.org:acme/widgets.git")

	settings := defaultSettings()
	settings.BaseURL = "https://api.bitbucket.org/2.0"
	ref, err := resolveTarget(settings, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Kind != kindCloud || ref.Workspace != "acme" || ref.Repo != "widgets" {
		t.Fatalf("cloud remote = %#v", ref)
	}
	if ref.BaseURL != "https://api.bitbucket.org/2.0" {
		t.Fatalf("base_url = %q", ref.BaseURL)
	}

	mustGit(t, dir, "remote", "set-url", "origin", "https://bitbucket.example.com/scm/PROJ/widgets.git")
	settings = defaultSettings()
	ref, err = resolveTarget(settings, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Kind != kindDataCenter || ref.Project != "PROJ" || ref.Repo != "widgets" {
		t.Fatalf("data center remote = %#v", ref)
	}
	if ref.BaseURL != "https://bitbucket.example.com" {
		t.Fatalf("inferred base_url = %q", ref.BaseURL)
	}
}

func TestResolveTargetPrefersWorktreePath(t *testing.T) {
	clearBitbucketEnv(t)
	settings := defaultSettings()
	settings.Workdir = t.TempDir()

	worktree := initGitRepo(t)
	mustGit(t, worktree, "remote", "add", "origin", "https://bitbucket.org/worktree/repo.git")

	ref, err := resolveTarget(settings, []string{worktree, settings.Workdir})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Workspace != "worktree" || ref.Repo != "repo" {
		t.Fatalf("worktree remote should win: %#v", ref)
	}
}

func TestResolveTargetMissing(t *testing.T) {
	clearBitbucketEnv(t)
	settings := defaultSettings()
	settings.Workdir = filepath.Join(t.TempDir(), "missing")
	settings.Repo = ""
	if err := os.MkdirAll(settings.Workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveTarget(settings, nil); err == nil {
		t.Fatal("expected an error when no repository can be resolved")
	}
}

func TestRepoRefValidate(t *testing.T) {
	valid := repoRef{Kind: kindCloud, Workspace: "acme", Repo: "widgets"}
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	if err := (repoRef{Kind: kindCloud, Repo: "widgets"}).validate(); err == nil {
		t.Fatal("expected missing workspace error")
	}
	if err := (repoRef{Kind: kindDataCenter, Repo: "widgets"}).validate(); err == nil {
		t.Fatal("expected missing project error")
	}
}
