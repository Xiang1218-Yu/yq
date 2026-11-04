package yqlib

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleRoundTripReport() *RoundTripReport {
	docOne := uint(1)
	docZero := uint(0)
	expected := 2
	actual := 1
	return &RoundTripReport{
		Parameters: RoundTripParameters{
			InputFormat:  "yaml",
			OutputFormat: "json",
			Compare:      RoundTripCompareSemantic,
			Path:         ".items[]",
			Documents:    []uint{0, 2},
		},
		Status: RoundTripStatusSemanticDiff,
		Files: []RoundTripFileResult{
			{
				Filename:        "example.yaml",
				Status:          RoundTripStatusSemanticDiff,
				DocumentsBefore: 2,
				DocumentsAfter:  1,
				Findings: []RoundTripFinding{
					{
						Kind:          RoundTripFindingDocCountChanged,
						ExpectedCount: &expected,
						ActualCount:   &actual,
					},
					{
						Kind:     RoundTripFindingTypeChanged,
						Document: &docZero,
						Path:     ".items[0].a",
						Before:   "!!int",
						After:    "!!str",
					},
					{
						Kind:     RoundTripFindingValueChanged,
						Document: &docOne,
						Path:     ".items[1].b",
						Before:   `"hello"`,
						After:    `"world"`,
					},
					{
						Kind:          RoundTripFindingKeyOrderChanged,
						Document:      &docZero,
						Path:          ".items[0]",
						BeforeKeys:    []string{"b", "a"},
						AfterKeys:     []string{"a", "b"},
						Informational: true,
					},
				},
			},
		},
		Summary: RoundTripSummary{
			Files: 1,
			StatusCounts: map[string]int{
				"semantic_diff": 1,
			},
			FindingCounts: map[string]int{
				"doc_count_changed": 1,
				"key_order_changed": 1,
				"type_changed":      1,
				"value_changed":     1,
			},
		},
	}
}

func TestRenderRoundTripTextReportGolden(t *testing.T) {
	expected := `round-trip check report
input_format: yaml
output_format: json
compare: semantic
path: .items[]
documents: 0,2
file: example.yaml
  status: semantic_diff
  documents: 2 -> 1
  doc_count_changed: 2 -> 1
  D0 .items[0]: key_order_changed: ["b", "a"] -> ["a", "b"] (informational)
  D0 .items[0].a: type_changed: !!int -> !!str
  D1 .items[1].b: value_changed: "hello" -> "world"
files: ok=0; semantic_diff=1; encode_error=0; parse_error=0
findings: parse_error=0; encode_error=0; doc_count_changed=1; type_changed=1; value_changed=1; missing=0; added=0; key_order_changed=1
overall_status: semantic_diff
`

	actual := renderRoundTripTextReport(sampleRoundTripReport())
	if actual != expected {
		t.Fatalf("text report mismatch:\nexpected:\n%s\nactual:\n%s", expected, actual)
	}
}

func TestRenderRoundTripTextReportParseErrorLocation(t *testing.T) {
	docZero := uint(0)
	report := &RoundTripReport{
		Parameters: RoundTripParameters{InputFormat: "yaml", OutputFormat: "json", Compare: RoundTripCompareSemantic},
		Status:     RoundTripStatusParseError,
		Files: []RoundTripFileResult{
			{
				Filename: "bad.yaml",
				Status:   RoundTripStatusParseError,
				Findings: []RoundTripFinding{
					{Kind: RoundTripFindingParseError, Document: &docZero, Message: "bad yaml"},
				},
			},
		},
		Summary: RoundTripSummary{Files: 1, StatusCounts: map[string]int{"parse_error": 1}, FindingCounts: map[string]int{"parse_error": 1}},
	}

	text := renderRoundTripTextReport(report)
	if !strings.Contains(text, "  D0 .: parse_error: bad yaml\n") {
		t.Fatalf("unexpected parse error line:\n%s", text)
	}
}

func TestRenderRoundTripJSONReportIsValidAndDeterministic(t *testing.T) {
	first, err := RenderRoundTripReport(sampleRoundTripReport(), RoundTripReportFormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderRoundTripReport(sampleRoundTripReport(), RoundTripReportFormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("json rendering is not deterministic")
	}
	if first[len(first)-1] != '\n' {
		t.Fatal("json report must end with a newline")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(first, &parsed); err != nil {
		t.Fatalf("json report is not valid json: %v", err)
	}
	for _, key := range []string{"parameters", "status", "files", "summary"} {
		if _, ok := parsed[key]; !ok {
			t.Fatalf("json report missing key %q", key)
		}
	}
}

func TestRenderRoundTripReportRejectsUnknownFormat(t *testing.T) {
	if _, err := RenderRoundTripReport(sampleRoundTripReport(), "toml"); err == nil {
		t.Fatal("expected error for unknown report format")
	}
}

func TestWriteRoundTripReportToStdout(t *testing.T) {
	var buffer bytes.Buffer
	if err := WriteRoundTripReport(sampleRoundTripReport(), RoundTripReportFormatJSON, "", &buffer); err != nil {
		t.Fatal(err)
	}
	if buffer.Len() == 0 {
		t.Fatal("expected report on stdout writer")
	}
}

func TestWriteRoundTripReportAtomically(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "nested", "report.json")

	// Destination directory does not exist: no file or partial temp files.
	err := WriteRoundTripReport(sampleRoundTripReport(), RoundTripReportFormatJSON, destination, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected write to fail when parent directory is missing")
	}
	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("destination must not exist after failure, got %v", statErr)
	}
	if entries, _ := os.ReadDir(filepath.Dir(destination)); len(entries) != 0 {
		t.Fatalf("missing parent directory must not be created, got %v", entries)
	}
	rootDirectory := filepath.Dir(filepath.Dir(destination))
	if entries, _ := os.ReadDir(rootDirectory); len(entries) != 0 {
		t.Fatalf("expected no residual temporary files in %v, got %v", rootDirectory, entries)
	}

	// Successful write replaces an existing file atomically.
	existing := filepath.Join(t.TempDir(), "report.json")
	original := []byte("previous content\n")
	if err := os.WriteFile(existing, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteRoundTripReport(sampleRoundTripReport(), RoundTripReportFormatJSON, existing, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(written, original) || !bytes.HasSuffix(written, []byte("\n")) {
		t.Fatal("destination should contain the new report with trailing newline")
	}
	if entries, _ := os.ReadDir(filepath.Dir(existing)); len(entries) != 1 {
		t.Fatalf("expected no residual temporary files, got %v", entries)
	}
}
