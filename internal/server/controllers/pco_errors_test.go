package controllers

import (
	"errors"
	"testing"

	pco "github.com/drkgrntt/go-pco"
)

const wantFriendlyMsg = "Your Planning Center account doesn't have permission to do that. Ask whoever manages your church's Planning Center account for Services Editor or Scheduler access."

func TestFriendlyPCOErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "401 request error becomes friendly message",
			err:  &pco.RequestError{StatusCode: 401},
			want: wantFriendlyMsg,
		},
		{
			name: "403 request error becomes friendly message",
			err:  &pco.RequestError{StatusCode: 403},
			want: wantFriendlyMsg,
		},
		{
			name: "500 request error passes through unchanged",
			err:  &pco.RequestError{StatusCode: 500},
			want: (&pco.RequestError{StatusCode: 500}).Error(),
		},
		{
			name: "plain error passes through unchanged",
			err:  errors.New("boom"),
			want: "boom",
		},
		{
			name: "nil returns empty string",
			err:  nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := friendlyPCOErr(tt.err); got != tt.want {
				t.Errorf("friendlyPCOErr(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
