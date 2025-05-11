package main

import (
	"context" // Import context package
	"fmt"
	"io" // Import io package
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/apex/gateway/v2"
	"github.com/aws/aws-lambda-go/lambdacontext" // Import for Lambda context
	"github.com/gregdel/pushover"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const loggerContextKey = contextKey("logger")

// getLoggerFromContext retrieves the logger from context or returns the default logger.
func getLoggerFromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerContextKey).(*slog.Logger); ok {
		return logger
	}
	return slog.Default() // Fallback to default logger
}

// loggingMiddleware logs request details and injects a logger with requestId into the context.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := getRequestID(r.Context())

		// Add requestID to response header
		w.Header().Set("X-Request-ID", requestID)

		// Create a logger with requestId pre-configured
		logger := slog.Default().With("requestId", requestID)

		// Store logger in context
		ctx := context.WithValue(r.Context(), loggerContextKey, logger)
		r = r.WithContext(ctx)

		// Log basic request information using the new logger
		logger.Info("request started",
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)

		// Call the next handler
		next.ServeHTTP(w, r)

		// Log request duration using the new logger
		duration := time.Since(start)
		logger.Info("request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", duration.Milliseconds(),
		)
	})
}

// getRequestID extracts the AWS Lambda request ID from the context if available, falling back to APIGateway request ID.
func getRequestID(ctx context.Context) string {
	// Try to get the Lambda invocation ID first
	if lc, ok := lambdacontext.FromContext(ctx); ok {
		if lc.AwsRequestID != "" {
			return lc.AwsRequestID
		}
	}

	// Fallback to API Gateway request ID provided by apex/gateway
	if lambdaCtx, ok := gateway.RequestContext(ctx); ok {
		return lambdaCtx.RequestID
	}

	// If neither is found, return an empty string or a custom placeholder
	return "" // Return empty if not found
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
		logger := getLoggerFromContext(r.Context()) // Retrieve logger from context

		// only respond to post requests
		if r.Method != http.MethodPost {
			logger.Info("method not allowed", "method", r.Method)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Use VERSION environment variable for version information
		version := os.Getenv("VERSION")
		w.Header().Set("X-Version", version)

		// Create a new recipient with the user key from env var
		recipient := pushover.NewRecipient(pushoverUserKey)

		// Read HTTP POST request body
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Error("failed reading request body", "error", err)
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}
		defer func() {
			err := r.Body.Close()
			if err != nil {
				logger.Error("failed closing request body", "error", err)
			}
		}()

		// Log the request payload
		logger.Info("received POST payload", "content_length", len(bodyBytes), "payload", string(bodyBytes))

		// Create a new message with the body content
		message := pushover.NewMessage(string(bodyBytes))

		// Send the message to the recipient
		response, err := pushoverClient.SendMessage(message, recipient)
		if err != nil {
			logger.Error("failed sending pushover message", "error", err)
			http.Error(w, "Error sending notification", http.StatusInternalServerError)
			return
		}

		// Log successful push notification
		logger.Info("pushover notification sent", "status", response.Status, "response_id", response.ID)

		_, err = w.Write([]byte("Notification sent.\n")) // Updated response
		if err != nil {
			logger.Error("writing response", "error", err)
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

	appVersion := os.Getenv("VERSION") // Get application version

	// Create a pushover client
	pushoverClient := NewDefaultPushoverClient(pushoverToken)

	// Set up the handler for the main endpoint
	mainHandler := createMainHandler(pushoverClient, pushoverUserKey)

	// Configure logger with version based on environment
	var baseHandler slog.Handler
	isLambda := false
	if _, ok := os.LookupEnv("AWS_LAMBDA_FUNCTION_NAME"); ok {
		isLambda = true
		baseHandler = slog.NewJSONHandler(os.Stdout, nil)
	} else {
		baseHandler = slog.NewTextHandler(os.Stdout, nil)
	}

	// Create logger with version attribute
	loggerWithVersion := slog.New(baseHandler.WithAttrs([]slog.Attr{slog.String("version", appVersion)}))
	slog.SetDefault(loggerWithVersion) // Set this as the default logger

	// Apply middleware to the main handler
	http.Handle("/", loggingMiddleware(mainHandler))

	var err error

	if isLambda {
		slog.Info("starting in AWS Lambda mode")
		err = gateway.ListenAndServe("", nil)
	} else {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080" // Default port if not specified
		}
		slog.Info("starting HTTP server", "port", port)
		err = http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
	}
	slog.Error("server stopped", "error", err)
}
