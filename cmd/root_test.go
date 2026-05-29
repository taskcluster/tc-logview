package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/taskcluster/tc-logview/internal/config"
	"github.com/taskcluster/tc-logview/internal/gcp"
)

func TestAuthModeMessage(t *testing.T) {
	tests := []struct {
		name      string
		projectID string
		auth      gcp.AuthConfig
		contains  []string
	}{
		{
			name:      "empty auth mentions ADC and project",
			projectID: "moz-fx-foo",
			auth:      gcp.AuthConfig{},
			contains:  []string{"no key_path", "application default credentials", "moz-fx-foo"},
		},
		{
			name:      "set key_path mentions key file and path",
			projectID: "moz-fx-bar",
			auth:      gcp.AuthConfig{KeyPath: "/etc/keys/x.json"},
			contains:  []string{"service account key file", "moz-fx-bar", "/etc/keys/x.json"},
		},
		{
			name:      "access token mentions injected token and env var name",
			projectID: "moz-fx-baz",
			auth:      gcp.AuthConfig{AccessToken: "ya29.fake"},
			contains:  []string{"injected access token", accessTokenEnv, "moz-fx-baz"},
		},
		{
			name:      "token wins over key_path",
			projectID: "moz-fx-qux",
			auth:      gcp.AuthConfig{KeyPath: "/etc/keys/x.json", AccessToken: "ya29.fake"},
			contains:  []string{"injected access token", "moz-fx-qux"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := authModeMessage(tt.projectID, tt.auth)
			for _, sub := range tt.contains {
				if !strings.Contains(got, sub) {
					t.Errorf("authModeMessage(%q,%+v) = %q; missing %q",
						tt.projectID, tt.auth, got, sub)
				}
			}
		})
	}
}

func TestResolveEnvAutoDetectSkipsScoped(t *testing.T) {
	prevCfg, prevFlag := cfg, envFlag
	t.Cleanup(func() { cfg, envFlag = prevCfg, prevFlag })

	envFlag = ""
	t.Setenv("TASKCLUSTER_ROOT_URL", "https://firefox-ci-tc.services.mozilla.com")
	cfg = &config.Config{Environments: map[string]config.Environment{
		"fx-ci": {
			ProjectID: "moz-fx-webservices-high-prod",
			RootURL:   "https://firefox-ci-tc.services.mozilla.com",
		},
		"fx-ci-scoped": {
			ProjectID:   "moz-fx-taskcluster-prod",
			RootURL:     "https://firefox-ci-tc.services.mozilla.com",
			LogBucket:   "gke-taskcluster-prod-log-bucket",
			LogLocation: "global",
			LogView:     "_AllLogs",
		},
	}}

	// Map iteration is randomized; a nondeterministic resolveEnv would eventually
	// pick the scoped env. Run enough times to catch that.
	for i := 0; i < 50; i++ {
		env, err := resolveEnv()
		if err != nil {
			t.Fatalf("resolveEnv: %v", err)
		}
		if env.ProjectID != "moz-fx-webservices-high-prod" || env.LogViewResource() != "" {
			t.Fatalf("auto-detect selected scoped env (project=%s, view=%q); want broad env",
				env.ProjectID, env.LogViewResource())
		}
	}
}

func TestResolveEnvScopedOnlyRequiresExplicit(t *testing.T) {
	prevCfg, prevFlag := cfg, envFlag
	t.Cleanup(func() { cfg, envFlag = prevCfg, prevFlag })

	envFlag = ""
	t.Setenv("TASKCLUSTER_ROOT_URL", "https://example.com")
	cfg = &config.Config{Environments: map[string]config.Environment{
		"only-scoped": {ProjectID: "p", RootURL: "https://example.com", LogView: "_AllLogs"},
	}}

	if _, err := resolveEnv(); err == nil {
		t.Fatal("expected error when only a scoped env matches TASKCLUSTER_ROOT_URL")
	}
}

func TestAuthHint(t *testing.T) {
	adcHintFragment := "gcloud auth application-default login"
	tokenHintFragment := "may be expired"

	t.Run("ADC mode + missing-creds error -> ADC hint", func(t *testing.T) {
		err := errors.New("google: could not find default credentials. See ...")
		got := authHint(err, gcp.AuthConfig{})
		if !strings.Contains(got, adcHintFragment) {
			t.Errorf("expected ADC hint containing %q, got %q", adcHintFragment, got)
		}
	})

	t.Run("ADC mode + unrelated error -> empty", func(t *testing.T) {
		err := errors.New("some other failure")
		if got := authHint(err, gcp.AuthConfig{}); got != "" {
			t.Errorf("expected empty hint, got %q", got)
		}
	})

	t.Run("key_file mode + missing-creds error -> empty (not user's problem)", func(t *testing.T) {
		err := errors.New("google: could not find default credentials")
		if got := authHint(err, gcp.AuthConfig{KeyPath: "/etc/keys/x.json"}); got != "" {
			t.Errorf("expected empty hint when key_path is set, got %q", got)
		}
	})

	t.Run("nil error -> empty", func(t *testing.T) {
		if got := authHint(nil, gcp.AuthConfig{}); got != "" {
			t.Errorf("expected empty hint for nil error, got %q", got)
		}
	})

	t.Run("token mode + 401 -> token re-mint hint", func(t *testing.T) {
		err := errors.New("rpc error: code = Unauthenticated desc = Request had invalid authentication credentials. Status 401")
		got := authHint(err, gcp.AuthConfig{AccessToken: "ya29.fake"})
		if !strings.Contains(got, tokenHintFragment) {
			t.Errorf("expected token hint containing %q, got %q", tokenHintFragment, got)
		}
		if !strings.Contains(got, accessTokenEnv) {
			t.Errorf("expected token hint to mention %q, got %q", accessTokenEnv, got)
		}
	})

	t.Run("token mode + expired wording -> token hint", func(t *testing.T) {
		err := errors.New("oauth2: token expired and refresh token is not set")
		got := authHint(err, gcp.AuthConfig{AccessToken: "ya29.fake"})
		if !strings.Contains(got, tokenHintFragment) {
			t.Errorf("expected token hint containing %q, got %q", tokenHintFragment, got)
		}
	})

	t.Run("token mode + unrelated error -> empty", func(t *testing.T) {
		err := errors.New("network unreachable")
		if got := authHint(err, gcp.AuthConfig{AccessToken: "ya29.fake"}); got != "" {
			t.Errorf("expected empty hint for unrelated error, got %q", got)
		}
	})
}
