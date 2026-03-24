// Package cli implements the two top-level subcommands: review and server.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/Tianqi2020/ai-code-reviewer/internal/config"
	diffpkg "github.com/Tianqi2020/ai-code-reviewer/internal/diff"
	ghclient "github.com/Tianqi2020/ai-code-reviewer/internal/github"
	"github.com/Tianqi2020/ai-code-reviewer/internal/llm"
	"github.com/Tianqi2020/ai-code-reviewer/internal/review"
)

const reviewUsage = `USAGE:
  ai-code-reviewer review [flags]

FLAGS:
  --owner   string   GitHub repository owner (required)
  --repo    string   GitHub repository name  (required)
  --pr      int      Pull request number     (required)
  --post            Post the review as inline GitHub comments (default: false)
  --format  string  Output format: text | json (default: text)
  --lang    string  Language for review comments (default: from env / "English")
  -h, --help        Show this help

ENVIRONMENT:
  GITHUB_TOKEN    GitHub Personal Access Token (required)
  OPENAI_API_KEY  OpenAI API key              (required)
  OPENAI_MODEL    Model name (default: gpt-4o)

EXAMPLES:
  # Print review in terminal
  ai-code-reviewer review --owner Tianqi2020 --repo myrepo --pr 42

  # Print AND post inline GitHub comments
  ai-code-reviewer review --owner Tianqi2020 --repo myrepo --pr 42 --post

  # Machine-readable JSON output (useful in CI scripts)
  ai-code-reviewer review --owner Tianqi2020 --repo myrepo --pr 42 --format json
`

