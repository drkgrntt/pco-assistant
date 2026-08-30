package notify

import (
	"context"
	"testing"
)

func TestHasCredentialsRequiresBothDomainAndKey(t *testing.T) {
	cases := []struct {
		name     string
		domain   string
		key      string
		expected bool
	}{
		{"both set", "mg.example.com", "key-123", true},
		{"domain missing", "", "key-123", false},
		{"key missing", "mg.example.com", "", false},
		{"neither set", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(tc.domain, tc.key, "recipient@example.com")
			if got := c.hasCredentials(); got != tc.expected {
				t.Errorf("hasCredentials() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestNewWithoutCredentialsDoesNotPanic(t *testing.T) {
	c := New("", "", "")
	if c == nil {
		t.Fatal("New returned nil")
	}
	if c.hasCredentials() {
		t.Error("expected hasCredentials() to be false with no domain/key")
	}
}

// TestSendFeedbackWithoutCredentialsLogsInsteadOfSending confirms the
// "no Mailgun creds configured" fallback path (see New's doc comment)
// never touches the network and never errors - it's what makes it safe
// for MG_DOMAIN/MG_KEY to be unset in local dev.
func TestSendFeedbackWithoutCredentialsLogsInsteadOfSending(t *testing.T) {
	c := New("", "", "")
	if err := c.SendFeedback(context.Background(), "someone@example.com", "it broke"); err != nil {
		t.Errorf("SendFeedback() with no credentials returned an error: %v", err)
	}
}
