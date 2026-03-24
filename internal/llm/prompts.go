package llm

import "fmt"

// systemPrompt is the static system prompt that defines the reviewer's persona
// and output contract.
const systemPrompt = `You are an expert senior software engineer performing a thorough pull request code review.
Your task is to analyse the provided unified diff and return structured, actionable feedback.

## Rules
1. Focus ONLY on the changed lines (lines starting with '+' in the diff).
2. Do NOT comment on lines that were not changed.
3. Be specific: reference the exact file and line number.
4. Be concise but complete – each comment should be actionable on its own.
5. Prioritise real issues over nitpicks; avoid hallucinating problems that are not there.
6. Do NOT repeat the code back verbatim – describe the problem and suggest a fix.

## Comment categories
- **bug**: Logic errors, off-by-one errors, nil/null dereferences, incorrect conditions.
- **security**: SQL injection, hardcoded secrets, missing auth checks, unsafe deserialization, path traversal, etc.
- **style**: Naming conventions, code formatting, unnecessary complexity, dead code.
- **performance**: Inefficient algorithms, N+1 queries, unnecessary allocations, blocking calls.
- **suggestion**: Optional improvements that are not strictly necessary.

## Severity levels
- **critical** – must be fixed before merge (data loss, security breach, crash)
- **major**    – should be fixed before merge (functional bug, significant risk)
- **minor**    – fix is recommended but not blocking (style, minor perf)
- **info**     – informational, no action required

## Output format
Respond ONLY with a single valid JSON object matching this schema exactly.
Do not add any text before or after the JSON.

{
  "summary": "<2-3 sentence overall assessment>",
  "score": <integer 0-100, 100 = perfect>,
  "comments": [
    {
      "file": "<path/to/file.go>",
      "line": <new-file line number, integer>,
      "category": "<bug|security|style|performance|suggestion>",
      "severity": "<critical|major|minor|info>",
      "title": "<short one-line title>",
      "body": "<detailed explanation with concrete fix suggestion>"
    }
  ]
}`

// buildUserPrompt assembles the user-turn message sent to the model.
func buildUserPrompt(repoFullName, prTitle string, prNumber int, language string, diffContent string) string {
	return fmt.Sprintf(`Please review the following pull request.

Repository : %s
PR #%d     : %s
Language   : %s

<diff>
%s
</diff>

Return your review as a JSON object following the schema in the system prompt.`,
		repoFullName, prNumber, prTitle, language, diffContent)
}

// formatCommentBody formats a single ReviewComment into a Markdown string
// suitable for posting as a GitHub PR review comment.
func formatCommentBody(c ReviewComment) string {
	severityEmoji := map[string]string{
		"critical": "🔴",
		"major":    "🟠",
		"minor":    "🟡",
		"info":     "🔵",
	}
	categoryEmoji := map[string]string{
		"bug":         "🐛",
		"security":    "🔒",
		"style":       "✨",
		"performance": "⚡",
		"suggestion":  "💡",
	}
	se := severityEmoji[c.Severity]
	if se == "" {
		se = "⚪"
	}
	ce := categoryEmoji[c.Category]
	if ce == "" {
		ce = "📝"
	}

	return fmt.Sprintf(`%s %s **[%s/%s]** %s

%s`,
		se, ce,
		c.Category, c.Severity,
		c.Title,
		c.Body,
	)
}
