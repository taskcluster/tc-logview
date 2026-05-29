# Changelog

## v1.3.1 - 2026-05-29

### Changed

- Updated version number
- Dependabot security updates

## v1.3.0 - 2026-05-28

### Added

- Pre-issued OAuth2 access tokens via the `TC_LOGVIEW_ACCESS_TOKEN` env var. Intended for containers/agents that should run without access to the host's `gcloud` session or a long-lived service account key. The host mints a short-lived, SA-scoped token (e.g. `gcloud auth print-access-token --impersonate-service-account=<SA>`) and injects only the token; tc-logview itself does not impersonate or refresh.
- Auth precedence is now `TC_LOGVIEW_ACCESS_TOKEN` > `key_path` > ADC; the active mode is logged under `-v` as `token`, `key_file`, or `ADC`. Client-create and query errors are tagged with the active mode for diagnosability.
- Expired/invalid token errors append a one-line hint pointing at the re-mint command.

### Changed

- `config init` template's authentication comment block now documents all three modes and the priority order.
- README Setup section gains Option C describing the host-mints-token / container-consumes-token flow.

## v1.2.0 - 2026-05-28

### Added

- `tc-logview version` subcommand and `--version` / `-V` flag print the version, embedded commit SHA (from Go build info), and Go runtime version.

## v1.1.0 - 2026-05-28

### Added

- Application Default Credentials (ADC) as a fallback auth mode: when `key_path` is unset for an environment, tc-logview now uses the GCP SDK's default credential chain (e.g. credentials from `gcloud auth application-default login`). Service-account JSON keys remain supported for untrusted/containerized environments.
- Active auth mode is logged to stderr under `-v` (one line per project queried). Client-create errors are tagged with `auth=ADC` or `auth=key_file` so misconfigurations are unambiguous.
- When ADC is selected but not configured, the error output appends a one-line hint pointing at `gcloud auth application-default login` or `key_path`.

### Changed

- `config init` template now documents both auth options and leaves the `dev` environment without `key_path` so newly-generated configs default that environment to ADC.
- README Setup section split into Option A (ADC) and Option B (SA key file).

## v1.0.0 - 2026-05-28

Initial release of `tc-logview`.

### Added

- Query GCP Cloud Logging for Taskcluster services with schema-aware filters.
- Sync and list Taskcluster log type references.
- Support shorthand field filters, raw GCP filters, and service narrowing.
- Output logs as columns, JSONL, or raw GCP entries.
- Cache absolute time-window queries for reuse.
- Include Kubernetes and CloudSQL log presets.
- Manage multiple Taskcluster environments and service-account key paths from config.
