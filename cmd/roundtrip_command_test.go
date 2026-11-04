package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCommandForRoundTrip(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := New()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func writeRoundTripInputFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("failed to write input file: %v", err)
	}
	return path
}

func assertRoundTripExitCode(t *testing.T, err error, expected int) {
	t.Helper()
	var exitCodeError *ExitCodeError
	if expected == 0 {
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		return
	}
	if !errors.As(err, &exitCodeError) {
		t.Fatalf("expected ExitCodeError, got %v", err)
	}
	if exitCodeError.Code != expected {
		t.Fatalf("expected exit code %d, got %d", expected, exitCodeError.Code)
	}
}

func TestRoundTripCommandOK(t *testing.T) {
	path := writeRoundTripInputFile(t, "a: 1\nb:\n  - one\n  - two\n")

	stdout, stderr, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "json", path)

	assertRoundTripExitCode(t, err, 0)
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if !strings.Contains(stdout, "status: ok") || !strings.Contains(stdout, "overall_status: ok") {
		t.Fatalf("unexpected report:\n%s", stdout)
	}
}

func TestRoundTripCommandSemanticDiffExitOne(t *testing.T) {
	path := writeRoundTripInputFile(t, "a: 1\n---\nb: 2\n")

	stdout, _, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "props", path)

	assertRoundTripExitCode(t, err, 1)
	if !strings.Contains(stdout, "doc_count_changed: 2 -> 1") {
		t.Fatalf("expected doc count finding in:\n%s", stdout)
	}
	if !strings.Contains(stdout, "D0 .a: type_changed: !!int -> !!str") {
		t.Fatalf("expected type change finding in:\n%s", stdout)
	}
}

func TestRoundTripCommandParseErrorExitOne(t *testing.T) {
	path := writeRoundTripInputFile(t, "a: [1\n")

	stdout, _, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "json", path)

	assertRoundTripExitCode(t, err, 1)
	if !strings.Contains(stdout, "status: parse_error") || !strings.Contains(stdout, "parse_error:") {
		t.Fatalf("expected parse_error report:\n%s", stdout)
	}
}

func TestRoundTripCommandEncodeErrorExitOne(t *testing.T) {
	path := writeRoundTripInputFile(t, "things:\n  - - 1\n    - 2\n")

	stdout, _, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "csv", path)

	assertRoundTripExitCode(t, err, 1)
	if !strings.Contains(stdout, "encode_error (encode):") {
		t.Fatalf("expected encode_error finding:\n%s", stdout)
	}
}

func TestRoundTripCommandUnknownStrategyExitTwo(t *testing.T) {
	path := writeRoundTripInputFile(t, "a: 1\n")

	_, stderr, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "json", "--compare", "bogus", path)

	assertRoundTripExitCode(t, err, 2)
	if !strings.Contains(stderr, "Error: unknown compare strategy") {
		t.Fatalf("expected usage error on stderr, got %q", stderr)
	}
}

func TestRoundTripCommandUnknownFormatExitTwo(t *testing.T) {
	path := writeRoundTripInputFile(t, "a: 1\n")

	_, stderr, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "bogus", path)

	assertRoundTripExitCode(t, err, 2)
	if !strings.Contains(stderr, "Error:") {
		t.Fatalf("expected error on stderr, got %q", stderr)
	}
}

func TestRoundTripCommandDocumentOutOfRangeExitTwo(t *testing.T) {
	path := writeRoundTripInputFile(t, "a: 1\n")

	_, stderr, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "json", "--document", "3", path)

	assertRoundTripExitCode(t, err, 2)
	if !strings.Contains(stderr, "out of range") {
		t.Fatalf("expected out of range error, got %q", stderr)
	}
}

func TestRoundTripCommandDocumentFilter(t *testing.T) {
	path := writeRoundTripInputFile(t, "a: 1\n---\na: 2\n")

	stdout, _, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "json", "--document", "0,1", path)

	assertRoundTripExitCode(t, err, 0)
	if !strings.Contains(stdout, "documents: 0,1") {
		t.Fatalf("expected document echo in report:\n%s", stdout)
	}
}

func TestRoundTripCommandPathFilter(t *testing.T) {
	path := writeRoundTripInputFile(t, "items:\n  - a: 1\n  - a: 2\n")

	stdout, _, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "json", "--path", ".items[]", path)

	assertRoundTripExitCode(t, err, 0)
	if !strings.Contains(stdout, "path: .items[]") {
		t.Fatalf("expected path echo in report:\n%s", stdout)
	}
}

func TestRoundTripCommandJSONReport(t *testing.T) {
	path := writeRoundTripInputFile(t, "a: 1\n")

	stdout, _, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "json", "--report-format", "json", path)

	assertRoundTripExitCode(t, err, 0)
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal([]byte(stdout), &parsed); jsonErr != nil {
		t.Fatalf("expected valid json report: %v\n%s", jsonErr, stdout)
	}
	if parsed["status"] != "ok" {
		t.Fatalf("unexpected status: %v", parsed["status"])
	}
}

