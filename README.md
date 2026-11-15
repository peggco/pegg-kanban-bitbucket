<div align="center">

<img src="https://cdn.pegg.dev/logo/infinity.gif" alt="Pegg" width="420" />

# Pegg Kanban Bitbucket

### Bitbucket pull requests for your Kanban board.

A Kanban integration for Bitbucket Cloud and Bitbucket Data Center. It prepares a git
worktree for each card run and opens a pull request when the card succeeds.

[Plugins docs](https://docs.pegg.dev/pegg/configurations/plugins/) · [Releases](https://github.com/peggco/pegg-kanban-bitbucket/releases) · [Website](https://pegg.dev) · [Discussions](https://github.com/peggco/pegg/discussions)

[![Latest release](https://img.shields.io/github/v/release/peggco/pegg-kanban-bitbucket?style=flat-square&sort=semver)](https://github.com/peggco/pegg-kanban-bitbucket/releases)
[![Go 1.26.5+](https://img.shields.io/badge/go-1.26.5%2B-00ADD8?style=flat-square&logo=go&logoColor=white)](https://github.com/peggco/pegg-kanban-bitbucket)
[![Plugin protocol v2](https://img.shields.io/badge/plugin%20protocol-v2-black?style=flat-square)](https://docs.pegg.dev/pegg/configurations/plugins/)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-black?style=flat-square)](https://github.com/peggco/pegg-kanban-bitbucket/blob/HEAD/LICENSE)

**Install**

```bash
pegg plugins install peggco/pegg-kanban
pegg plugins install peggco/pegg-kanban-bitbucket
```

</div>

## What it does

This plugin is hooks-only. It contributes no agent tools, skills, commands, or TUI, and it does not
sync issues.

Bitbucket is the delivery side of the board:

- `kanban.run.prepare` creates a git worktree and a `pegg/<slug>` branch for a card run.
- `kanban.card.succeeded` opens a pull request from that branch into the card's base branch.

Issues come from another provider — [Jira](https://github.com/peggco/pegg-kanban-jira),
[GitHub](https://github.com/peggco/pegg-kanban-github), or
[GitLab](https://github.com/peggco/pegg-kanban-gitlab). There is no Bitbucket issue fetch because
Bitbucket Cloud Issues was removed in August 2026 and Bitbucket Data Center has no built-in issue
tracker.

## Jira → Bitbucket flow

A common setup pairs `kanban-jira` with this plugin:

1. `kanban-jira` imports issues into the board.
2. A card run starts. `kanban-bitbucket` creates `.pegg-worktrees/<slug>` on branch
   `pegg/<slug>`.
3. The agent finishes the work and commits to that branch.
4. `kanban.card.succeeded` opens the pull request in Bitbucket.

The card's issue provider can be Jira, GitHub, or GitLab. This plugin does not require
`card.issue.provider == "bitbucket"`.

## Build and install

```sh
go mod tidy
go vet ./...
go test ./...
go build -trimpath -ldflags "-s -w" -o bin/pegg-kanban-bitbucket .
install -m 0755 bin/pegg-kanban-bitbucket "$HOME/.pegg/plugins/pegg-kanban-bitbucket"
pegg plugins list
```

## Hooks

| Hook | Kind | Priority | Purpose |
|---|---|---|---|
| `kanban.run.prepare` | filter | 10 | Create a git worktree with a `pegg/<slug>` branch for a card run. |
| `kanban.card.succeeded` | action | 10 | Open a Bitbucket pull request when a card succeeds. |

Both hooks are no-ops when the plugin is disabled.

### Worktrees

`kanban.run.prepare` reads the repository from `repo` in the incoming payload. It creates a
worktree under `worktree_root` (default `.pegg-worktrees`) using the card title and ID for the
slug. The base branch is `base_branch` when set, then `default_base`, then the repository's
current branch or `main`/`master`.

The returned payload adds a `context` object:

```json
{
  "kind": "worktree",
  "path": "/repo/.pegg-worktrees/fix-login-abc12345",
  "branch": "pegg/fix-login-abc12345",
  "base": "main",
  "label": "fix-login-abc12345"
}
```

### Pull requests

`kanban.card.succeeded` reads the branch from `context.branch`, `worktree.branch`, or
`card.worktree.branch`, and the base branch from `context.base`, `worktree.base`, `base_branch`,
then `default_base`. When no base is configured it asks Bitbucket for the repository default
branch.

The repository can be configured explicitly or discovered from the origin remote of the document
worktree. Supported remote forms:

- `https://bitbucket.org/{workspace}/{repo}.git`
- `git@bitbucket.org:{workspace}/{repo}.git`
- `https://{host}/scm/{project}/{repo}.git`
- `ssh://git@{host}:7999/{project}/{repo}.git`
- `ssh://git@{host}/{project}/{repo}.git`

`bitbucket.org` remotes are treated as Cloud; any other host is treated as Data Center with
`base_url` inferred as `https://{host}`.

The hook returns:

```json
{
  "pull_request": 42,
  "pull_request_url": "https://bitbucket.org/acme/widgets/pull-requests/42"
}
```

Duplicate pull requests produce an actionable error instead of a second PR. Credentials are never
printed; hook errors are redacted before they return to the host.

## Configuration

`~/.pegg/pegg.json`.

### Bitbucket Cloud

```json
{
  "plugins": {
    "options": {
      "kanban-bitbucket": {
        "enabled": true,
        "kind": "cloud",
        "workspace": "acme",
        "repo": "widgets",
        "email": "bot@example.com",
        "api_token": "ATBB...",
        "default_base": "main",
        "worktree_root": ".pegg-worktrees",
        "request_timeout_seconds": 30
      }
    }
  }
}
```

### Bitbucket Data Center

```json
{
  "plugins": {
    "options": {
      "kanban-bitbucket": {
        "enabled": true,
        "kind": "datacenter",
        "base_url": "https://bitbucket.example.com",
        "project": "PROJ",
        "repo": "widgets",
        "token": "http-access-token",
        "default_base": "main"
      }
    }
  }
}
```

`kind` also accepts `server` and `dc` for Data Center. The option keys `pegg-kanban-bitbucket`,
`bitbucket`, and `pegg-bitbucket` are accepted as fallbacks, with `kanban-bitbucket` taking
precedence.

### Environment

| Variable | Purpose |
|---|---|
| `PEGG_BITBUCKET_ENABLED` | Set `false` to disable the plugin. |
| `PEGG_BITBUCKET_KIND` | `cloud` or `datacenter` (`server` and `dc` accepted). |
| `PEGG_BITBUCKET_URL` | API base URL. Cloud defaults to `https://api.bitbucket.org/2.0`. |
| `PEGG_BITBUCKET_WORKSPACE` | Cloud workspace. |
| `PEGG_BITBUCKET_PROJECT` | Data Center project key. |
| `PEGG_BITBUCKET_REPO` | Repository slug. |
| `PEGG_BITBUCKET_EMAIL` | Cloud email for API token authentication. |
| `PEGG_BITBUCKET_API_TOKEN` | Cloud API token. |
| `PEGG_BITBUCKET_ACCESS_TOKEN` | Cloud access token (Bearer). |
| `PEGG_BITBUCKET_USERNAME` | Data Center username. |
| `PEGG_BITBUCKET_PASSWORD` | Data Center password. |
| `PEGG_BITBUCKET_TOKEN` | Data Center HTTP access token (Bearer). |
| `PEGG_BITBUCKET_DEFAULT_BASE` | Default pull request destination branch. |
| `PEGG_BITBUCKET_WORKTREE_ROOT` | Worktree directory (default `.pegg-worktrees`). |
| `PEGG_BITBUCKET_TIMEOUT_SECONDS` | API timeout (default `30`). |

## Authentication

**Bitbucket Cloud** uses an API token with your Atlassian account email:

```json
{ "email": "bot@example.com", "api_token": "ATBB..." }
```

That pair is sent as HTTP Basic. Alternatively set `access_token` to send
`Authorization: Bearer`. App passwords are deprecated — use an API token instead.

**Bitbucket Data Center** uses an HTTP access token, sent as `Authorization: Bearer` via `token`.
Username and password (`username` + `password`) are also supported as HTTP Basic.

Missing credentials produce an error naming the setting and environment variable to set. Tokens
and passwords are redacted from hook errors.

## Tests

```sh
gofmt -l .
go vet ./...
go test ./...
```

Tests use temporary git repositories and `httptest` servers; no live Bitbucket instance or network
access is required.

## License

Apache-2.0. See [LICENSE](https://github.com/peggco/pegg-kanban-bitbucket/blob/HEAD/LICENSE).
