package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "init", "-b", "main")
	mustGit(t, dir, "config", "user.name", "Pegg Test")
	mustGit(t, dir, "config", "user.email", "pegg@example.com")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "README.md")
	mustGit(t, dir, "commit", "-m", "initial commit")
	return dir
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestWorktreeCreate(t *testing.T) {
	repo := initGitRepo(t)
	manager, err := NewWorktreeManager(repo, ".pegg-worktrees", "main")
	if err != nil {
		t.Fatal(err)
	}
	entry, err := manager.Create(CreateWorktreeOptions{
		Slug:   "Issue 42: Fix Login!",
		Branch: "pegg/issue-42-fix-login",
		Base:   "main",
		Issue:  42,
		CardID: "card-abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Branch != "pegg/issue-42-fix-login" || entry.Base != "main" {
		t.Fatalf("entry = %#v", entry)
	}
	if entry.Path != filepath.Join(repo, ".pegg-worktrees", "issue-42-fix-login") {
		t.Fatalf("path = %q", entry.Path)
	}
	if info, err := os.Stat(entry.Path); err != nil || !info.IsDir() {
		t.Fatalf("worktree path missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(entry.Path, "README.md")); err != nil {
		t.Fatalf("worktree checkout missing file: %v", err)
	}

	entries, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || !entries[0].Main || entries[1].Slug != entry.Slug {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestWorktreeSlug(t *testing.T) {
	card := Card{ID: "card-1234567890", Title: "Fix the login page"}
	if got := worktreeSlugForCard(card); got != "fix-the-login-page-12345678" {
		t.Fatalf("slug = %q", got)
	}
	if got := worktreeSlugForCard(Card{ID: "card-1"}); got != "card-1" {
		t.Fatalf("empty title slug = %q", got)
	}
}
