package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	userAgent       = "pegg-kanban-bitbucket-plugin"
	maxResponseSize = 8 << 20
	maxErrorBody    = 64 << 10
)

type Client struct {
	kind    string
	baseURL string
	auth    string
	http    *http.Client
	secrets []string
}

func newClient(settings Settings) (*Client, error) {
	kind := normalizeKind(settings.Kind)
	baseURL := strings.TrimRight(strings.TrimSpace(settings.BaseURL), "/")
	secrets := settings.secrets()

	var auth string
	switch kind {
	case kindCloud:
		if baseURL == "" {
			baseURL = defaultCloudBaseURL
		}
		switch {
		case settings.AccessToken != "":
			auth = "Bearer " + settings.AccessToken
		case settings.Email != "" && settings.APIToken != "":
			auth = "Basic " + basicAuth(settings.Email, settings.APIToken)
		default:
			return nil, fmt.Errorf("bitbucket cloud authentication is required: set access_token (PEGG_BITBUCKET_ACCESS_TOKEN) or email and api_token (PEGG_BITBUCKET_EMAIL, PEGG_BITBUCKET_API_TOKEN)")
		}
	case kindDataCenter:
		if baseURL == "" {
			return nil, fmt.Errorf("bitbucket data center base_url is required: set base_url (PEGG_BITBUCKET_URL) or the plugin option base_url")
		}
		switch {
		case settings.Token != "":
			auth = "Bearer " + settings.Token
		case settings.Username != "" && settings.Password != "":
			auth = "Basic " + basicAuth(settings.Username, settings.Password)
		default:
			return nil, fmt.Errorf("bitbucket data center authentication is required: set token (PEGG_BITBUCKET_TOKEN) or username and password (PEGG_BITBUCKET_USERNAME, PEGG_BITBUCKET_PASSWORD)")
		}
	default:
		return nil, fmt.Errorf("unsupported bitbucket kind %q: use %q or %q", settings.Kind, kindCloud, kindDataCenter)
	}

	return &Client{
		kind:    kind,
		baseURL: baseURL,
		auth:    auth,
		http:    &http.Client{Timeout: settings.timeout()},
		secrets: secrets,
	}, nil
}

func basicAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}

func (c *Client) redact(message string) string {
	for _, secret := range c.secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return message
}

type apiResponse struct {
	status int
	body   []byte
}

func (c *Client) request(ctx context.Context, method, path string, payload any) (*apiResponse, error) {
	target := c.baseURL + path
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if c.auth != "" {
		req.Header.Set("Authorization", c.auth)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %s", method, path, c.redact(err.Error()))
	}
	defer resp.Body.Close()

	limit := int64(maxResponseSize)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limit = maxErrorBody
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("read response: %s", c.redact(err.Error()))
	}
	out := &apiResponse{status: resp.StatusCode, body: data}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, c.apiError(method, path, resp.StatusCode, data)
	}
	return out, nil
}

func (c *Client) apiError(method, path string, status int, body []byte) error {
	message := extractMessage(body)
	if len(message) > 400 {
		message = message[:400] + "..."
	}
	base := fmt.Sprintf("Bitbucket API %s %s: HTTP %d", method, path, status)
	if message != "" {
		base += ": " + message
	}
	switch status {
	case http.StatusUnauthorized:
		base += " (authentication failed; check the configured Bitbucket credentials)"
	case http.StatusForbidden:
		base += " (the credentials lack permission to create pull requests in this repository)"
	case http.StatusNotFound:
		base += " (check the workspace or project, repository slug, and branch names)"
	case http.StatusConflict:
		base += " (a pull request for this branch may already exist; close or merge it before retrying)"
	case http.StatusTooManyRequests:
		base += " (rate limited; retry later)"
	case http.StatusBadRequest:
		if looksLikeDuplicatePullRequest(message) {
			base += " (a pull request for this branch may already exist; close or merge it before retrying)"
		}
	}
	return fmt.Errorf("%s", c.redact(base))
}

