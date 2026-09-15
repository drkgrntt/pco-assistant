package auth

import "testing"

// TestDeviceLabel is pure string logic - no DB, so it runs under a plain
// `go test ./internal/auth/...` with no RUN_DB_TESTS gating (unlike
// sessions_test.go, see that file's doc comment).
func TestDeviceLabel(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
		want      string
	}{
		{
			name:      "empty string",
			userAgent: "",
			want:      "Unknown device",
		},
		{
			name:      "Chrome on Windows",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			want:      "Chrome on Windows",
		},
		{
			name:      "Safari on iPhone",
			userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
			want:      "Safari on iPhone",
		},
		{
			name:      "Firefox on Mac",
			userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:120.0) Gecko/20100101 Firefox/120.0",
			want:      "Firefox on Mac",
		},
		{
			name:      "Edge on Windows (must not say Chrome)",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
			want:      "Edge on Windows",
		},
		{
			name:      "Chrome on Android",
			userAgent: "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
			want:      "Chrome on Android",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeviceLabel(tt.userAgent); got != tt.want {
				t.Errorf("DeviceLabel(%q) = %q, want %q", tt.userAgent, got, tt.want)
			}
		})
	}
}
