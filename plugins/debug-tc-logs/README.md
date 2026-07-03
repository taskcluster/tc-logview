# debug-tc-logs (Claude Code plugin)

A Claude Code skill for debugging Taskcluster — task failures, worker problems,
API errors, and queue issues — by querying task status/logs via the
`taskcluster` CLI and GCP Cloud Logging via [`tc-logview`](https://github.com/taskcluster/tc-logview),
across the `community-tc`, `fx-ci`, `staging`, and `dev` environments.

## Prerequisites

This plugin drives external tools; it does not install them. Before using the
skill, make sure you have:

1. **`tc-logview`** on your `PATH`:
   ```bash
   go install github.com/taskcluster/tc-logview@latest
   ```
2. **The `taskcluster` CLI** installed and `TASKCLUSTER_ROOT_URL` set for your
   target deployment.
3. **GCP service-account keys** for the environments you query, then:
   ```bash
   tc-logview config init
   # place key files under ~/.config/tc-logview/keys/
   tc-logview sync
   ```

The skill checks for these at runtime and tells you what is missing.

## Install

```bash
/plugin marketplace add taskcluster/tc-logview
/plugin install debug-tc-logs@tc-logview
```

Then invoke it in a session with `/debug-tc-logs`, or just ask Claude to debug a
Taskcluster issue.

## Contributors

- Yaraslau Kurmyza (ykurmyza@mozilla.com) — author

## License

MPL-2.0 (see the repository `LICENSE`).