func looksLikeDuplicatePullRequest(message string) bool {
	lower := strings.ToLower(message)
	if !strings.Contains(lower, "pull request") {
		return false
	}
	for _, needle := range []string{"already exist", "already exists", "duplicate", "active pull request"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func extractMessage(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	var cloud struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &cloud); err == nil && cloud.Error.Message != "" {
		return cloud.Error.Message
	}
	var datacenter struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &datacenter); err == nil && len(datacenter.Errors) > 0 {
		parts := make([]string, 0, len(datacenter.Errors))
		for _, item := range datacenter.Errors {
			if item.Message != "" {
				parts = append(parts, item.Message)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}
	var plain struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &plain); err == nil {
		if plain.Message != "" {
			return plain.Message
		}
		if plain.Error != "" {
			return plain.Error
		}
	}
	if len(trimmed) > 400 {
		return trimmed[:400]
	}
	return trimmed
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	resp, err := c.request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if len(resp.body) == 0 {
		return nil
	}
	if err := json.Unmarshal(resp.body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) postJSON(ctx context.Context, path string, payload any, out any) error {
	resp, err := c.request(ctx, http.MethodPost, path, payload)
	if err != nil {
		return err
	}
	if len(resp.body) == 0 {
		return nil
	}
	if err := json.Unmarshal(resp.body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) repoPath(ref repoRef) (string, error) {
	if err := ref.validate(); err != nil {
		return "", err
	}
	switch c.kind {
	case kindDataCenter:
		return "/rest/api/1.0/projects/" + url.PathEscape(ref.Project) + "/repos/" + url.PathEscape(ref.Repo), nil
	default:
		return "/repositories/" + url.PathEscape(ref.Workspace) + "/" + url.PathEscape(ref.Repo), nil
	}
}

func (c *Client) defaultBranch(ctx context.Context, ref repoRef) (string, error) {
	path, err := c.repoPath(ref)
	if err != nil {
		return "", err
	}
	branch := ""
	switch c.kind {
	case kindDataCenter:
		var out struct {
			DisplayID string `json:"displayId"`
			ID        string `json:"id"`
		}
		if err := c.getJSON(ctx, path+"/default-branch", &out); err != nil {
			return "", fmt.Errorf("resolve default branch for %s: %w", ref.label(), err)
		}
		branch = firstNonEmpty(out.DisplayID, trimBranchRef(out.ID))
	default:
		var out struct {
			MainBranch struct {
				Name string `json:"name"`
			} `json:"mainbranch"`
		}
		if err := c.getJSON(ctx, path, &out); err != nil {
			return "", fmt.Errorf("resolve default branch for %s: %w", ref.label(), err)
		}
		branch = trimBranchRef(out.MainBranch.Name)
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", fmt.Errorf("default branch for %s is empty: set base_branch or default_base", ref.label())
	}
	return branch, nil
}

type pullRequestInput struct {
	Title        string
	Description  string
	SourceBranch string
	TargetBranch string
}

type pullRequestCreated struct {
	ID  int64
	URL string
}

func (c *Client) createPullRequest(ctx context.Context, ref repoRef, input pullRequestInput) (pullRequestCreated, error) {
	path, err := c.repoPath(ref)
	if err != nil {
		return pullRequestCreated{}, err
	}
	switch c.kind {
	case kindDataCenter:
		return c.createDataCenterPullRequest(ctx, path, ref, input)
	default:
		return c.createCloudPullRequest(ctx, path, input)
	}
}

func (c *Client) createCloudPullRequest(ctx context.Context, path string, input pullRequestInput) (pullRequestCreated, error) {
	payload := struct {
		Title             string    `json:"title"`
		Description       string    `json:"description"`
		Source            branchRef `json:"source"`
		Destination       branchRef `json:"destination"`
		CloseSourceBranch bool      `json:"close_source_branch"`
	}{
		Title:             input.Title,
		Description:       input.Description,
		Source:            branchRef{Branch: branchName{Name: input.SourceBranch}},
		Destination:       branchRef{Branch: branchName{Name: input.TargetBranch}},
		CloseSourceBranch: false,
	}
	var out struct {
		ID    int64 `json:"id"`
		Links struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
	}
	if err := c.postJSON(ctx, path+"/pullrequests", payload, &out); err != nil {
		return pullRequestCreated{}, err
	}
	return pullRequestCreated{ID: out.ID, URL: out.Links.HTML.Href}, nil
}

func (c *Client) createDataCenterPullRequest(ctx context.Context, path string, ref repoRef, input pullRequestInput) (pullRequestCreated, error) {
	repository := dcRepository{Slug: ref.Repo}
	repository.Project.Key = ref.Project

	payload := struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		FromRef     dcRef  `json:"fromRef"`
		ToRef       dcRef  `json:"toRef"`
	}{
		Title:       input.Title,
		Description: input.Description,
		FromRef:     dcRef{ID: "refs/heads/" + input.SourceBranch, Repository: repository},
		ToRef:       dcRef{ID: "refs/heads/" + input.TargetBranch, Repository: repository},
	}
	var out struct {
		ID    int64 `json:"id"`
		Links struct {
			Self []struct {
				Href string `json:"href"`
			} `json:"self"`
		} `json:"links"`
	}
	if err := c.postJSON(ctx, path+"/pull-requests", payload, &out); err != nil {
		return pullRequestCreated{}, err
	}
	result := pullRequestCreated{ID: out.ID}
	if len(out.Links.Self) > 0 {
		result.URL = out.Links.Self[0].Href
	}
	return result, nil
}

func trimBranchRef(value string) string {
	value = strings.TrimSpace(value)
	return strings.TrimPrefix(value, "refs/heads/")
}

type branchName struct {
	Name string `json:"name"`
}

type branchRef struct {
	Branch branchName `json:"branch"`
}

type dcRepository struct {
	Slug    string `json:"slug"`
	Project struct {
		Key string `json:"key"`
	} `json:"project"`
}

type dcRef struct {
	ID         string       `json:"id"`
	Repository dcRepository `json:"repository"`
}
