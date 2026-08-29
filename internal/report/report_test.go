package report

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codejavu-llc/ghemails/internal/model"
)

func fixtureRun() model.Run {
	return model.Run{
		SchemaVersion: model.SchemaVersion,
		Target:        "example.com",
		Mode:          "balanced",
		Status:        "complete",
		StartedAt:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		CompletedAt:   time.Date(2025, 1, 1, 0, 1, 0, 0, time.UTC),
		Findings: []model.Finding{{
			Email: "security@example.com", Domain: "example.com", Classification: "role", SyntaxValid: true,
			SourceCount: 1, EvidenceCount: 1,
			Evidence: []model.Evidence{{Source: "github_code", URL: "https://github.com/o/r/blob/x/a#L2", Line: 2}},
		}},
		Sources: []model.SourceStatus{{Name: "code", Status: "complete", Findings: 1}},
	}
}

func TestAllFormats(t *testing.T) {
	t.Parallel()
	for format := range Formats {
		format := format
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			if err := Write(fixtureRun(), format, &output); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "security@example.com") {
				t.Fatalf("%s output omitted finding: %s", format, output.String())
			}
		})
	}
}

func TestGoldenFormats(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"txt", "jsonl"} {
		var output bytes.Buffer
		if err := Write(fixtureRun(), format, &output); err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join("testdata", "fixture."+format))
		if err != nil {
			t.Fatal(err)
		}
		if output.String() != string(want) {
			t.Fatalf("%s output changed\nwant: %s\ngot:  %s", format, want, output.String())
		}
	}
}

func TestBaselineJSONLAndText(t *testing.T) {
	t.Parallel()
	values := map[string]string{
		"baseline.txt":   "security@example.com\n",
		"baseline.jsonl": "{\"type\":\"finding\",\"email\":\"security@example.com\"}\n",
	}
	for name, content := range values {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		known, err := LoadBaseline(path)
		if err != nil {
			t.Fatal(err)
		}
		run := fixtureRun()
		ApplyBaseline(&run, known)
		if len(run.Findings) != 0 {
			t.Fatalf("%s did not suppress finding", name)
		}
	}
}
