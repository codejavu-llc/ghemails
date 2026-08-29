# Benchmark methodology

Benchmarks measure coverage, precision, reliability, and resource use—not the number of personal addresses collected from arbitrary organizations.

## Controlled corpus

Use synthetic `example.com` addresses in local Git fixtures and mocked GitHub responses covering:

- current files and README content;
- issue, pull-request, and comment fragments;
- commit messages, co-author trailers, authors, and committers;
- non-default refs, tags, deleted lines, and duplicate blobs;
- exact-domain negatives such as `user@example.com.evil`;
- pagination, incomplete results, 1,000-result partitions, rate limits, retries, timeouts, and cancellation.

## Required metrics

- Extraction recall and precision against the labeled corpus.
- Unique findings and evidence classes relative to the pre-1.0 implementation.
- Requests, cache hits, bytes, runtime, and peak memory per mode.
- Determinism across three warm-cache and three cold-cache repetitions.
- Validity and stable ordering of every report format.

The release gate is 100% recall on supported corpus cases, zero cross-domain boundary false positives, a clean race run, no token leakage, and at least all findings produced by the original implementation.

Live smoke tests may target only repositories owned for this project and must use synthetic reserved-domain data. Never benchmark by harvesting unrelated real-world personal information.
