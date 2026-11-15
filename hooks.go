package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	peggplugin "github.com/peggco/pegg/pkg/plugin"
)

const (
	HookRunPrepare    = "kanban.run.prepare"
	HookCardSucceeded = "kanban.card.succeeded"

	hookPriority = 10
)

type IssueRef struct {
	Provider string `json:"provider,omitempty"`
	Repo     string `json:"repo"`
	Number   int    `json:"number"`
	Key      string `json:"key,omitempty"`
	URL      string `json:"url"`
}

type Card struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Body      string         `json:"body,omitempty"`
	Column    string         `json:"column"`
	Position  int            `json:"position"`
	Priority  string         `json:"priority,omitempty"`
	Labels    []string       `json:"labels,omitempty"`
	Assignee  string         `json:"assignee,omitempty"`
	Source    string         `json:"source"`
	Issue     *IssueRef      `json:"issue,omitempty"`
	Worktree  *WorktreeEntry `json:"worktree,omitempty"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

type RunContext struct {
	Kind   string            `json:"kind,omitempty"`
	Path   string            `json:"path,omitempty"`
	Branch string            `json:"branch,omitempty"`
	Base   string            `json:"base,omitempty"`
	Label  string            `json:"label,omitempty"`
	Env    map[string]string `json:"env,omitempty"`
	Meta   map[string]string `json:"meta,omitempty"`
}

type RunPrepare struct {
	Card       Card       `json:"card"`
	Repo       string     `json:"repo"`
	BaseBranch string     `json:"base_branch,omitempty"`
	Context    RunContext `json:"context"`
}

type CardSucceeded struct {
	Card                Card           `json:"card"`
	Worktree            *WorktreeEntry `json:"worktree,omitempty"`
	Context             *RunContext    `json:"context,omitempty"`
	Summary             string         `json:"summary,omitempty"`
	Feedback            bool           `json:"feedback"`
	CreateChangeRequest bool           `json:"create_change_request"`
	TransitionOnSuccess string         `json:"transition_on_success,omitempty"`
	BaseBranch          string         `json:"base_branch,omitempty"`
}

type pullRequestResult struct {
	PullRequest    int64  `json:"pull_request"`
	PullRequestURL string `json:"pull_request_url"`
}

func (p *Plugin) Hooks() ([]peggplugin.HookSpec, error) {
	return []peggplugin.HookSpec{
		{
			Name:        HookRunPrepare,
			Kind:        peggplugin.HookFilter,
			Priority:    hookPriority,
			Description: "Create a git worktree for a Kanban card run.",
		},
		{
			Name:        HookCardSucceeded,
			Kind:        peggplugin.HookAction,
			Priority:    hookPriority,
			Description: "Open a Bitbucket pull request after a Kanban card succeeds.",
		},
	}, nil
}

func (p *Plugin) ExecuteHook(req peggplugin.HookRequest) (*peggplugin.HookResult, error) {
	settings := p.currentSettings()
	result, err := executeHook(settings, req)
	if err != nil {
		return nil, fmt.Errorf("%s", redactSettings(err.Error(), settings))
	}
	return result, nil
}

func executeHook(settings Settings, req peggplugin.HookRequest) (*peggplugin.HookResult, error) {
	switch req.Name {
	case HookRunPrepare:
		return hookRunPrepare(settings, req)
	case HookCardSucceeded:
		return hookCardSucceeded(settings, req)
	default:
		return &peggplugin.HookResult{Handled: false}, nil
	}
}

func hookRunPrepare(settings Settings, req peggplugin.HookRequest) (*peggplugin.HookResult, error) {
	var payload RunPrepare
	if len(req.Value) > 0 && string(req.Value) != "null" {
		if err := json.Unmarshal(req.Value, &payload); err != nil {
			return nil, fmt.Errorf("decode %s payload: %w", HookRunPrepare, err)
		}
	}
	if !settings.Enabled || strings.TrimSpace(payload.Repo) == "" {
		return &peggplugin.HookResult{Handled: false}, nil
	}

	base := strings.TrimSpace(payload.BaseBranch)
	if base == "" {
		base = strings.TrimSpace(settings.DefaultBase)
	}
	issue := 0
	if payload.Card.Issue != nil {
		issue = payload.Card.Issue.Number
	}
	slug := worktreeSlugForCard(payload.Card)
	manager, err := NewWorktreeManager(payload.Repo, settings.WorktreeRoot, settings.DefaultBase)
	if err != nil {
		return nil, err
	}
	entry, err := manager.Create(CreateWorktreeOptions{
		Slug:   slug,
		Branch: "pegg/" + slug,
		Base:   base,
		Issue:  issue,
		CardID: payload.Card.ID,
	})
	if err != nil {
		return nil, err
	}
	payload.Context = RunContext{
		Kind:   "worktree",
		Path:   entry.Path,
		Branch: entry.Branch,
		Base:   entry.Base,
		Label:  entry.Slug,
	}
	value, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode %s payload: %w", HookRunPrepare, err)
	}
	return &peggplugin.HookResult{Value: value, Handled: true}, nil
}

func worktreeSlugForCard(card Card) string {
	base := slugify(card.Title)
	if base == "" {
		base = "card"
	}
	if len(base) > 50 {
		base = strings.Trim(base[:50], "-")
	}
	suffix := card.ID
	if idx := strings.LastIndex(card.ID, "-"); idx >= 0 && idx+1 < len(card.ID) {
		suffix = card.ID[idx+1:]
	}
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	slug := slugify(base + "-" + suffix)
	if slug == "" {
		slug = "card"
	}
	return slug
}

func hookCardSucceeded(settings Settings, req peggplugin.HookRequest) (*peggplugin.HookResult, error) {
	var payload CardSucceeded
	if len(req.Value) > 0 && string(req.Value) != "null" {
		if err := json.Unmarshal(req.Value, &payload); err != nil {
			return nil, fmt.Errorf("decode %s payload: %w", HookCardSucceeded, err)
		}
	}
	if !settings.Enabled || !payload.CreateChangeRequest {
		return &peggplugin.HookResult{Handled: false}, nil
	}

	branch := changeRequestBranch(payload)
	if branch == "" {
		return &peggplugin.HookResult{Handled: false}, nil
	}

	target, err := resolveTarget(settings, worktreeDirs(payload, settings))
	if err != nil {
		return nil, err
	}
	originalKind := normalizeKind(settings.Kind)
	settings.Kind = target.Kind
	switch {
	case target.BaseURL != "":
		settings.BaseURL = target.BaseURL
	case originalKind != target.Kind:
		settings.BaseURL = ""
	}
	target.BaseURL = settings.BaseURL

	ctx, cancel := hookContext(req, settings)
	defer cancel()

	client, err := newClient(settings)
	if err != nil {
		return nil, err
	}

	base := changeRequestBase(payload, settings)
	if base == "" {
		base, err = client.defaultBranch(ctx, target)
		if err != nil {
			return nil, err
		}
	}

	title := strings.TrimSpace(payload.Card.Title)
	if title == "" {
		title = branch
	}

	created, err := client.createPullRequest(ctx, target, pullRequestInput{
		Title:        title,
		Description:  changeRequestBody(payload.Card.Body, payload.Summary),
		SourceBranch: branch,
		TargetBranch: base,
	})
	if err != nil {
		return nil, err
	}

	value, err := json.Marshal(pullRequestResult{
		PullRequest:    created.ID,
		PullRequestURL: created.URL,
	})
	if err != nil {
		return nil, fmt.Errorf("encode %s payload: %w", HookCardSucceeded, err)
	}
	return &peggplugin.HookResult{Value: value, Handled: true}, nil
}

func changeRequestBranch(payload CardSucceeded) string {
	if payload.Context != nil {
		if branch := strings.TrimSpace(payload.Context.Branch); branch != "" {
			return branch
		}
	}
	if payload.Worktree != nil {
		if branch := strings.TrimSpace(payload.Worktree.Branch); branch != "" {
			return branch
		}
	}
	if payload.Card.Worktree != nil {
		return strings.TrimSpace(payload.Card.Worktree.Branch)
	}
	return ""
}

func changeRequestBase(payload CardSucceeded, settings Settings) string {
	if payload.Context != nil {
		if base := strings.TrimSpace(payload.Context.Base); base != "" {
			return base
		}
	}
	if payload.Worktree != nil {
		if base := strings.TrimSpace(payload.Worktree.Base); base != "" {
			return base
		}
	}
	if payload.Card.Worktree != nil {
		if base := strings.TrimSpace(payload.Card.Worktree.Base); base != "" {
			return base
		}
	}
	if base := strings.TrimSpace(payload.BaseBranch); base != "" {
		return base
	}
	return strings.TrimSpace(settings.DefaultBase)
}

func worktreeDirs(payload CardSucceeded, settings Settings) []string {
	dirs := make([]string, 0, 5)
	add := func(path string) {
		if path = strings.TrimSpace(path); path != "" {
			dirs = append(dirs, path)
		}
	}
	if payload.Context != nil {
		add(payload.Context.Path)
	}
	if payload.Worktree != nil {
		add(payload.Worktree.Path)
	}
	if payload.Card.Worktree != nil {
		add(payload.Card.Worktree.Path)
	}
	add(settings.Repo)
	add(settings.Workdir)
	return dirs
}

func changeRequestBody(body, summary string) string {
	body = strings.TrimSpace(body)
	summary = strings.TrimSpace(summary)
	switch {
	case body == "":
		return summary
	case summary == "":
		return body
	default:
		return body + "\n\n" + summary
	}
}

func hookContext(req peggplugin.HookRequest, settings Settings) (context.Context, context.CancelFunc) {
	timeout := settings.timeout()
	if req.TimeoutMS > 0 {
		timeout = time.Duration(req.TimeoutMS) * time.Millisecond
	}
	return context.WithTimeout(context.Background(), timeout)
}
