package extract

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeDomain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
		err   bool
	}{
		{"Example.COM.", "example.com", false},
		{"bücher.de", "xn--bcher-kva.de", false},
		{"https://example.com", "", true},
		{"person@example.com", "", true},
		{"localhost", "", true},
		{"-bad.example", "", true},
		{"bad_.example", "", true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeDomain(test.input)
			if (err != nil) != test.err || got != test.want {
				t.Fatalf("NormalizeDomain(%q) = %q, %v; want %q, err=%v", test.input, got, err, test.want, test.err)
			}
		})
	}
}

func TestMatcherExactBoundariesAndDeduplication(t *testing.T) {
	t.Parallel()
	matcher := NewMatcher("example.com", false, false)
	text := "Alice <Alice@Example.com>, evil@Example.com.evil, bad..dots@example.com, .bad@example.com, alice@example.com, ops@sub.example.com"
	want := []string{"alice@example.com"}
	if got := matcher.Find(text); !reflect.DeepEqual(got, want) {
		t.Fatalf("Find() = %#v; want %#v", got, want)
	}
}

func TestMatcherSubdomainsAndNoreply(t *testing.T) {
	t.Parallel()
	matcher := NewMatcher("github.com", true, false)
	want := []string{"security@github.com", "ops@corp.github.com"}
	text := "security@github.com ops@corp.github.com 123+bot@users.noreply.github.com"
	if got := matcher.Find(text); !reflect.DeepEqual(got, want) {
		t.Fatalf("Find() = %#v; want %#v", got, want)
	}
	withNoreply := NewMatcher("github.com", true, true)
	if got := withNoreply.Find(text); len(got) != 3 {
		t.Fatalf("Find() with noreply returned %d values; want 3", len(got))
	}
}

func TestClassification(t *testing.T) {
	t.Parallel()
	if got := Classification("security+triage@example.com"); got != "role" {
		t.Fatalf("Classification() = %q; want role", got)
	}
	if got := Classification("alice@example.com"); got != "person" {
		t.Fatalf("Classification() = %q; want person", got)
	}
}

func FuzzMatcherNeverCrossesDomainBoundary(f *testing.F) {
	f.Add("alice", ".evil")
	f.Add("security", "")
	f.Fuzz(func(t *testing.T, local, suffix string) {
		matcher := NewMatcher("example.com", false, false)
		for _, email := range matcher.Find(local + "@example.com" + suffix) {
			if !strings.HasSuffix(email, "@example.com") {
				t.Fatalf("cross-domain match: %q", email)
			}
		}
	})
}
