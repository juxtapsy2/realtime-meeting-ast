package intelligence

import "testing"

func TestProviderHTTPErrorPermanent(t *testing.T) {
	tests := []struct {
		status int
		want   bool
	}{
		{401, true},  // invalid key
		{402, true},  // billing exhausted
		{403, true},  // forbidden
		{400, false}, // transient until input changes (chunking handles size)
		{408, false},
		{429, false},
		{500, false},
		{503, false},
	}
	for _, tt := range tests {
		e := &ProviderHTTPError{StatusCode: tt.status}
		if got := e.Permanent(); got != tt.want {
			t.Errorf("ProviderHTTPError{StatusCode: %d}.Permanent() = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestProviderHTTPErrorFallbackMessage(t *testing.T) {
	e := &ProviderHTTPError{StatusCode: 402}
	if got, want := e.Error(), "LLM API error (status 402)"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	e.Message = "custom"
	if got := e.Error(); got != "custom" {
		t.Errorf("Error() = %q, want custom message", got)
	}
}
