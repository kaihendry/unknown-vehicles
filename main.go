package main

import (
	"fmt"
	"io" // Import io package
	"log"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"

	"github.com/apex/gateway/v2"
	"github.com/gregdel/pushover"
)

func main() {
	// Read Pushover credentials from environment variables
	pushoverToken := os.Getenv("PUSHOVER_TOKEN")
	pushoverUserKey := os.Getenv("PUSHOVER_USER_KEY")

	if pushoverToken == "" || pushoverUserKey == "" {
		log.Fatal("PUSHOVER_TOKEN and PUSHOVER_USER_KEY environment variables must be set")
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// only respond to post requests
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		build, ok := debug.ReadBuildInfo()
		if !ok {
			http.Error(w, "No build info available", http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-Version", build.Main.Version)

		// Create a new pushover app with the token from env var
		app := pushover.New(pushoverToken)

		// Create a new recipient with the user key from env var
		recipient := pushover.NewRecipient(pushoverUserKey)

		// Read HTTP POST request body
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			slog.Error("error reading request body", "error", err)
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Create a new message with the body content
		message := pushover.NewMessage(string(bodyBytes))

		// Send the message to the recipient
		response, err := app.SendMessage(message, recipient)
		if err != nil {
			slog.Error("error sending pushover message", "error", err)
			http.Error(w, "Error sending notification", http.StatusInternalServerError)
			return
		}

		// Print the response if you want
		log.Println(response)

		_, err = w.Write([]byte("Notification sent. Version: " + build.Main.Version)) // Updated response
		if err != nil {
			slog.Error("error writing response", "error", err)
		}
	})

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	var err error

	if _, ok := os.LookupEnv("AWS_LAMBDA_FUNCTION_NAME"); ok {
		err = gateway.ListenAndServe("", nil)
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
		err = http.ListenAndServe(fmt.Sprintf(":%s", os.Getenv("PORT")), nil)
	}
	slog.Error("error listening", "error", err)
}
