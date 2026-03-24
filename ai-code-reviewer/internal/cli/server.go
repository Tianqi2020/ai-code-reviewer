package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/Tianqi2020/ai-code-reviewer/internal/config"
	ghclient "github.com/Tianqi2020/ai-code-reviewer/internal/github"
	"github.com/Tianqi2020/ai-code-reviewer/internal/llm"
	"github.com/Tianqi2020/ai-code-reviewer/internal/review"
	"github.com/Tianqi2020/ai-code-reviewer/internal/webhook"
)

const serverUsage = `USAGE:
  ai-code-reviewer server [flags]

FLAGS:
  --port  int    HTTP port to listen on (default: 8080, or $PORT env var)
  -h, --help     Show this help

DESCRIPTION:
  Starts a long-running HTTP server that listens for GitHub webhook events.
  When a pull_request event (opened / synchronize / reopened) is received,
  it automatically fetches the diff, sends it to the LLM, and posts inline
  review comments back to the PR.

SETUP:
  1. Set GITHUB_TOKEN, GITHUB_WEBHOOK_SECRET, OPENAI_API_KEY in .env
  2. Run:  ai-code-reviewer server
  3. Expose port with ngrok for local dev:  ngrok http 8080
  4. Register the ngrok URL in GitHub: Settings → Webhooks → Add webhook
     Payload URL  : https://xxxx.ngrok.io/webhook
     Content type : application/json
     Events       : Pull requests
`

// RunServer is the entry point for the "server" subcommand.
func RunServer(args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	fs.Usage = func() { fmt.Print(serverUsage) }
	port := fs.String("port", "", "HTTP port (overrides $PORT)")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	_ = godotenv.Load()

	// Structured JSON logging for the server mode
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}
	if err := cfg.ValidateServerMode(); err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}
	if *port != "" {
		cfg.Port = *port
	}

	ghClient, err := ghclient.NewClient(cfg.GitHubToken)
	if err != nil {
		slog.Error("failed to create GitHub client", "error", err)
		os.Exit(1)
	}

	reviewer, err := llm.NewReviewer(cfg.OpenAIAPIKey, cfg.OpenAIModel)
	if err != nil {
		slog.Error("failed to create LLM reviewer", "error", err)
		os.Exit(1)
	}

	processor := review.NewProcessor(ghClient, reviewer, cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", webhook.NewHandler(cfg.GitHubWebhookSecret, processor))
	mux.HandleFunc("/health", healthHandler)

	addr := fmt.Sprintf(":%s", cfg.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("webhook server starting",
			"addr", addr,
			"model", cfg.OpenAIModel,
			"review_language", cfg.ReviewLanguage,
		)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-quit
	slog.Info("shutting down server …")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}
	slog.Info("server stopped")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ok"}`)
}
