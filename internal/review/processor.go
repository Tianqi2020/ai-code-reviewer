// Package review orchestrates the complete code review pipeline:
//  1. Fetch the PR diff from GitHub
//  2. Filter and truncate the diff
//  3. Send it to the LLM
//  4. Map LLM comments back to diff positions
//  5. Post the review back to GitHub
package review

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/go-github/v62/github"
	"github.com/Tianqi2020/ai-code-reviewer/internal/config"
	diffpkg "github.com/Tianqi2020/ai-code-reviewer/internal/diff"
	ghclient "github.com/Tianqi2020/ai-code-reviewer/internal/github"
	"github.com/Tianqi2020/ai-code-reviewer/internal/llm"
)

// githubClient is the subset of ghclient.Client used by the processor.
type githubClient interface {
	GetPR(ctx context.Context, owner, repo string, number int) (*ghclient.PullRequestInfo, error)
	GetPRDiff(ctx context.Context, owner, repo string, number int) (string, error)
	PostReview(ctx context.Context, prInfo *ghclient.PullRequestInfo, summary string, score int, comments []ghclient.ReviewComment) error
}

// llmReviewer is the subset of llm.Reviewer used by the processor.
type llmReviewer interface {
	Review(ctx context.Context, repoFullName, prTitle string, prNumber int, language, diffContent string) (*llm.ReviewResult, error)
}

// Processor ties together the GitHub client and LLM reviewer.
type Processor struct {
	gh     githubClient
	llm    llmReviewer
	cfg    *config.Config
}

// NewProcessor creates a new Processor.
func NewProcessor(gh *ghclient.Client, reviewer *llm.Reviewer, cfg *config.Config) *Processor {
	return &Processor{
		gh:  gh,
		llm: reviewer,
		cfg: cfg,
	}
}

// ProcessPullRequest is the entry point called by the webhook handler.
func (p *Processor) ProcessPullRequest(payload *github.PullRequestEvent) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	owner := payload.GetRepo().GetOwner().GetLogin()
	repo := payload.GetRepo().GetName()
	number := payload.GetNumber()

	slog.Info("starting review",
		"repo", payload.GetRepo().GetFullName(),
		"pr", number,
		"action", payload.GetAction(),
	)

	// 1. Fetch PR metadata
	prInfo, err := p.gh.GetPR(ctx, owner, repo, number)
	if err != nil {
		return fmt.Errorf("get PR metadata: %w", err)
	}

	// 2. Fetch the unified diff
	rawDiff, err := p.gh.GetPRDiff(ctx, owner, repo, number)
	if err != nil {
		return fmt.Errorf("get PR diff: %w", err)
	}

	if strings.TrimSpace(rawDiff) == "" {
		slog.Info("empty diff – nothing to review", "pr", number)
		return nil
	}

	// 3. Parse the diff (builds line→position maps)
	parsedDiff := diffpkg.Parse(rawDiff)

	// 4. Filter out ignored files and build a trimmed diff for the LLM
	filteredDiff := p.filterDiff(parsedDiff)
	if strings.TrimSpace(filteredDiff) == "" {
		slog.Info("all files ignored – nothing to review", "pr", number)
		return nil
	}

	// 5. Truncate if the diff is excessively large
	truncated := truncateLines(filteredDiff, p.cfg.MaxDiffLines)
	if len(truncated) < len(filteredDiff) {
		slog.Warn("diff truncated", "original_lines", lineCount(filteredDiff), "max_lines", p.cfg.MaxDiffLines)
		truncated += "\n\n[... diff truncated due to size limit ...]"
	}

	// 6. Send to the LLM for review
	result, err := p.llm.Review(ctx, prInfo.FullName, prInfo.Title, prInfo.Number, p.cfg.ReviewLanguage, truncated)
	if err != nil {
		return fmt.Errorf("LLM review: %w", err)
	}

	// 7. Convert LLM comments → GitHub inline comments (with diff position lookup)
	ghComments := p.MapComments(result.Comments, parsedDiff)

	// 8. Post the review to GitHub
	if err := p.gh.PostReview(ctx, prInfo, result.Summary, result.Score, ghComments); err != nil {
		return fmt.Errorf("post review: %w", err)
	}

	slog.Info("review completed",
		"repo", prInfo.FullName,
		"pr", number,
		"score", result.Score,
		"comments", len(ghComments),
	)
	return nil
}

// filterDiff removes files matching the ignore patterns and rebuilds a
// concatenated diff string from the remaining file diffs.
func (p *Processor) filterDiff(parsed *diffpkg.ParsedDiff) string {
	var sb strings.Builder
	for filename, fd := range parsed.Files {
		if diffpkg.ShouldIgnore(filename, p.cfg.IgnorePatterns) {
			slog.Debug("ignoring file", "file", filename)
			continue
		}
		sb.WriteString(fd.RawContent)
	}
	return sb.String()
}

// MapComments converts LLM ReviewComments into GitHub ReviewComments by looking
// up the diff position for each (file, line) pair.
// It is exported so the CLI review command can call it directly.
func (p *Processor) MapComments(comments []llm.ReviewComment, parsed *diffpkg.ParsedDiff) []ghclient.ReviewComment {
	formatted := llm.FormatComments(comments)
	out := make([]ghclient.ReviewComment, 0, len(formatted))

	for _, fc := range formatted {
		pos := parsed.GetPosition(fc.File, fc.Line)
		if pos == 0 {
			// Try with a leading slash stripped (some LLMs add one)
			pos = parsed.GetPosition(strings.TrimPrefix(fc.File, "/"), fc.Line)
		}
		out = append(out, ghclient.ReviewComment{
			Path:     fc.File,
			Position: pos,
			Body:     fc.FormattedBody,
		})
	}
	return out
}

// ── helpers ──────────────────────────────────────────────────────────────────

func truncateLines(s string, maxLines int) string {
	if maxLines <= 0 {
		return s
	}
	lines := strings.SplitN(s, "\n", maxLines+1)
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[:maxLines], "\n")
}

func lineCount(s string) int {
	return strings.Count(s, "\n")
}
