package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gregdel/pushover"
)

// MockPushoverClient implements the PushoverClient interface for testing
type MockPushoverClient struct {
	messages   []*pushover.Message
	recipients []*pushover.Recipient
}

// SendMessage records the messages and recipients for verification
func (m *MockPushoverClient) SendMessage(message *pushover.Message, recipient *pushover.Recipient) (*pushover.Response, error) {
	m.messages = append(m.messages, message)
	m.recipients = append(m.recipients, recipient)
	return &pushover.Response{
		Status: 1,
		ID:     "test-request-id",
	}, nil
}

// TestHandlePostRequest tests our HTTP handler with a mocked pushover client
func TestHandlePostRequest(t *testing.T) {
	// Create mock pushover client
	mockClient := &MockPushoverClient{}

	// Create test request with payload
	testPayload := []byte("Test notification payload")
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(testPayload))

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Create the handler using our mock client
	handler := createMainHandler(mockClient, "test-user-key")

	// Call the handler
	handler.ServeHTTP(recorder, req)

	// Verify response code
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, recorder.Code)
	}

	// Verify response contains expected text
	responseBody := recorder.Body.String()
	if !bytes.Contains(bytes.ToLower([]byte(responseBody)), []byte("notification sent")) {
		t.Errorf("Expected response to contain 'notification sent', got: %s", responseBody)
	}

	// Verify message was sent with correct content
	if len(mockClient.messages) != 1 {
		t.Fatalf("Expected 1 message to be sent, got %d", len(mockClient.messages))
	}

	sentMessage := mockClient.messages[0]
	if sentMessage.Message != string(testPayload) {
		t.Errorf("Expected message content %q, got %q", string(testPayload), sentMessage.Message)
	}

	// Verify recipient was used
	if len(mockClient.recipients) != 1 {
		t.Fatalf("Expected 1 recipient, got %d", len(mockClient.recipients))
	}
}
