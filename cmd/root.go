package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/taskcluster/tc-logview/internal/config"
	"github.com/spf13/cobra"
)

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

// authModeMessage returns a human-readable description of which auth mode
// the gcp client will use for the given project. Intended for stderr
// diagnostics under -v.
func authModeMessage(projectID, keyPath string) string {
	if keyPath == "" {
		return fmt.Sprintf(
			"auth: no key_path configured, using application default credentials (project=%s)",
			projectID,
		)
	}
	return fmt.Sprintf(
		"auth: using service account key file (project=%s, path=%s)",
		projectID, keyPath,
	)
}

// adcHintIfMissing returns a one-line hint when a GCP client-create error
// indicates that Application Default Credentials are not configured AND
// the user has no key_path set. Empty string in any other case.
func adcHintIfMissing(err error, keyPath string) string {
	if err == nil || keyPath != "" {
		return ""
	}
	if !strings.Contains(err.Error(), "could not find default credentials") {
		return ""
	}
	return "hint: run `gcloud auth application-default login`, or set `key_path` in ~/.config/tc-logview/config.yaml"
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
		for name, env := range cfg.Environments {
			if env.RootURL == rootURL {
				e := env
				logInfo("Auto-detected environment: %s", name)
				return &e, nil
			}
		}
		return nil, fmt.Errorf("TASKCLUSTER_ROOT_URL=%q does not match any environment", rootURL)
	}
	return nil, fmt.Errorf("specify --env or set TASKCLUSTER_ROOT_URL, available envs: %v", cfg.EnvNames())
}
