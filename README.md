# ghemails

Evidence-first GitHub email reconnaissance for authorized bug-bounty, attack-surface, and defensive security work.

ghemails searches multiple GitHub evidence planes, normalizes directly observed target-domain addresses, and preserves where every finding came from. It does not guess addresses or claim that an observed mailbox is deliverable.

## Why ghemails

- Searches code, repository READMEs, issues, pull requests, comments, commit messages, commit identities, public profiles/events, and Git history.
- Recursively partitions oversized searches where GitHub allows it and reports irreducible limits as partial instead of silently dropping coverage.
- Emits reproducible repository, path, line, ref, commit, actor, timestamp, and URL evidence.
- Honors GitHub's actual rate-limit headers and retry guidance without rotating tokens to evade quotas.
- Supports resumable caching, baselines, bounded workers, safe cancellation, and automation-friendly exit codes.
- Produces text, JSON, JSONL, CSV, Markdown, and SARIF reports.

## Install

Download a signed, checksummed binary from the Releases page, or install with Go:

```bash
go install github.com/codejavu-llc/ghemails@latest
```

From source:

```bash
git clone https://github.com/codejavu-llc/ghemails.git
cd ghemails
go build -o ghemails .
```

Source builds require Go 1.26.6 or newer so the networking and TLS stack includes the current security fixes.

Docker:

```bash
docker build -t ghemails .
docker run --rm -e GH_TOKEN ghemails scan -d example.com
```

Deep mode requires `git`; it is included in the published container.

## Authentication

GitHub code search requires authentication. Prefer an environment variable so the token is not exposed in process listings:

```bash
export GH_TOKEN='github_pat_...'
# GITHUB_TOKEN is also supported.
```

Alternatively, use a mode-`0600` token file:

```bash
ghemails scan -d example.com --token-file ~/.config/ghemails/github-token
```

Use the least privilege necessary. Public reconnaissance does not require repository write access. The legacy `-t` flag remains accepted but is deprecated.

## Quick start

```bash
# Balanced public scan (default)
ghemails scan -d example.com

# Existing syntax remains compatible
ghemails -d example.com -o emails.txt

# Structured evidence for pipelines
ghemails scan -d example.com --format jsonl -o report.jsonl

# Expand only an explicitly authorized repository or organization
ghemails scan -d example.com --repo owner/repository
ghemails scan -d example.com --org authorized-org --mode deep

# Include mail domains such as staff.example.com
ghemails scan -d example.com --include-subdomains

# Report only findings absent from the previous run
ghemails scan -d example.com --baseline previous.jsonl -f jsonl
```

Progress and source status go to stderr. Results go to stdout unless `-o` is supplied, so normal shell pipelines remain clean.

## Scan modes

| Mode | Behavior |
|---|---|
| `fast` | Global repository, current-code, issue/PR/comment, and commit-message searches. |
| `balanced` | Fast mode plus bounded default-branch commit metadata and evidence-linked public identity enrichment. This is the default. |
| `deep` | Balanced mode plus isolated Git clones that scan all refs, commit identities/messages, historical diffs, and deleted lines. |

Use `--sources` to choose adapters directly. `--repo` and `--org` seed authorized scopes; `--visibility accessible` opts into private/internal resources the token can read. Public-only, exact-domain matching remains the default.

## Evidence and completeness

JSON and JSONL reports use schema version `1`. Each finding includes its classification, source/evidence counts, observation range, and evidence records. Each source records one of:

- `complete`: all results available within GitHub and configured limits were processed.
- `partial`: useful evidence was retained, but a provider limit, configured cap, timeout, cancellation, or individual fetch prevented full coverage.
- `failed`: the source returned no usable evidence because of an operational error.

GitHub code and commit search cover default branches, and code search excludes files above GitHub's index limit. No tool using these APIs can honestly promise universal coverage. Deep mode addresses much of that gap for repositories first connected to the target or explicitly placed in scope.

## Exit codes

| Code | Meaning |
|---:|---|
| `0` | Successful run; partial results are allowed and labeled. |
| `1` | Operational failure with no usable source for at least one target. |
| `2` | Invalid arguments, domain, authentication, or output configuration. |
| `3` | Partial run while `--require-complete` was enabled. |
| `4` | Findings emitted while `--fail-on-findings` was enabled. |

## Privacy and responsible use

Only run ghemails against assets you own or are explicitly authorized to assess. GitHub prohibits using API data for spam, unsolicited recruiting, sale of personal information, or evasion of rate limits. Reports and caches can contain personal information; secure them, honor removal requests, and delete them when the engagement ends.

The cache defaults to the operating system's user cache directory with restrictive permissions, credential-isolated keys, and a 24-hour TTL. Disable it with `--no-cache` or remove it with `ghemails cache clear`.

Read [Responsible use](docs/responsible-use.md) before operating the tool.

## Development

```bash
make test
make lint
make build
```

See [Architecture](docs/architecture.md), [Benchmarking](docs/benchmarking.md), and [Contributing](CONTRIBUTING.md).

## License

MIT. See [LICENSE](LICENSE).
