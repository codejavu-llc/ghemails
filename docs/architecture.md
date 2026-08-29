# Architecture

ghemails is organized as a small set of independently testable layers:

1. The CLI resolves domains, authentication, modes, limits, output, baselines, and exit behavior.
2. The GitHub client owns request construction, approved credential origins, bounded bodies, caching, retries, pagination metadata, and per-resource throttling.
3. Ordered source adapters collect candidate repositories/users and feed directly observed text or metadata into the extractor.
4. The extractor normalizes IDNs, applies exact-domain policy, validates local/domain boundaries, filters noreply identities, and classifies role accounts.
5. The thread-safe aggregator deduplicates findings and evidence and calculates source counts and observation ranges.
6. Reporters serialize the versioned run model without mixing diagnostics into stdout.

## Discovery flow

Repository, code, issue, and commit searches establish direct evidence and seed candidate repositories. Balanced mode walks default-branch commit metadata and enriches only GitHub users already linked to relevant evidence. Deep mode clones only candidate or explicitly scoped repositories, disables prompts, hooks, credential helpers, system/global Git configuration, and local protocols, then scans all refs and historical diffs within byte/time limits.

Searches over GitHub's 1,000-result limit are partitioned by code file size or repository/issue/commit date. A single size/day bucket that still exceeds the limit is retained as partial evidence rather than represented as complete.

## Data model

A run contains target/mode/status metadata, source execution outcomes, and sorted findings. A finding contains an observed normalized email and one or more evidence records. Evidence identifies the GitHub plane plus the strongest available URL, repository, path, line, ref, SHA, actor, and timestamp.

Schema changes require a new `schema_version` and backward-compatible baseline parsing.
