package main

import (
	"fmt"
	"os"

	"github.com/Tianqi2020/ai-code-reviewer/internal/cli"
)

const version = "0.1.0"

const usage = `ai-code-reviewer - AI-powered GitHub PR code review tool

USAGE:
  ai-code-reviewer <command> [flags]

COMMANDS:
  review    Fetch a PR diff, send it to the LLM, and print (or post) the review
  server    Start the webhook server that auto-reviews PRs on push events

GLOBAL FLAGS:
  -h, --help      Show this help message
  -v, --version   Show version

Run 'ai-code-reviewer <command> --help' for command-specific flags.

EXAMPLES:
  # Review PR #42 and print results in the terminal
  ai-code-reviewer review --owner Tianqi2020 --repo myrepo --pr 42

  # Review and also post the feedback as inline GitHub comments
  ai-code-reviewer review --owner Tianqi2020 --repo myrepo --pr 42 --post

  # Output as JSON (useful for scripting / CI)
  ai-code-reviewer review --owner Tianqi2020 --repo myrepo --pr 42 --format json

  # Start the webhook server (listens for GitHub push events)
  ai-code-reviewer server
  ai-code-reviewer server --port 9090
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "review":
		cli.RunReview(os.Args[2:])
	case "server":
		cli.RunServer(os.Args[2:])
	case "-v", "--version", "version":
		fmt.Printf("ai-code-reviewer v%s\n", version)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %q\n\n", os.Args[1])
		fmt.Print(usage)
		os.Exit(1)
	}
}
