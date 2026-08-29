# Changelog

All notable changes use semantic versioning.

## [Unreleased]

### Added

- Evidence-first modular scanner with fast, balanced, and deep modes.
- Repository, code, issue, pull-request, comment, commit, history, profile, and public-event discovery.
- Structured JSON, JSONL, CSV, Markdown, and SARIF reports.
- Search partitioning, adaptive rate limiting, retries, caching, baselines, safe cancellation, and explicit completeness status.
- Secure Git-history scanning, multi-domain input, authorized repository/organization scopes, and private-resource opt-in.
- Tests, fuzzing, race checks, release automation, container packaging, and community documentation.

### Changed

- The legacy `-d/-o` invocation remains supported, while `ghemails scan` is now the primary interface.
- GitHub tokens are no longer displayed; `-t` is deprecated in favor of environment variables or a token file.
