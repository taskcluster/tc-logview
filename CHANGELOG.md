# Changelog

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
