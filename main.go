package main

import (
	"fmt"
	"io" // Import io package
	"log"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"github.com/apex/gateway/v2"
	"github.com/gregdel/pushover"
)

var gitCommit string // Added to store the git commit hash

// loggingMiddleware logs request details including method, path, user agent, and duration
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Log basic request information
		slog.Info("request started",
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)

		// Call the next handler
		next.ServeHTTP(w, r)

		// Log request duration
		duration := time.Since(start)
		slog.Info("request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", duration.Milliseconds(),
		)
	})
}

// PushoverClient defines the interface for sending push notifications
type PushoverClient interface {
	SendMessage(message *pushover.Message, recipient *pushover.Recipient) (*pushover.Response, error)
}

// DefaultPushoverClient is the standard Pushover client implementation
type DefaultPushoverClient struct {
	client *pushover.Pushover
}

// NewDefaultPushoverClient creates a new default client
func NewDefaultPushoverClient(token string) *DefaultPushoverClient {
	return &DefaultPushoverClient{
		client: pushover.New(token),
	}
}

// SendMessage sends a push notification using the Pushover API
func (c *DefaultPushoverClient) SendMessage(message *pushover.Message, recipient *pushover.Recipient) (*pushover.Response, error) {
	return c.client.SendMessage(message, recipient)
}

// createMainHandler creates the main HTTP handler for handling POST requests
// with Pushover integration
func createMainHandler(pushoverClient PushoverClient, pushoverUserKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// only respond to post requests
		if r.Method != http.MethodPost {
			slog.Info("method not allowed", "method", r.Method)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Use gitCommit for version information
		if gitCommit == "" {
			// Fallback or error if gitCommit is not set, though ldflags should set it.
			// For now, we can try to read build info as a fallback, but ideally gitCommit is always present.
			build, ok := debug.ReadBuildInfo()
			if ok {
				w.Header().Set("X-Version", build.Main.Version) // Fallback to build info if gitCommit is empty
			} else {
				w.Header().Set("X-Version", "unknown")
			}
		} else {
			w.Header().Set("X-Version", gitCommit)
		}

		// Create a new recipient with the user key from env var
		recipient := pushover.NewRecipient(pushoverUserKey)

		// Read HTTP POST request body
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			slog.Error("failed reading request body", "error", err)
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Log the request payload
		slog.Info("received POST payload", "content_length", len(bodyBytes), "payload", string(bodyBytes))

		// Create a new message with the body content
		message := pushover.NewMessage(string(bodyBytes))

		// Send the message to the recipient
		response, err := pushoverClient.SendMessage(message, recipient)
		if err != nil {
			slog.Error("failed sending pushover message", "error", err)
			http.Error(w, "Error sending notification", http.StatusInternalServerError)
			return
		}

		// Log successful push notification
		slog.Info("pushover notification sent", "status", response.Status, "response_id", response.ID)

		versionInfo := gitCommit
		if versionInfo == "" {
			// Fallback for version info in response body
			build, ok := debug.ReadBuildInfo()
			if ok {
				versionInfo = build.Main.Version
			} else {
				versionInfo = "unknown"
			}
		}

		_, err = w.Write([]byte("Notification sent.")) // Updated response
		if err != nil {
			slog.Error("writing response", "error", err)
		}
	}
}

func main() {
	// Read Pushover credentials from environment variables
	pushoverToken := os.Getenv("PUSHOVER_TOKEN")
	pushoverUserKey := os.Getenv("PUSHOVER_USER_KEY")

	if pushoverToken == "" || pushoverUserKey == "" {
		log.Fatal("PUSHOVER_TOKEN and PUSHOVER_USER_KEY environment variables must be set")
	}

	// Create a pushover client
	pushoverClient := NewDefaultPushoverClient(pushoverToken)

	// Set up the handler for the main endpoint
	mainHandler := createMainHandler(pushoverClient, pushoverUserKey)

	// Set up default logger
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	// Apply middleware to the main handler
	http.Handle("/", loggingMiddleware(mainHandler))

	var err error

	if _, ok := os.LookupEnv("AWS_LAMBDA_FUNCTION_NAME"); ok {
		slog.Info("starting in AWS Lambda mode")
		err = gateway.ListenAndServe("", nil)
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080" // Default port if not specified
		}
		slog.Info("starting HTTP server", "port", port)
		err = http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
	}
	slog.Error("server stopped", "error", err)
}
