// Package webhook handles incoming GitHub webhook events, validates their
// HMAC-SHA256 signatures, and dispatches pull_request events for review.
package webhook

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/go-github/v62/github"
	"github.com/Tianqi2020/ai-code-reviewer/internal/review"
)

// Processor is the interface the handler calls when a reviewable PR event arrives.
type Processor interface {
	ProcessPullRequest(payload *github.PullRequestEvent) error
}

// NewHandler returns an http.HandlerFunc that handles GitHub webhook payloads.
// It validates the X-Hub-Signature-256 header and dispatches pull_request events
// to the supplied Processor.
func NewHandler(secret string, processor Processor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 25*1024*1024)) // 25 MB limit
		if err != nil {
			slog.Error("failed to read request body", "error", err)
			http.Error(w, "failed to read body", http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		// Validate HMAC-SHA256 signature
		if err := validateSignature(secret, r.Header.Get("X-Hub-Signature-256"), body); err != nil {
			slog.Warn("webhook signature validation failed", "error", err)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		eventType := r.Header.Get("X-GitHub-Event")
		deliveryID := r.Header.Get("X-GitHub-Delivery")

		slog.Info("webhook received",
			"event", eventType,
			"delivery", deliveryID,
		)

		switch eventType {
		case "pull_request":
			if err := handlePullRequest(w, body, processor); err != nil {
				slog.Error("pull_request handler error",
					"delivery", deliveryID,
					"error", err,
				)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

		case "ping":
			slog.Info("ping received – webhook configured successfully")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"message":"pong"}`)
			return

		default:
			// Acknowledge but do nothing for unhandled event types
			slog.Debug("unhandled event type", "event", eventType)
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	}
}

// handlePullRequest parses the payload and dispatches it for review if the
// action is one that warrants a new review run.
func handlePullRequest(w http.ResponseWriter, body []byte, processor Processor) error {
	var payload github.PullRequestEvent
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("failed to parse pull_request payload: %w", err)
	}

	action := payload.GetAction()
	slog.Info("pull_request event",
		"action", action,
		"repo", payload.GetRepo().GetFullName(),
		"pr", payload.GetNumber(),
	)

	// Only trigger review on these actions
	switch action {
	case "opened", "synchronize", "reopened":
		// Process asynchronously so we return 200 to GitHub quickly
		go func() {
			if err := processor.ProcessPullRequest(&payload); err != nil {
				slog.Error("review processor error",
					"repo", payload.GetRepo().GetFullName(),
					"pr", payload.GetNumber(),
					"error", err,
				)
			}
		}()
	default:
		slog.Debug("ignoring pull_request action", "action", action)
	}

	return nil
}

// validateSignature checks the X-Hub-Signature-256 header against the payload.
func validateSignature(secret, signature string, body []byte) error {
	if secret == "" {
		return fmt.Errorf("webhook secret is not configured")
	}
	if signature == "" {
		return fmt.Errorf("missing X-Hub-Signature-256 header")
	}

	// go-github provides a convenience validator
	if err := github.ValidateSignature(signature, body, []byte(secret)); err != nil {
		return fmt.Errorf("signature mismatch: %w", err)
	}
	return nil
}

// Ensure *review.Processor implements the Processor interface at compile time.
var _ Processor = (*review.Processor)(nil)
