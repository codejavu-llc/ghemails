# Contributing

Contributions that improve authorized security research, precision, provenance, or operator safety are welcome.

## Workflow

1. Open an issue for behavior changes or new discovery sources.
2. Create a focused branch and use synthetic data under reserved domains in tests.
3. Run `make test lint` before submitting a pull request.
4. Document new flags, evidence fields, provider limits, and privacy implications.

Pull requests must preserve deterministic output, stdout/stderr separation, restrictive data-file permissions, token redaction, and honest partial-source reporting.

## Adding a source

- Add the provider response types to the GitHub API package when necessary.
- Implement the source in the scanner and register it in the ordered source list and configuration allowlist.
- Emit evidence with a stable source name and the most reproducible URL/location available.
- Bound pagination, response bodies, workers, and retries. Treat provider caps or incomplete results as partial.
- Add `httptest` coverage for success, pagination, malformed data, throttling, cancellation, and partial/failure behavior.
- Update the source list and scan-mode documentation.

Do not add token rotation, authentication bypasses, mailbox guessing, unsolicited-contact features, or sources whose terms prohibit this use.
