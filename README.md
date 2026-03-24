# 🤖 ai-code-reviewer

A **Go CLI tool** that fetches GitHub Pull Request diffs, sends them to OpenAI GPT-4o for analysis, and returns structured, inline code review feedback — covering **bugs**, **security vulnerabilities**, **style issues**, and **performance problems**.

![Go](https://img.shields.io/badge/Go-1.22-00ADD8?style=flat&logo=go)
![OpenAI](https://img.shields.io/badge/OpenAI-GPT--4o-412991?style=flat&logo=openai)
![GitHub](https://img.shields.io/badge/GitHub-Webhooks-181717?style=flat&logo=github)
![License](https://img.shields.io/badge/license-MIT-green?style=flat)

---

## ✨ Features

| Feature | Details |
|---|---|
| **CLI-first** | Run `ai-code-reviewer review --pr 42` from any terminal |
| **Inline comments** | Feedback posted directly on the changed lines in the diff |
| **Structured categories** | `bug` · `security` · `style` · `performance` · `suggestion` |
| **Severity levels** | `critical` 🔴 · `major` 🟠 · `minor` 🟡 · `info` 🔵 |
| **Prompt engineering** | System prompt ensures actionable, non-generic feedback with concrete fix suggestions |
| **JSON output** | Machine-readable output for scripting and CI pipelines |
| **Webhook server** | Optional `server` subcommand for automated PR review on push events |
| **File filtering** | Skips lock files, generated code, vendor directories |
| **HMAC validation** | Verifies every webhook payload signature |
| **Docker-ready** | Minimal `scratch`-based image (~10 MB) |

---

## 📦 Installation

### Option A — go install (recommended)

```bash
go install github.com/Tianqi2020/ai-code-reviewer@latest
```

### Option B — Build from source

```bash
git clone https://github.com/Tianqi2020/ai-code-reviewer.git
cd ai-code-reviewer
make install      # installs to $GOPATH/bin
# or
make build        # builds to ./bin/ai-code-reviewer
```

---

## ⚙️ Configuration

Set environment variables, or create a `.env` file in the working directory:

```bash
cp .env.example .env
# Fill in your credentials
```

| Variable | Required | Default | Description |
|---|---|---|---|
| `GITHUB_TOKEN` | ✅ | — | GitHub PAT with `repo` scope |
| `OPENAI_API_KEY` | ✅ | — | OpenAI API key |
| `GITHUB_WEBHOOK_SECRET` | ⚠️ server only | — | Webhook HMAC secret |
| `OPENAI_MODEL` | | `gpt-4o` | Model (`gpt-4o`, `gpt-4o-mini`, …) |
| `REVIEW_LANGUAGE` | | `English` | Language for review comments |
| `MAX_DIFF_LINES` | | `2000` | Max diff lines sent to LLM |
| `IGNORE_PATTERNS` | | `*.lock,*.sum,vendor/,…` | Comma-separated globs to skip |

**GitHub Token scopes needed:** `repo` (private repos) or `public_repo` + `pull_requests:write` (public repos).

---

## 🚀 Usage

### `review` — CLI mode (primary feature)

Review a pull request directly from your terminal:

```bash
# Print structured review in the terminal
ai-code-reviewer review --owner Tianqi2020 --repo myrepo --pr 42

# Print AND post inline GitHub comments on the PR
ai-code-reviewer review --owner Tianqi2020 --repo myrepo --pr 42 --post

# Output as JSON (for CI scripting)
ai-code-reviewer review --owner Tianqi2020 --repo myrepo --pr 42 --format json

# Review in a specific language
ai-code-reviewer review --owner Tianqi2020 --repo myrepo --pr 42 --lang Chinese
```

**All flags:**

```
--owner   string   GitHub repository owner     (required)
--repo    string   GitHub repository name      (required)
--pr      int      Pull request number         (required)
--post             Post review as GitHub inline comments
--format  string   Output format: text | json  (default: text)
--lang    string   Language for review comments
```

**Example terminal output:**

```
  → Fetching PR #42 from Tianqi2020/myrepo …
  → PR title: Add user authentication handler
  → Fetching diff …
  → Diff ready: 312 lines (limit: 2000)
  → Sending diff to gpt-4o for review …

──────────────────────────────────────────────────────────────────────
  🤖  AI Code Review  ·  Tianqi2020/myrepo  ·  PR #42
──────────────────────────────────────────────────────────────────────

  Score   : 68 / 100
  Summary : The PR introduces a working authentication handler, but contains
            a critical SQL injection vulnerability and two missing error checks
            that must be resolved before merging.

  Found 3 comment(s):
──────────────────────────────────────────────────────────────────────

[1/3] 🔴 🔒  SQL injection via unsanitised input  (security / critical)
  File : handlers/auth.go  (line 47)
  The `username` value is interpolated directly into the query string.
  Use a parameterised query instead:
    db.QueryRow("SELECT id FROM users WHERE username = $1", username)

[2/3] 🟠 🐛  Unchecked error from rows.Close()  (bug / major)
  File : handlers/auth.go  (line 61)
  rows.Close() returns an error that is silently discarded.
  Add:  if err := rows.Close(); err != nil { ... }

[3/3] 🟡 ✨  Exported function lacks godoc comment  (style / minor)
  File : handlers/auth.go  (line 12)
  Exported functions should have a godoc comment starting with the function name.

──────────────────────────────────────────────────────────────────────

💡  Tip: run with --post to publish inline comments to GitHub.
```

---

### `server` — Webhook mode (automation)

Start a server that automatically reviews every new/updated PR:

```bash
ai-code-reviewer server
ai-code-reviewer server --port 9090
```

#### Setup steps

1. Start the server: `ai-code-reviewer server`
2. Expose it publicly (local dev): `ngrok http 8080`
3. Register the webhook in GitHub:
   - **Payload URL:** `https://xxxx.ngrok.io/webhook`
   - **Content type:** `application/json`
   - **Secret:** your `GITHUB_WEBHOOK_SECRET` value
   - **Events:** Pull requests

The server handles: `opened`, `synchronize`, `reopened` actions.

---

## 🏗 Architecture

```
CLI Entry Point (main.go)
         │
    ┌────┴────────────────────┐
    │                         │
    ▼                         ▼
review subcommand         server subcommand
(internal/cli/review.go)  (internal/cli/server.go)
    │                         │
    │                    webhook handler
    │                    (HMAC validation)
    │                         │
    └──────────┬──────────────┘
               │
        ┌──────▼──────────────────────┐
        │      Review Processor       │
        │  (internal/review/          │
        │   processor.go)             │
        └──────┬──────────────────────┘
               │
    ┌──────────┼──────────────┐
    ▼          ▼              ▼
GitHub      Diff          OpenAI
Client      Parser        Reviewer
(fetch      (line→pos     (GPT-4o,
diff,       mapping)      structured
post)                     JSON)
```

---

## 📂 Project Structure

```
ai-code-reviewer/
├── main.go                        # Subcommand dispatcher (review | server)
├── go.mod / go.sum
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── .env.example
├── .github/
│   └── workflows/ci.yml           # GitHub Actions CI
└── internal/
    ├── cli/
    │   ├── review.go              # "review" subcommand — CLI entry point
    │   └── server.go              # "server" subcommand — webhook server
    ├── config/
    │   └── config.go              # Environment-based configuration
    ├── diff/
    │   └── parser.go              # Unified diff parser + ignore filtering
    ├── github/
    │   └── client.go              # GitHub API (diff fetch, review posting)
    ├── llm/
    │   ├── reviewer.go            # OpenAI API integration
    │   └── prompts.go             # System prompt + Markdown formatting
    ├── review/
    │   └── processor.go           # Pipeline orchestration
    └── webhook/
        └── handler.go             # HTTP handler + HMAC-SHA256 validation
```

---

## 🧪 Testing

```bash
make test
```

**Manual webhook test (no GitHub needed):**

```bash
BODY='{"action":"opened","number":1,"pull_request":{"title":"test","head":{"sha":"abc"},"base":{"ref":"main"},"number":1},"repository":{"name":"repo","full_name":"user/repo","owner":{"login":"user"}}}'

SIG=$(echo -n "$BODY" | openssl dgst -sha256 -hmac "your_secret" | awk '{print "sha256="$2}')

curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: pull_request" \
  -H "X-Hub-Signature-256: $SIG" \
  -d "$BODY"
```

---

## 🐳 Docker

```bash
make docker-build
make docker-run       # uses .env

# Or with Compose
make compose-up
make compose-down
```

---

## 🛠 Makefile reference

```bash
make build            # Build binary → ./bin/ai-code-reviewer
make install          # Install to $GOPATH/bin
make run-server       # Start webhook server
make review OWNER=x REPO=y PR=42        # CLI review (terminal output)
make review-post OWNER=x REPO=y PR=42   # CLI review + post to GitHub
make test             # Run all tests
make tidy             # go mod tidy
make docker-build     # Build Docker image
make compose-up       # Start via Docker Compose
make ngrok            # Expose port 8080 with ngrok
```

---

## 🔐 Prompt Engineering

The system prompt in `internal/llm/prompts.go` is carefully designed to produce **actionable, context-aware** feedback:

- Instructs the model to focus **only on changed lines** (avoids noise)
- Defines strict **categories and severity levels** with concrete examples
- Requires a **concrete fix suggestion** in every comment body
- Uses **JSON schema enforcement** via OpenAI's `response_format: json_object`
- Sets `temperature: 0.1` for deterministic, consistent output
- Wraps the diff in `<diff>` XML tags for clear context boundaries

---

## 🛣 Roadmap

- [ ] GitHub App support (replace PAT with App auth)
- [ ] Skip re-reviewing already-reviewed commits (SHA cache)
- [ ] Multiple LLM provider support (Anthropic Claude, local models)
- [ ] Per-repository config file (`.ai-reviewer.yaml`)
- [ ] Cost / token usage report per review

---

## 📄 License

MIT — see [LICENSE](LICENSE) for details.
