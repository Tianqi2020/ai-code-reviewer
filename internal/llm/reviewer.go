package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// ReviewComment represents a single inline review comment returned by the LLM.
type ReviewComment struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Category string `json:"category"` // bug | security | style | performance | suggestion
	Severity string `json:"severity"` // critical | major | minor | info
	Title    string `json:"title"`
	Body     string `json:"body"`
}

// ReviewResult is the full structured response from the LLM.
type ReviewResult struct {
	Summary  string          `json:"summary"`
	Score    int             `json:"score"`
	Comments []ReviewComment `json:"comments"`
}

// FormattedComment is a ReviewComment enriched with its formatted body text.
type FormattedComment struct {
	ReviewComment
	FormattedBody string
}

// Reviewer sends code diffs to an OpenAI model and parses the structured response.
type Reviewer struct {
	client *openai.Client
	model  string
}

// NewReviewer constructs a Reviewer with the provided OpenAI credentials.
func NewReviewer(apiKey, model string) (*Reviewer, error) {
	if apiKey == "" {
		return nil, errors.New("openai api key must not be empty")
	}
	if model == "" {
		model = openai.GPT4o
	}
	return &Reviewer{
		client: openai.NewClient(apiKey),
		model:  model,
	}, nil
}

// Review sends the diff to the configured model and returns a ReviewResult.
// diffContent should be a unified diff string (may be truncated by the caller).
func (r *Reviewer) Review(
	ctx context.Context,
	repoFullName, prTitle string,
	prNumber int,
	language string,
	diffContent string,
) (*ReviewResult, error) {
	userPrompt := buildUserPrompt(repoFullName, prTitle, prNumber, language, diffContent)

	slog.Info("sending diff to LLM",
		"repo", repoFullName,
		"pr", prNumber,
		"model", r.model,
		"diff_chars", len(diffContent),
	)

	resp, err := r.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: r.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: userPrompt},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Temperature: 0.1,
		MaxTokens:   4096,
	})
	if err != nil {
		return nil, fmt.Errorf("openai request failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, errors.New("openai returned no choices")
	}

	raw := strings.TrimSpace(resp.Choices[0].Message.Content)

	slog.Info("LLM response received",
		"repo", repoFullName,
		"pr", prNumber,
		"usage_prompt", resp.Usage.PromptTokens,
		"usage_completion", resp.Usage.CompletionTokens,
	)

	var result ReviewResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("failed to parse LLM JSON response: %w\nraw: %s", err, raw)
	}

	return &result, nil
}

// FormatComments converts ReviewComments into FormattedComments with
// Markdown-formatted bodies ready for posting to GitHub.
func FormatComments(comments []ReviewComment) []FormattedComment {
	out := make([]FormattedComment, 0, len(comments))
	for _, c := range comments {
		out = append(out, FormattedComment{
			ReviewComment: c,
			FormattedBody: formatCommentBody(c),
		})
	}
	return out
}
