package cmd

import (
	"fmt"
	"os"

	"github.com/lotas/tc-logview/internal/config"
	"github.com/spf13/cobra"
)

var (
	envFlag string
	cfg     *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "tc-logview",
	Short: "Query GCP Cloud Logging for Taskcluster services",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "init" {
			return nil
		}
		var err error
		cfg, err = config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&envFlag, "env", "e", "", "environment name")
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
				fmt.Fprintf(os.Stderr, "Auto-detected environment: %s\n", name)
				return &e, nil
			}
		}
		return nil, fmt.Errorf("TASKCLUSTER_ROOT_URL=%q does not match any environment", rootURL)
	}
	return nil, fmt.Errorf("specify --env or set TASKCLUSTER_ROOT_URL, available envs: %v", cfg.EnvNames())
}
