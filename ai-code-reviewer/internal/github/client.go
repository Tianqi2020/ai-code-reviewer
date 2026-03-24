// Package github wraps the go-github library with the specific operations
// needed by the code reviewer: fetching PR diffs and posting PR reviews.
package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"
)

// PullRequestInfo contains the key fields of a GitHub pull request event.
type PullRequestInfo struct {
	Owner     string
	Repo      string
	Number    int
	Title     string
	HeadSHA   string
	BaseRef   string
	HeadRef   string
	FullName  string // "owner/repo"
}

// ReviewComment is the input type for posting an inline review comment.
type ReviewComment struct {
	Path     string
	Position int // diff position (from ParsedDiff)
	Body     string
}

// Client wraps the GitHub REST API.
type Client struct {
	gh *github.Client
}

// NewClient constructs an authenticated GitHub client.
func NewClient(token string) (*Client, error) {
	if token == "" {
		return nil, errors.New("github token must not be empty")
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	return &Client{gh: github.NewClient(tc)}, nil
}

// GetPRDiff fetches the raw unified diff for a pull request.
// It uses the application/vnd.github.v3.diff Accept header.
func (c *Client) GetPRDiff(ctx context.Context, owner, repo string, number int) (string, error) {
	opts := &github.RawOptions{Type: github.Diff}
	raw, _, err := c.gh.PullRequests.GetRaw(ctx, owner, repo, number, *opts)
	if err != nil {
		return "", fmt.Errorf("failed to fetch PR diff for %s/%s#%d: %w", owner, repo, number, err)
	}
	return raw, nil
}

// GetPR fetches pull request metadata.
func (c *Client) GetPR(ctx context.Context, owner, repo string, number int) (*PullRequestInfo, error) {
	pr, _, err := c.gh.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR metadata: %w", err)
	}
	return &PullRequestInfo{
		Owner:    owner,
		Repo:     repo,
		Number:   number,
		Title:    pr.GetTitle(),
		HeadSHA:  pr.GetHead().GetSHA(),
		BaseRef:  pr.GetBase().GetRef(),
		HeadRef:  pr.GetHead().GetRef(),
		FullName: fmt.Sprintf("%s/%s", owner, repo),
	}, nil
}

// PostReview creates a GitHub pull request review with inline comments.
// If comments is empty the review is posted as a general comment only.
func (c *Client) PostReview(
	ctx context.Context,
	prInfo *PullRequestInfo,
	summary string,
	score int,
	comments []ReviewComment,
) error {
	// Build inline comments for the GitHub API
	var draftComments []*github.DraftReviewComment
	for _, rc := range comments {
		if rc.Position <= 0 {
			// Skip comments that couldn't be mapped to a diff position
			slog.Warn("skipping comment with no diff position", "file", rc.Path)
			continue
		}
		pos := rc.Position
		draftComments = append(draftComments, &github.DraftReviewComment{
			Path:     github.String(rc.Path),
			Position: &pos,
			Body:     github.String(rc.Body),
		})
	}

	reviewBody := buildReviewSummary(summary, score, len(comments), len(draftComments))

	event := "COMMENT" // Use COMMENT so we don't block merges; adjust to "REQUEST_CHANGES" if desired
	req := &github.PullRequestReviewRequest{
		CommitID: github.String(prInfo.HeadSHA),
		Body:     github.String(reviewBody),
		Event:    github.String(event),
		Comments: draftComments,
	}

	_, _, err := c.gh.PullRequests.CreateReview(ctx, prInfo.Owner, prInfo.Repo, prInfo.Number, req)
	if err != nil {
		return fmt.Errorf("failed to create GitHub review: %w", err)
	}

	slog.Info("review posted",
		"repo", prInfo.FullName,
		"pr", prInfo.Number,
		"inline_comments", len(draftComments),
		"score", score,
	)
	return nil
}

// SetPRLabel adds a label to a pull request (creates it if it doesn't exist).
func (c *Client) SetPRLabel(ctx context.Context, owner, repo string, number int, label string) error {
	_, _, err := c.gh.Issues.AddLabelsToIssue(ctx, owner, repo, number, []string{label})
	return err
}

// FetchURLContent is a helper that fetches raw URL content (used in tests / debugging).
func FetchURLContent(url, token string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github.v3.diff")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func buildReviewSummary(summary string, score, totalComments, inlineComments int) string {
	skipped := totalComments - inlineComments
	var sb strings.Builder

	sb.WriteString("## 🤖 AI Code Review\n\n")
	sb.WriteString(fmt.Sprintf("**Score:** %d / 100\n\n", score))
	sb.WriteString("### Summary\n\n")
	sb.WriteString(summary)
	sb.WriteString("\n\n")

	if totalComments > 0 {
		sb.WriteString(fmt.Sprintf("**Inline comments:** %d", inlineComments))
		if skipped > 0 {
			sb.WriteString(fmt.Sprintf(" (%d comment(s) could not be mapped to diff lines and were skipped)", skipped))
		}
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("✅ No issues found — looks good to me!\n\n")
	}

	sb.WriteString("---\n*Powered by [ai-code-reviewer](https://github.com/Tianqi2020/ai-code-reviewer)*")
	return sb.String()
}
