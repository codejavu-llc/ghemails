package model

import (
	"testing"
	"time"
)

func TestAggregatorDeduplicatesAndSortsEvidence(t *testing.T) {
	t.Parallel()
	aggregator := NewAggregator("example.com", func(string) string { return "person" })
	late := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	early := late.Add(-time.Hour)
	aggregator.Add("B@EXAMPLE.COM", Evidence{Source: "code", URL: "https://example/2", Line: 2, ObservedAt: TimeValue(late)})
	aggregator.Add("a@example.com", Evidence{Source: "commit", URL: "https://example/1", ObservedAt: TimeValue(early)})
	aggregator.Add("b@example.com", Evidence{Source: "code", URL: "https://example/2", Line: 2, ObservedAt: TimeValue(late)})
	aggregator.Add("b@example.com", Evidence{Source: "code", URL: "https://example/2", Line: 3, ObservedAt: TimeValue(early)})
	findings := aggregator.Findings()
	if len(findings) != 2 || findings[0].Email != "a@example.com" || findings[1].Email != "b@example.com" {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	if findings[1].EvidenceCount != 2 || findings[1].SourceCount != 1 {
		t.Fatalf("unexpected evidence counts: %#v", findings[1])
	}
	if findings[1].FirstSeen == nil || !findings[1].FirstSeen.Equal(early) || findings[1].LastSeen == nil || !findings[1].LastSeen.Equal(late) {
		t.Fatalf("unexpected observation range: %#v", findings[1])
	}
}
