package extract

import "testing"

func BenchmarkMatcher(b *testing.B) {
	matcher := NewMatcher("example.com", true, false)
	input := "noise x@example.com.evil security@example.com alice@staff.example.com more-noise\n"
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		_ = matcher.Find(input)
	}
}
