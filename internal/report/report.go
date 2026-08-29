package report

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/codejavu-llc/ghemails/internal/model"
)

var Formats = map[string]struct{}{"txt": {}, "jsonl": {}, "json": {}, "csv": {}, "markdown": {}, "sarif": {}}

func Write(run model.Run, format string, writer io.Writer) error {
	switch format {
	case "txt":
		for _, finding := range run.Findings {
			if _, err := fmt.Fprintln(writer, finding.Email); err != nil {
				return err
			}
		}
		return nil
	case "json":
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(run)
	case "jsonl":
		encoder := json.NewEncoder(writer)
		summary := struct {
			Type          string               `json:"type"`
			SchemaVersion string               `json:"schema_version"`
			Target        string               `json:"target"`
			Mode          string               `json:"mode"`
			Status        string               `json:"status"`
			StartedAt     time.Time            `json:"started_at"`
			CompletedAt   time.Time            `json:"completed_at"`
			FindingCount  int                  `json:"finding_count"`
			Sources       []model.SourceStatus `json:"sources"`
		}{"summary", run.SchemaVersion, run.Target, run.Mode, run.Status, run.StartedAt, run.CompletedAt, len(run.Findings), run.Sources}
		if err := encoder.Encode(summary); err != nil {
			return err
		}
		for _, finding := range run.Findings {
			line := struct {
				Type string `json:"type"`
				model.Finding
			}{Type: "finding", Finding: finding}
			if err := encoder.Encode(line); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		return writeCSV(run, writer)
	case "markdown":
		return writeMarkdown(run, writer)
	case "sarif":
		return writeSARIF(run, writer)
	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
}

func writeCSV(run model.Run, writer io.Writer) error {
	out := csv.NewWriter(writer)
	if err := out.Write([]string{"email", "classification", "source_count", "evidence_count", "first_seen", "last_seen", "sources", "evidence_urls"}); err != nil {
		return err
	}
	for _, finding := range run.Findings {
		sources, urls := findingLists(finding)
		first, last := "", ""
		if finding.FirstSeen != nil {
			first = finding.FirstSeen.Format(time.RFC3339)
		}
		if finding.LastSeen != nil {
			last = finding.LastSeen.Format(time.RFC3339)
		}
		if err := out.Write([]string{finding.Email, finding.Classification, fmt.Sprint(finding.SourceCount), fmt.Sprint(finding.EvidenceCount), first, last, strings.Join(sources, ";"), strings.Join(urls, ";")}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

func writeMarkdown(run model.Run, writer io.Writer) error {
	if _, err := fmt.Fprintf(writer, "# ghemails report: %s\n\n- Status: `%s`\n- Mode: `%s`\n- Findings: %d\n- Completed: %s\n\n", run.Target, run.Status, run.Mode, len(run.Findings), run.CompletedAt.Format(time.RFC3339)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "| Email | Type | Sources | Evidence |\n|---|---:|---:|---:|"); err != nil {
		return err
	}
	for _, finding := range run.Findings {
		if _, err := fmt.Fprintf(writer, "| `%s` | %s | %d | %d |\n", finding.Email, finding.Classification, finding.SourceCount, finding.EvidenceCount); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(writer, "\n## Source status\n\n| Source | Status | Findings | Reason |\n|---|---|---:|---|"); err != nil {
		return err
	}
	for _, source := range run.Sources {
		if _, err := fmt.Fprintf(writer, "| %s | %s | %d | %s |\n", source.Name, source.Status, source.Findings, source.Reason); err != nil {
			return err
		}
	}
	return nil
}

type sarifDocument struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    any           `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifResult struct {
	RuleID    string `json:"ruleId"`
	Level     string `json:"level"`
	Message   any    `json:"message"`
	Locations []any  `json:"locations,omitempty"`
}

func writeSARIF(run model.Run, writer io.Writer) error {
	results := make([]sarifResult, 0, len(run.Findings))
	for _, finding := range run.Findings {
		result := sarifResult{RuleID: "ghemails/observed-email", Level: "note", Message: map[string]string{"text": "Observed target-domain email: " + finding.Email}}
		if len(finding.Evidence) > 0 && finding.Evidence[0].URL != "" {
			physical := map[string]any{"artifactLocation": map[string]string{"uri": finding.Evidence[0].URL}}
			if finding.Evidence[0].Line > 0 {
				physical["region"] = map[string]int{"startLine": finding.Evidence[0].Line}
			}
			result.Locations = []any{map[string]any{"physicalLocation": physical}}
		}
		results = append(results, result)
	}
	doc := sarifDocument{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs: []sarifRun{{
			Tool:    map[string]any{"driver": map[string]any{"name": "ghemails", "informationUri": "https://github.com/codejavu-llc/ghemails", "rules": []any{map[string]any{"id": "ghemails/observed-email", "name": "Observed target-domain email"}}}},
			Results: results,
		}},
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(doc)
}

func findingLists(finding model.Finding) ([]string, []string) {
	sourceSet, urlSet := make(map[string]struct{}), make(map[string]struct{})
	for _, evidence := range finding.Evidence {
		sourceSet[evidence.Source] = struct{}{}
		if evidence.URL != "" {
			urlSet[evidence.URL] = struct{}{}
		}
	}
	sources, urls := keys(sourceSet), keys(urlSet)
	return sources, urls
}

func keys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func LoadBaseline(path string) (map[string]struct{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{})
	var run model.Run
	if json.Unmarshal(data, &run) == nil && run.SchemaVersion != "" {
		for _, finding := range run.Findings {
			known[strings.ToLower(finding.Email)] = struct{}{}
		}
		return known, nil
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var finding struct {
			Email string `json:"email"`
		}
		if json.Unmarshal([]byte(line), &finding) == nil && finding.Email != "" {
			known[strings.ToLower(finding.Email)] = struct{}{}
			continue
		}
		candidate := strings.Trim(strings.SplitN(line, ",", 2)[0], " \t\"`")
		if strings.Contains(candidate, "@") {
			known[strings.ToLower(candidate)] = struct{}{}
		}
	}
	return known, scanner.Err()
}

func ApplyBaseline(run *model.Run, known map[string]struct{}) {
	if len(known) == 0 {
		return
	}
	filtered := run.Findings[:0]
	for _, finding := range run.Findings {
		if _, exists := known[strings.ToLower(finding.Email)]; !exists {
			filtered = append(filtered, finding)
		}
	}
	run.Findings = filtered
}
