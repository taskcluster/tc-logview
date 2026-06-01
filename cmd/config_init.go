package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/taskcluster/tc-logview/internal/config"
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
#
# Authentication (priority: TC_LOGVIEW_ACCESS_TOKEN > key_path > ADC):
#   - Export ` + "`TC_LOGVIEW_ACCESS_TOKEN`" + ` to use a pre-issued OAuth2 bearer
#     token (short-lived; ideal for containers/agents — host mints via
#     ` + "`gcloud auth print-access-token --impersonate-service-account=<SA>`" + `).
#   - Set ` + "`key_path`" + ` to use a service account JSON key
#     (long-lived; intended for environments that cannot refresh tokens).
#   - Omit ` + "`key_path`" + ` to use Application Default Credentials
#     (run ` + "`gcloud auth application-default login`" + ` once on your machine).
#
# Place service account keys in ~/.config/tc-logview/keys/

environments:
  fx-ci:
    project_id: "moz-fx-webservices-high-prod"
    cluster: "webservices-high-prod"
    namespace: "taskcluster-prod"
    cloudsql_project_id: "moz-fx-taskcluster-prod"
    cloudsql_instance: "taskcluster-prod-20260409-1"
    root_url: "https://firefox-ci-tc.services.mozilla.com"
    key_path: "~/.config/tc-logview/keys/tc-prod.json"
  community-tc:
    project_id: "moz-fx-webservices-high-prod"
    cluster: "webservices-high-prod"
    namespace: "taskcluster-communitytc"
    cloudsql_project_id: "moz-fx-taskcluster-prod"
    cloudsql_instance: "taskcluster-community-20260317-1"
    root_url: "https://community-tc.services.mozilla.com"
    key_path: "~/.config/tc-logview/keys/tc-prod.json"
  staging:
    project_id: "moz-fx-webservices-high-nonpro"
    cluster: "webservices-high-nonprod"
    namespace: "taskcluster-stage"
    root_url: "https://stage.taskcluster.nonprod.cloudops.mozgcp.net"
    key_path: "~/.config/tc-logview/keys/tc-staging.json"
  dev:
    project_id: "taskcluster-dev"
    cluster: "taskcluster-dev"
    root_url: "https://tc.dev.taskcluster.mozgcp.net"
    # key_path omitted — uses ADC by default

  # Scoped environments for untrusted agents/containers. These query a single
  # Cloud Logging log view (a per-namespace tenant bucket) instead of the whole
  # project, so an injected TC_LOGVIEW_ACCESS_TOKEN minted from the narrow
  # tc-logview-reader service account can read ONLY TaskCluster service logs.
  # Infra/k8s/CloudSQL presets are not available here — use the broad envs above.
  fx-ci-scoped:
    project_id: "moz-fx-taskcluster-prod"
    cluster: "webservices-high-prod"
    namespace: "taskcluster-prod"
    log_bucket: "gke-taskcluster-prod-log-bucket"
    log_location: "global"
    log_view: "_AllLogs"
    root_url: "https://firefox-ci-tc.services.mozilla.com"
  community-tc-scoped:
    project_id: "moz-fx-taskcluster-prod"
    cluster: "webservices-high-prod"
    namespace: "taskcluster-communitytc"
    log_bucket: "gke-taskcluster-communitytc-log-bucket"
    log_location: "global"
    log_view: "_AllLogs"
    root_url: "https://community-tc.services.mozilla.com"
  staging-scoped:
    project_id: "moz-fx-webservices-high-nonpro"
    cluster: "webservices-high-nonprod"
    namespace: "taskcluster-stage"
    log_bucket: "gke-taskcluster-stage-log-bucket"
    log_location: "global"
    log_view: "_AllLogs"
    root_url: "https://stage.taskcluster.nonprod.cloudops.mozgcp.net"
`
		if err := os.WriteFile(cfgPath, []byte(example), 0o644); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Config created at %s\n", cfgPath)
		fmt.Fprintf(cmd.OutOrStdout(), "Keys directory: %s\n", keysDir)
		fmt.Fprintln(cmd.OutOrStdout(), "\nNext steps:")
		fmt.Fprintln(cmd.OutOrStdout(), "  1. Authentication — choose one per environment:")
		fmt.Fprintln(cmd.OutOrStdout(), "     a) Place a GCP service account JSON key in the keys directory and reference it via key_path")
		fmt.Fprintln(cmd.OutOrStdout(), "     b) Or run `gcloud auth application-default login` and omit key_path (ADC)")
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