// RunReview is the entry point for the "review" subcommand.
func RunReview(args []string) {
	fs := flag.NewFlagSet("review", flag.ExitOnError)
	fs.Usage = func() { fmt.Print(reviewUsage) }

	owner := fs.String("owner", "", "GitHub repository owner")
	repo := fs.String("repo", "", "GitHub repository name")
	prNum := fs.Int("pr", 0, "Pull request number")
	post := fs.Bool("post", false, "Post review as inline GitHub comments")
	format := fs.String("format", "text", "Output format: text | json")
	lang := fs.String("lang", "", "Language for review comments")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// Validate required flags
	var missing []string
	if *owner == "" {
		missing = append(missing, "--owner")
	}
	if *repo == "" {
		missing = append(missing, "--repo")
	}
	if *prNum == 0 {
		missing = append(missing, "--pr")
	}
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "error: missing required flags: %s\n\n", strings.Join(missing, ", "))
		fmt.Print(reviewUsage)
		os.Exit(1)
	}

	if *format != "text" && *format != "json" {
		fmt.Fprintf(os.Stderr, "error: --format must be 'text' or 'json'\n")
		os.Exit(1)
	}

	// Load env
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		fatalf("configuration error: %v", err)
	}
	if *lang != "" {
		cfg.ReviewLanguage = *lang
	}

	// Build clients
	ghClient, err := ghclient.NewClient(cfg.GitHubToken)
	if err != nil {
		fatalf("github client: %v", err)
	}
	reviewer, err := llm.NewReviewer(cfg.OpenAIAPIKey, cfg.OpenAIModel)
	if err != nil {
		fatalf("llm reviewer: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// ── 1. Fetch PR metadata ──────────────────────────────────────────────
	printStep("Fetching PR #%d from %s/%s …", *prNum, *owner, *repo)
	prInfo, err := ghClient.GetPR(ctx, *owner, *repo, *prNum)
	if err != nil {
		fatalf("fetch PR: %v", err)
	}
	printStep("PR title: %s", prInfo.Title)

	// ── 2. Fetch unified diff ─────────────────────────────────────────────
	printStep("Fetching diff …")
	rawDiff, err := ghClient.GetPRDiff(ctx, *owner, *repo, *prNum)
	if err != nil {
		fatalf("fetch diff: %v", err)
	}
	if strings.TrimSpace(rawDiff) == "" {
		fmt.Println("No diff found — PR has no changed files.")
		os.Exit(0)
	}

	// ── 3. Parse + filter diff ────────────────────────────────────────────
	parsedDiff := diffpkg.Parse(rawDiff)
	filteredLines := filterAndBuild(parsedDiff, cfg)
	if strings.TrimSpace(filteredLines) == "" {
		fmt.Println("All changed files are in the ignore list — nothing to review.")
		os.Exit(0)
	}

	truncated := truncateLines(filteredLines, cfg.MaxDiffLines)
	printStep("Diff ready: %d lines (limit: %d)", lineCount(filteredLines), cfg.MaxDiffLines)

	// ── 4. Send to LLM ───────────────────────────────────────────────────
	printStep("Sending diff to %s for review …", cfg.OpenAIModel)
	result, err := reviewer.Review(ctx, prInfo.FullName, prInfo.Title, prInfo.Number, cfg.ReviewLanguage, truncated)
	if err != nil {
		fatalf("LLM review: %v", err)
	}

	// ── 5. Output results ─────────────────────────────────────────────────
	switch *format {
	case "json":
		printJSON(result)
	default:
		printText(result, prInfo)
	}

	// ── 6. Optionally post to GitHub ──────────────────────────────────────
	if *post {
		printStep("Posting review to GitHub …")
		processor := review.NewProcessor(ghClient, reviewer, cfg)
		ghComments := processor.MapComments(result.Comments, parsedDiff)
		if err := ghClient.PostReview(ctx, prInfo, result.Summary, result.Score, ghComments); err != nil {
			fatalf("post review: %v", err)
		}
		fmt.Printf("\n✅  Review posted to https://github.com/%s/%s/pull/%d\n",
			*owner, *repo, *prNum)
	} else {
		fmt.Printf("\n💡  Tip: run with --post to publish inline comments to GitHub.\n")
	}
}

// ── output formatters ─────────────────────────────────────────────────────────

func printText(result *llm.ReviewResult, prInfo *ghclient.PullRequestInfo) {
	sep := strings.Repeat("─", 70)

	fmt.Printf("\n%s\n", sep)
	fmt.Printf("  🤖  AI Code Review  ·  %s  ·  PR #%d\n", prInfo.FullName, prInfo.Number)
	fmt.Printf("%s\n\n", sep)
	fmt.Printf("  Score   : %d / 100\n", result.Score)
	fmt.Printf("  Summary : %s\n\n", result.Summary)

	if len(result.Comments) == 0 {
		fmt.Println("  ✅  No issues found — looks great!")
		fmt.Printf("%s\n", sep)
		return
	}

	fmt.Printf("  Found %d comment(s):\n", len(result.Comments))
	fmt.Printf("%s\n", sep)

	severityEmoji := map[string]string{
		"critical": "🔴", "major": "🟠", "minor": "🟡", "info": "🔵",
	}
	categoryEmoji := map[string]string{
		"bug": "🐛", "security": "🔒", "style": "✨",
		"performance": "⚡", "suggestion": "💡",
	}

	for i, c := range result.Comments {
		se := severityEmoji[c.Severity]
		if se == "" {
			se = "⚪"
		}
		ce := categoryEmoji[c.Category]
		if ce == "" {
			ce = "📝"
		}

		fmt.Printf("\n[%d/%d] %s %s  %s  (%s / %s)\n",
			i+1, len(result.Comments),
			se, ce,
			c.Title,
			c.Category, c.Severity,
		)
		fmt.Printf("  File : %s  (line %d)\n", c.File, c.Line)
		// Indent the body for readability
		for _, line := range strings.Split(c.Body, "\n") {
			fmt.Printf("  %s\n", line)
		}
	}

	fmt.Printf("\n%s\n", sep)
}

func printJSON(result *llm.ReviewResult) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		fatalf("json encode: %v", err)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func printStep(format string, args ...any) {
	fmt.Printf("  → "+format+"\n", args...)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

func filterAndBuild(parsed *diffpkg.ParsedDiff, cfg *config.Config) string {
	var sb strings.Builder
	for filename, fd := range parsed.Files {
		if diffpkg.ShouldIgnore(filename, cfg.IgnorePatterns) {
			continue
		}
		sb.WriteString(fd.RawContent)
	}
	return sb.String()
}

func truncateLines(s string, max int) string {
	if max <= 0 {
		return s
	}
	lines := strings.SplitN(s, "\n", max+1)
	if len(lines) <= max {
		return s
	}
	return strings.Join(lines[:max], "\n") + "\n\n[... diff truncated ...]"
}

func lineCount(s string) int {
	return strings.Count(s, "\n")
}
