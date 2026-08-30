package controllers

import "testing"

func TestFeedbackValidationErrorRequiresMessage(t *testing.T) {
	cases := []struct {
		name    string
		message string
		wantErr bool
	}{
		{"blank", "", true},
		{"whitespace only", "   \n\t  ", true},
		{"has content", "the build page is great", false},
		{"content with surrounding whitespace", "  looks good  ", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := feedbackValidationError(tc.message)
			if tc.wantErr && got == "" {
				t.Errorf("feedbackValidationError(%q) = %q, want a non-empty error", tc.message, got)
			}
			if !tc.wantErr && got != "" {
				t.Errorf("feedbackValidationError(%q) = %q, want no error", tc.message, got)
			}
		})
	}
}
