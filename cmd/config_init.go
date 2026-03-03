package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lotas/tc-logview/internal/config"
	"github.com/spf13/cobra"
)

var forceInit bool

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration management",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate default config file",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgDir := config.ConfigDir()
		cfgPath := config.ConfigPath()
		keysDir := filepath.Join(cfgDir, "keys")

		if !forceInit {
			if _, err := os.Stat(cfgPath); err == nil {
				return fmt.Errorf("config already exists at %s, use --force to overwrite", cfgPath)
			}
		}

		if err := os.MkdirAll(keysDir, 0o755); err != nil {
			return fmt.Errorf("creating directories: %w", err)
		}

		example := `# tc-logview configuration
# Place service account keys in ~/.config/tc-logview/keys/

environments:
  fx-ci:
    project_id: "moz-fx-taskcluster-prod-4b87"
    cluster: "taskcluster-firefoxcitc-v1"
    root_url: "https://firefox-ci-tc.services.mozilla.com"
    key_path: "~/.config/tc-logview/keys/tc-prod.json"
  community-tc:
    project_id: "moz-fx-taskcluster-prod-4b87"
    cluster: "taskcluster-communitytc-v1"
    root_url: "https://community-tc.services.mozilla.com"
    key_path: "~/.config/tc-logview/keys/tc-prod.json"
`
		if err := os.WriteFile(cfgPath, []byte(example), 0o644); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Config created at %s\n", cfgPath)
		fmt.Fprintf(cmd.OutOrStdout(), "Keys directory: %s\n", keysDir)
		fmt.Fprintln(cmd.OutOrStdout(), "\nNext steps:")
		fmt.Fprintln(cmd.OutOrStdout(), "  1. Place your GCP service account key(s) in the keys directory")
		fmt.Fprintln(cmd.OutOrStdout(), "  2. Edit the config to match your environments")
		fmt.Fprintln(cmd.OutOrStdout(), "  3. Run 'tc-logview sync' to fetch log type references")
		return nil
	},
}

func init() {
	configInitCmd.Flags().BoolVar(&forceInit, "force", false, "overwrite existing config")
	configCmd.AddCommand(configInitCmd)
	rootCmd.AddCommand(configCmd)
}
