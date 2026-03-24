// Package diff parses unified diffs and maps file line numbers to GitHub
// pull-request review "diff positions" required by the GitHub REST API.
package diff

import (
	"bufio"
	"path/filepath"
	"strconv"
	"strings"
)

// ParsedDiff holds per-file position maps built from a unified diff.
type ParsedDiff struct {
	// Files maps filename -> *FileDiff
	Files map[string]*FileDiff
}

// FileDiff stores the parsed content and position mapping for a single file.
type FileDiff struct {
	Filename    string
	OldFilename string
	// LineToPosition maps new-file line numbers to GitHub diff positions.
	// Only lines that appear in the diff (changed or context) are present.
	LineToPosition map[int]int
	// RawContent is the raw diff content for this file (for display / debug).
	RawContent string
}

// GetPosition returns the GitHub diff position for (filename, newLineNumber).
// Returns 0 if the file or line is not in the diff.
func (pd *ParsedDiff) GetPosition(filename string, line int) int {
	f, ok := pd.Files[filename]
	if !ok {
		return 0
	}
	return f.LineToPosition[line]
}

// Filenames returns all filenames present in the diff.
func (pd *ParsedDiff) Filenames() []string {
	names := make([]string, 0, len(pd.Files))
	for k := range pd.Files {
		names = append(names, k)
	}
	return names
}

// Parse parses a unified diff string and returns a ParsedDiff.
// It handles standard Git unified diffs (e.g. from GitHub's /pulls/{n}.diff endpoint).
func Parse(rawDiff string) *ParsedDiff {
	pd := &ParsedDiff{Files: make(map[string]*FileDiff)}

	var cur *FileDiff
	var diffPos int    // position counter within the current file's diff
	var newLine int    // current new-file line number
	var sb strings.Builder

	scanner := bufio.NewScanner(strings.NewReader(rawDiff))
	// Increase buffer for very large diffs
	buf := make([]byte, 2*1024*1024)
	scanner.Buffer(buf, cap(buf))

	for scanner.Scan() {
		line := scanner.Text()

		// ── New file section ────────────────────────────────────────────────
		if strings.HasPrefix(line, "diff --git ") {
			saveFile(pd, cur, sb.String())
			sb.Reset()

			filename := extractNewFilename(line)
			cur = &FileDiff{
				Filename:       filename,
				LineToPosition: make(map[int]int),
			}
			diffPos = 0
			newLine = 0
			sb.WriteString(line + "\n")
			continue
		}

		if cur == nil {
			continue
		}

		sb.WriteString(line + "\n")

		switch {
		// Headers we skip without counting
		case strings.HasPrefix(line, "index "),
			strings.HasPrefix(line, "new file mode"),
			strings.HasPrefix(line, "deleted file mode"),
			strings.HasPrefix(line, "rename from"),
			strings.HasPrefix(line, "rename to"),
			strings.HasPrefix(line, "Binary files"),
			strings.HasPrefix(line, "similarity index"):
			continue

		case strings.HasPrefix(line, "--- "):
			if strings.HasPrefix(line, "--- a/") {
				cur.OldFilename = strings.TrimPrefix(line, "--- a/")
			}
			continue

		case strings.HasPrefix(line, "+++ "):
			// Sometimes the filename in the diff header can differ (renames),
			// so update from the +++ line as the canonical new name.
			if strings.HasPrefix(line, "+++ b/") {
				cur.Filename = strings.TrimPrefix(line, "+++ b/")
			}
			continue

		// ── Hunk header ─────────────────────────────────────────────────────
		// According to GitHub REST API docs, position 1 = first line AFTER @@.
		// The @@ line itself does not get a position.
		case strings.HasPrefix(line, "@@"):
			diffPos++
			newLine = parseHunkNewStart(line) - 1 // will be ++ on the first content line

		// ── Added line ───────────────────────────────────────────────────────
		case strings.HasPrefix(line, "+"):
			diffPos++
			newLine++
			cur.LineToPosition[newLine] = diffPos

		// ── Removed line ─────────────────────────────────────────────────────
		case strings.HasPrefix(line, "-"):
			diffPos++
			// Removed lines have no new-file line number.

		// ── Context line ─────────────────────────────────────────────────────
		default:
			diffPos++
			newLine++
			cur.LineToPosition[newLine] = diffPos
		}
	}

	saveFile(pd, cur, sb.String())
	return pd
}

// ── helpers ──────────────────────────────────────────────────────────────────

func saveFile(pd *ParsedDiff, f *FileDiff, content string) {
	if f == nil {
		return
	}
	f.RawContent = content
	pd.Files[f.Filename] = f
}

// extractNewFilename extracts the new filename from a "diff --git a/x b/y" line.
func extractNewFilename(line string) string {
	// Format: diff --git a/<old> b/<new>
	parts := strings.SplitN(line, " b/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return line
}

// parseHunkNewStart extracts the new-file start line number from a hunk header.
// Example: "@@ -10,6 +15,8 @@ func Foo()" → 15
func parseHunkNewStart(hunkLine string) int {
	idx := strings.Index(hunkLine, " +")
	if idx < 0 {
		return 1
	}
	rest := hunkLine[idx+2:]
	end := strings.IndexAny(rest, ", @\t ")
	if end < 0 {
		end = len(rest)
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil || n == 0 {
		return 1
	}
	return n
}

// ShouldIgnore returns true if the filename matches any of the given glob patterns.
func ShouldIgnore(filename string, patterns []string) bool {
	base := filepath.Base(filename)
	for _, pattern := range patterns {
		// Check both full path and basename
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, filename); matched {
			return true
		}
		// Prefix match for directories (e.g. "vendor/")
		if strings.HasSuffix(pattern, "/") && strings.HasPrefix(filename, pattern) {
			return true
		}
	}
	return false
}
