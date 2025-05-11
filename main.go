package main

import (
	"context"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/apex/gateway/v2"
	"github.com/aws/aws-lambda-go/lambdacontext"
	"github.com/gregdel/pushover"
)

type contextKey string

const loggerContextKey = contextKey("logger")

// getLoggerFromContext retrieves the logger from context or returns default
func getLoggerFromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerContextKey).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

// loggingMiddleware logs request details and adds a contextual logger
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := getRequestID(r.Context())
		w.Header().Set("X-Request-ID", requestID)

		// Configure logger and execute with context
		logger := slog.Default().With("requestId", requestID)
		ctx := context.WithValue(r.Context(), loggerContextKey, logger)

		logger.Info("request", "start", true, "method", r.Method, "path", r.URL.Path,
			"remote", r.RemoteAddr, "agent", r.UserAgent())

		next.ServeHTTP(w, r.WithContext(ctx))

		logger.Info("request", "complete", true, "method", r.Method, "path", r.URL.Path,
			"duration_ms", time.Since(start).Milliseconds())
	})
}

// getRequestID gets request ID from context sources
func getRequestID(ctx context.Context) string {
	if lc, ok := lambdacontext.FromContext(ctx); ok && lc.AwsRequestID != "" {
		return lc.AwsRequestID
	}
	if ctx, ok := gateway.RequestContext(ctx); ok {
		return ctx.RequestID
	}
	return ""
}

// PushoverClient interface for sending notifications
type PushoverClient interface {
	SendMessage(message *pushover.Message, recipient *pushover.Recipient) (*pushover.Response, error)
}

// DefaultPushoverClient wraps the standard Pushover client
type DefaultPushoverClient struct {
	client *pushover.Pushover
}

func NewDefaultPushoverClient(token string) *DefaultPushoverClient {
	return &DefaultPushoverClient{client: pushover.New(token)}
}

func (c *DefaultPushoverClient) SendMessage(message *pushover.Message, recipient *pushover.Recipient) (*pushover.Response, error) {
	return c.client.SendMessage(message, recipient)
}

// createMainHandler creates the HTTP handler for POST requests
func createMainHandler(pushoverClient PushoverClient, pushoverUserKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := getLoggerFromContext(r.Context())

		// Only allow POST
		if r.Method != http.MethodPost {
			logger.Info("method not allowed", "method", r.Method)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("X-Version", os.Getenv("VERSION"))

		// Read request body
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Error("failed reading request body", "error", err)
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}
		defer func() {
			if err := r.Body.Close(); err != nil {
				logger.Error("failed closing request body", "error", err)
			}
		}()

		logger.Info("received payload", "bytes", len(bodyBytes), "content", string(bodyBytes))

		// Send notification in one step
		response, err := pushoverClient.SendMessage(
			pushover.NewMessage(string(bodyBytes)),
			pushover.NewRecipient(pushoverUserKey),
		)
		if err != nil {
			logger.Error("failed sending pushover message", "error", err)
			http.Error(w, "Error sending notification", http.StatusInternalServerError)
			return
		}

		logger.Info("notification sent", "status", response.Status, "id", response.ID)
		if _, err = w.Write([]byte("Notification sent.\n")); err != nil {
			logger.Error("writing response", "error", err)
		}
	}
}

func main() {
	// Check required environment variables
	pushoverToken := os.Getenv("PUSHOVER_TOKEN")
	pushoverUserKey := os.Getenv("PUSHOVER_USER_KEY")
	if pushoverToken == "" || pushoverUserKey == "" {
		log.Fatal("PUSHOVER_TOKEN and PUSHOVER_USER_KEY must be set")
	}

	// Configure logger
	isLambda := os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != ""
	var baseHandler slog.Handler
	if isLambda {
		baseHandler = slog.NewJSONHandler(os.Stdout, nil)
	} else {
		baseHandler = slog.NewTextHandler(os.Stdout, nil)
	}
	slog.SetDefault(slog.New(baseHandler.WithAttrs([]slog.Attr{
		slog.String("version", os.Getenv("VERSION")),
	})))

	// Setup handler and start server
	http.Handle("/", loggingMiddleware(createMainHandler(
		NewDefaultPushoverClient(pushoverToken),
		pushoverUserKey,
	)))

	var err error
	if isLambda {
		slog.Info("starting lambda mode")
		err = gateway.ListenAndServe("", nil)
	} else {
		err = http.ListenAndServe(":"+os.Getenv("PORT"), nil)
	}
	slog.Error("server stopped", "error", err)
}
