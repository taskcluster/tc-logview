package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/taskcluster/tc-logview/internal/config"
	"github.com/taskcluster/tc-logview/internal/gcp"
)

// accessTokenEnv is the env var that injects a pre-issued OAuth2 bearer
// token into tc-logview. Set by host orchestration (e.g. a wrapper script
// running `gcloud auth print-access-token --impersonate-service-account=...`)
// to give a container short-lived, SA-scoped credentials without mounting
// the host's gcloud session.
const accessTokenEnv = "TC_LOGVIEW_ACCESS_TOKEN"

var (
	envFlag string
	verbose bool
	cfg     *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "tc-logview",
	Short: "Query GCP Cloud Logging for Taskcluster services",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if showVersion, _ := cmd.Flags().GetBool("version"); showVersion {
			fmt.Fprintln(cmd.OutOrStdout(), versionString())
			os.Exit(0)
		}
		if cmd.Name() == "init" || cmd.Name() == "version" || !cmd.HasParent() {
			return nil
		}
		var err error
		cfg, err = config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if showVersion, _ := cmd.Flags().GetBool("version"); showVersion {
			fmt.Fprintln(cmd.OutOrStdout(), versionString())
			return nil
		}
		return cmd.Help()
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&envFlag, "env", "e", "", "environment name")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "show diagnostic messages")
	rootCmd.PersistentFlags().BoolP("version", "V", false, "show version and exit")
}

func logInfo(format string, args ...any) {
	if verbose {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

// resolveAuth assembles the AuthConfig for the current invocation by combining
// the env-var token (if present) with the per-env key_path from config.
func resolveAuth(env *config.Environment) gcp.AuthConfig {
	return gcp.AuthConfig{
		KeyPath:     env.KeyPath,
		AccessToken: os.Getenv(accessTokenEnv),
	}
}

// authModeMessage returns a human-readable description of which auth mode
// the gcp client will use for the given project. Intended for stderr
// diagnostics under -v.
func authModeMessage(projectID string, auth gcp.AuthConfig) string {
	switch {
	case auth.AccessToken != "":
		return fmt.Sprintf(
			"auth: using injected access token from %s (project=%s)",
			accessTokenEnv, projectID,
		)
	case auth.KeyPath != "":
		return fmt.Sprintf(
			"auth: using service account key file (project=%s, path=%s)",
			projectID, auth.KeyPath,
		)
	default:
		return fmt.Sprintf(
			"auth: no key_path configured, using application default credentials (project=%s)",
			projectID,
		)
	}
}

// authHint returns a one-line, mode-specific hint when a GCP error suggests
// a fixable auth misconfiguration. Empty string when no hint applies.
func authHint(err error, auth gcp.AuthConfig) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case auth.AccessToken != "" && (strings.Contains(msg, "invalid") || strings.Contains(msg, "expired") || strings.Contains(msg, "401")):
		return fmt.Sprintf("hint: %s may be expired (~1h TTL).\n      Re-mint: gcloud auth print-access-token --impersonate-service-account=<SA>", accessTokenEnv)
	case auth.AccessToken == "" && auth.KeyPath == "" && strings.Contains(msg, "could not find default credentials"):
		return "hint: run `gcloud auth application-default login`, or set `key_path` in ~/.config/tc-logview/config.yaml"
	}
	return ""
}

func Execute() error {
	return rootCmd.Execute()
}

func resolveEnv() (*config.Environment, error) {
	if envFlag != "" {
		env, ok := cfg.Environments[envFlag]
		if !ok {
			return nil, fmt.Errorf("unknown environment %q, available: %v", envFlag, cfg.EnvNames())
		}
		return &env, nil
	}
	rootURL := os.Getenv("TASKCLUSTER_ROOT_URL")
	if rootURL != "" {
		// Auto-detection deliberately ignores log-view-scoped environments:
		// multiple envs (broad + scoped) can share a root_url, and a scoped env
		// uses different credentials/scope. Scoped access must be opted into
		// explicitly with --env, so detection resolves to the broad env only.
		var scopedMatch string
		for name, env := range cfg.Environments {
			if env.RootURL != rootURL {
				continue
			}
			if env.LogViewResource() != "" {
				scopedMatch = name
				continue
			}
			e := env
			logInfo("Auto-detected environment: %s", name)
			return &e, nil
		}
		if scopedMatch != "" {
			return nil, fmt.Errorf("TASKCLUSTER_ROOT_URL=%q matches only the scoped environment %q; select it explicitly with --env", rootURL, scopedMatch)
		}
		return nil, fmt.Errorf("TASKCLUSTER_ROOT_URL=%q does not match any environment", rootURL)
	}
	return nil, fmt.Errorf("specify --env or set TASKCLUSTER_ROOT_URL, available envs: %v", cfg.EnvNames())
}
