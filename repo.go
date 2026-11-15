package main

import (
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
)

type repoRef struct {
	Kind      string
	BaseURL   string
	Workspace string
	Project   string
	Repo      string
}

func (r repoRef) label() string {
	switch r.Kind {
	case kindDataCenter:
		return r.Project + "/" + r.Repo
	default:
		return r.Workspace + "/" + r.Repo
	}
}

func (r repoRef) validate() error {
	if !validRepoSegment(r.Repo) {
		return fmt.Errorf("bitbucket repository slug is required: set repo or configure a Bitbucket origin remote")
	}
	switch r.Kind {
	case kindDataCenter:
		if !validRepoSegment(r.Project) {
			return fmt.Errorf("bitbucket data center project key is required: set project or configure a Bitbucket origin remote")
		}
	case kindCloud:
		if !validRepoSegment(r.Workspace) {
			return fmt.Errorf("bitbucket cloud workspace is required: set workspace or configure a Bitbucket origin remote")
		}
	}
	return nil
}

func resolveTarget(settings Settings, dirs []string) (repoRef, error) {
	if ref, ok := settingsTarget(settings); ok {
		return ref, nil
	}
	for _, dir := range dirs {
		raw := gitRemoteURL(dir)
		if raw == "" {
			continue
		}
		ref, ok := parseRemote(raw)
		if !ok {
			continue
		}
		if ref.BaseURL == "" && settings.BaseURL != "" {
			ref.BaseURL = settings.BaseURL
		}
		return ref, nil
	}
	return repoRef{}, fmt.Errorf("bitbucket repository is required: set workspace and repo (Cloud) or project and repo (Data Center), or add a Bitbucket origin remote")
}

func settingsTarget(settings Settings) (repoRef, bool) {
	repo := strings.TrimSpace(settings.Repo)
	if !validRepoSegment(repo) {
		return repoRef{}, false
	}
	if settings.Kind == kindDataCenter {
		project := firstNonEmpty(settings.Project, settings.Workspace)
		if !validRepoSegment(project) {
			return repoRef{}, false
		}
		return repoRef{Kind: kindDataCenter, BaseURL: settings.BaseURL, Project: project, Repo: repo}, true
	}
	workspace := firstNonEmpty(settings.Workspace, settings.Project)
	if !validRepoSegment(workspace) {
		return repoRef{}, false
	}
	return repoRef{Kind: kindCloud, BaseURL: settings.BaseURL, Workspace: workspace, Repo: repo}, true
}

var reScpRemote = regexp.MustCompile(`^(?:[^@/\s]+@)?([^:/\s]+):(.+)$`)

func parseRemote(raw string) (repoRef, bool) {
	host, remotePath, scheme, ok := splitRemote(raw)
	if !ok {
		return repoRef{}, false
	}
	path := strings.Trim(strings.TrimSpace(remotePath), "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")
	if path == "" {
		return repoRef{}, false
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return repoRef{}, false
	}

	if strings.EqualFold(host, "bitbucket.org") {
		workspace, repo := parts[0], parts[1]
		if !validRepoSegment(workspace) || !validRepoSegment(repo) {
			return repoRef{}, false
		}
		return repoRef{Kind: kindCloud, Workspace: workspace, Repo: repo}, true
	}

	if len(parts) >= 3 && strings.EqualFold(parts[0], "scm") {
		parts = parts[1:]
	}
	project, repo := parts[0], parts[1]
	if !validRepoSegment(project) || !validRepoSegment(repo) {
		return repoRef{}, false
	}
	base := "https://" + host
	if scheme == "http" || scheme == "https" {
		base = scheme + "://" + host
	}
	return repoRef{Kind: kindDataCenter, BaseURL: base, Project: project, Repo: repo}, true
}

func splitRemote(raw string) (host, path, scheme string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", false
	}
	if strings.HasPrefix(raw, "ssh://") {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Hostname() == "" {
			return "", "", "", false
		}
		return parsed.Hostname(), parsed.Path, "https", true
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" {
			return "", "", "", false
		}
		return parsed.Host, parsed.Path, parsed.Scheme, true
	}
	if match := reScpRemote.FindStringSubmatch(raw); match != nil {
		return match[1], match[2], "https", true
	}
	return "", "", "", false
}

func validRepoSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	return !strings.ContainsAny(value, " \t\r\n/")
}

func gitRemoteURL(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	if out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output(); err == nil {
		if remote := strings.TrimSpace(string(out)); remote != "" {
			return remote
		}
	}
	out, err := exec.Command("git", "-C", dir, "remote").Output()
	if err != nil {
		return ""
	}
	for _, name := range strings.Fields(string(out)) {
		if remote, err := exec.Command("git", "-C", dir, "remote", "get-url", name).Output(); err == nil {
			if value := strings.TrimSpace(string(remote)); value != "" {
				return value
			}
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
