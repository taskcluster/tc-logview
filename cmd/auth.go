package cmd

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/taskcluster/tc-logview/internal/config"
)

// readerServiceAccount is the dedicated, read-only log-reader identity that
// backs the *-scoped environments. It holds no key and only
// roles/logging.viewAccessor on the TaskCluster per-namespace log buckets, so a
// token minted from it can read only TaskCluster logs. Members of
// workgroup:taskcluster/admins may impersonate it.
const readerServiceAccount = "tc-logview-reader@moz-fx-taskcluster-prod.iam.gserviceaccount.com"

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Show how to authenticate and mint scoped tokens",
	Long:  "Print authentication options, config setup, and how to mint a least-privilege scoped token for agents/containers.",
	RunE: func(cmd *cobra.Command, args []string) error {
		w := cmd.OutOrStdout()

		fmt.Fprint(w, `tc-logview authentication
=========================

1. Config

   tc-logview reads ~/.config/tc-logview/config.yaml. Create it with:

       tc-logview config init

2. Authentication modes (precedence: token > key_path > ADC)

   ADC      Your own gcloud session (broad — your identity's privileges).
            Run once:  gcloud auth application-default login
            Use when:  local human use; omit key_path for that environment.

   key_file A service account JSON key referenced by key_path in config.
            Use when:  a long-lived credential is needed and the host
                       cannot refresh short-lived tokens.

   token    A pre-issued OAuth2 bearer token in TC_LOGVIEW_ACCESS_TOKEN.
            Use when:  untrusted containers/agents (see section 3).

`)
		fmt.Fprintf(w, `3. Scoped, least-privilege token for agents/containers

   The %s
   service account can read ONLY TaskCluster logs. Mint a short-lived (~1h)
   token from it on a trusted host and inject it into the container:

       TOKEN=$(gcloud auth print-access-token \
         --impersonate-service-account=%s)

       TC_LOGVIEW_ACCESS_TOKEN=$TOKEN \
         tc-logview query -e fx-ci-scoped --type monitor.error --since 1h

   Requires roles/iam.serviceAccountTokenCreator on that SA (granted to
   workgroup:taskcluster/admins). The container never sees your gcloud
   session and cannot mint or refresh tokens.

   Note: infra presets (k8s.*, cloudsql.*) are not available on *-scoped
   envs — use a broad env with your own ADC for those.

`, readerServiceAccount, readerServiceAccount)

		printEnvironments(w)
		return nil
	},
}

// printEnvironments lists configured environments (marking scoped ones), or
// points the user at `config init` when the config is missing/unreadable.
func printEnvironments(w io.Writer) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(w, "4. Environments\n\n   No readable config — run `tc-logview config init` to create one.\n")
		return
	}

	names := make([]string, 0, len(cfg.Environments))
	for name := range cfg.Environments {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Fprint(w, "4. Configured environments\n\n")
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "   ENV\tPROJECT\tSCOPE")
	for _, name := range names {
		env := cfg.Environments[name]
		scope := "project (broad)"
		if env.LogViewResource() != "" {
			scope = "log view (scoped)"
		}
		fmt.Fprintf(tw, "   %s\t%s\t%s\n", name, env.ProjectID, scope)
	}
	tw.Flush()
}

func init() {
	rootCmd.AddCommand(authCmd)
}