func TestRoundTripCommandReportFile(t *testing.T) {
	input := writeRoundTripInputFile(t, "a: 1\n")
	report := filepath.Join(t.TempDir(), "report.json")

	stdout, _, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "json", "--report-format", "json", "--report-file", report, input)

	assertRoundTripExitCode(t, err, 0)
	if stdout != "" {
		t.Fatalf("expected no stdout when writing report file, got %q", stdout)
	}
	contents, readErr := os.ReadFile(report)
	if readErr != nil {
		t.Fatalf("expected report file: %v", readErr)
	}
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(contents, &parsed); jsonErr != nil {
		t.Fatalf("report file is not valid json: %v", jsonErr)
	}
}

func TestRoundTripCommandReportFileFailureExitTwo(t *testing.T) {
	input := writeRoundTripInputFile(t, "a: 1\n")
	report := filepath.Join(t.TempDir(), "missing-dir", "report.json")

	_, stderr, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "json", "--report-file", report, input)

	assertRoundTripExitCode(t, err, 2)
	if !strings.Contains(stderr, "failed to write report") {
		t.Fatalf("expected write failure error, got %q", stderr)
	}
	if _, statErr := os.Stat(report); !os.IsNotExist(statErr) {
		t.Fatalf("no partial report should exist, got %v", statErr)
	}
}

func TestRoundTripCommandKeyOrderSemanticIsInformational(t *testing.T) {
	// XML decoding lists attributes before elements, so a mapping written
	// the other way round produces a pure key order change.
	path := writeRoundTripInputFile(t, "a:\n  child: x\n  +@attr: y\n")

	stdout, _, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "xml", "--compare", "semantic", path)

	assertRoundTripExitCode(t, err, 0)
	if !strings.Contains(stdout, "key_order_changed") || !strings.Contains(stdout, "(informational)") {
		t.Fatalf("expected informational key order finding:\n%s", stdout)
	}
}

func TestRoundTripCommandKeyOrderOrderedStrategyExitOne(t *testing.T) {
	path := writeRoundTripInputFile(t, "a:\n  child: x\n  +@attr: y\n")

	stdout, _, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "xml", "--compare", "ordered", path)

	assertRoundTripExitCode(t, err, 1)
	if !strings.Contains(stdout, "key_order_changed") || strings.Contains(stdout, "(informational)") {
		t.Fatalf("expected failing key order finding without informational marker:\n%s", stdout)
	}
}

func TestRoundTripCommandUnknownFlagExitTwo(t *testing.T) {
	_, stderr, err := runCommandForRoundTrip(t, "roundtrip", "--nope")

	assertRoundTripExitCode(t, err, 2)
	if !strings.Contains(stderr, "Error: unknown flag") {
		t.Fatalf("expected flag error on stderr, got %q", stderr)
	}
	if strings.Contains(stderr, "Usage:") {
		t.Fatalf("flag errors must not print usage, got %q", stderr)
	}
}

func TestRoundTripCommandEmptyCompareExitTwo(t *testing.T) {
	path := writeRoundTripInputFile(t, "a: 1\n")

	_, stderr, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "json", "--compare", "", path)

	assertRoundTripExitCode(t, err, 2)
	if !strings.Contains(stderr, "compare strategy cannot be empty") {
		t.Fatalf("expected empty compare error, got %q", stderr)
	}
}

func TestRoundTripCommandEmptyDocumentTokenExitTwo(t *testing.T) {
	path := writeRoundTripInputFile(t, "a: 1\n")

	_, stderr, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "json", "--document", "1,", path)

	assertRoundTripExitCode(t, err, 2)
	if !strings.Contains(stderr, "invalid document index") {
		t.Fatalf("expected invalid document index error, got %q", stderr)
	}
}

func TestRoundTripCommandPathRuntimeErrorExitTwo(t *testing.T) {
	path := writeRoundTripInputFile(t, "a: 1\n")

	_, stderr, err := runCommandForRoundTrip(t, "roundtrip", "-p", "yaml", "-o", "json", "--path", `error("boom")`, path)

	assertRoundTripExitCode(t, err, 2)
	if !strings.Contains(stderr, "failed to evaluate path expression") {
		t.Fatalf("expected path evaluation error, got %q", stderr)
	}
}

func TestRoundTripCommandAlias(t *testing.T) {
	path := writeRoundTripInputFile(t, "a: 1\n")

	stdout, _, err := runCommandForRoundTrip(t, "rt", "-p", "yaml", "-o", "json", path)

	assertRoundTripExitCode(t, err, 0)
	if !strings.Contains(stdout, "status: ok") {
		t.Fatalf("alias rt should behave like roundtrip:\n%s", stdout)
	}
}

func TestRoundTripCommandDoesNotAffectEval(t *testing.T) {
	path := writeRoundTripInputFile(t, "a: 1\n")

	stdout, stderr, err := runCommandForRoundTrip(t, "eval", "-p", "yaml", ".a", path)

	assertRoundTripExitCode(t, err, 0)
	if strings.TrimSpace(stdout) != "1" || stderr != "" {
		t.Fatalf("eval behaviour changed: stdout=%q stderr=%q", stdout, stderr)
	}
}
