package cmd

import (
	"errors"
	"strings"
	"testing"
)

func TestAuthModeMessage(t *testing.T) {
	tests := []struct {
		name      string
		projectID string
		keyPath   string
		contains  []string
	}{
		{
			name:      "empty key_path mentions ADC and project",
			projectID: "moz-fx-foo",
			keyPath:   "",
			contains:  []string{"no key_path", "application default credentials", "moz-fx-foo"},
		},
		{
			name:      "set key_path mentions key file and path",
			projectID: "moz-fx-bar",
			keyPath:   "/etc/keys/x.json",
			contains:  []string{"service account key file", "moz-fx-bar", "/etc/keys/x.json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := authModeMessage(tt.projectID, tt.keyPath)
			for _, sub := range tt.contains {
				if !strings.Contains(got, sub) {
					t.Errorf("authModeMessage(%q,%q) = %q; missing %q",
						tt.projectID, tt.keyPath, got, sub)
				}
			}
		})
	}
}

func TestAdcHintIfMissing(t *testing.T) {
	hint := "gcloud auth application-default login"

	t.Run("ADC mode + matching error -> hint", func(t *testing.T) {
		err := errors.New("google: could not find default credentials. See ...")
		got := adcHintIfMissing(err, "")
		if !strings.Contains(got, hint) {
			t.Errorf("expected hint containing %q, got %q", hint, got)
		}
	})

	t.Run("ADC mode + unrelated error -> empty", func(t *testing.T) {
		err := errors.New("some other failure")
		if got := adcHintIfMissing(err, ""); got != "" {
			t.Errorf("expected empty hint, got %q", got)
		}
	})

	t.Run("key_file mode + matching error -> empty (not user's problem)", func(t *testing.T) {
		err := errors.New("google: could not find default credentials")
		if got := adcHintIfMissing(err, "/etc/keys/x.json"); got != "" {
			t.Errorf("expected empty hint when key_path is set, got %q", got)
		}
	})

	t.Run("nil error -> empty", func(t *testing.T) {
		if got := adcHintIfMissing(nil, ""); got != "" {
			t.Errorf("expected empty hint for nil error, got %q", got)
		}
	})
}
