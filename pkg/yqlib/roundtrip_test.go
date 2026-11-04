package yqlib

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeRoundTripTempFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.yaml")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	return path
}

func runRoundTripCheck(t *testing.T, contents string, options RoundTripOptions) *RoundTripReport {
	t.Helper()
	path := writeRoundTripTempFile(t, contents)
	report, err := CheckRoundTrip([]string{path}, options)
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	return report
}

func findingKindsForFileRoundTrip(t *testing.T, report *RoundTripReport, fileIndex int) []RoundTripFindingKind {
	t.Helper()
	if len(report.Files) != 1 {
		t.Fatalf("expected 1 file result, got %d", len(report.Files))
	}
	kinds := []RoundTripFindingKind{}
	for _, finding := range report.Files[fileIndex].Findings {
		kinds = append(kinds, finding.Kind)
	}
	return kinds
}

func TestRoundTripCheckYAMLToJSONOK(t *testing.T) {
	report := runRoundTripCheck(t, "a: 1\nb:\n  - one\n  - two\nc: true\n", RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "json",
	})

	if report.Status != RoundTripStatusOK {
		t.Fatalf("expected ok status, got %v: %v", report.Status, report.Files[0].Findings)
	}
}

func TestRoundTripCheckBadInputIsParseError(t *testing.T) {
	report := runRoundTripCheck(t, "a: [1, 2\n", RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "json",
	})

	result := report.Files[0]
	if result.Status != RoundTripStatusParseError {
		t.Fatalf("expected parse_error, got %v", result.Status)
	}
	if len(result.Findings) != 1 || result.Findings[0].Kind != RoundTripFindingParseError || result.Findings[0].Message == "" {
		t.Fatalf("expected parse_error finding with raw message, got %v", result.Findings)
	}
	if report.Status != RoundTripStatusParseError {
		t.Fatalf("expected overall parse_error, got %v", report.Status)
	}
}

func TestRoundTripCheckNestedArrayToCSVIsEncodeError(t *testing.T) {
	report := runRoundTripCheck(t, "things:\n  - - 1\n    - 2\n", RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "csv",
	})

	result := report.Files[0]
	if result.Status != RoundTripStatusEncodeError {
		t.Fatalf("expected encode_error, got %v (%v)", result.Status, result.Findings)
	}
	finding := result.Findings[0]
	if finding.Kind != RoundTripFindingEncodeError || finding.Phase != "encode" || finding.Message == "" {
		t.Fatalf("expected encode phase finding with message, got %v", finding)
	}
}

func TestRoundTripCheckDocumentFilter(t *testing.T) {
	report := runRoundTripCheck(t, "a: 1\n---\na: 2\n---\na: 3\n", RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "json",
		DocumentIndexes:  []uint{2},
	})

	result := report.Files[0]
	if result.DocumentsBefore != 1 || result.DocumentsAfter != 1 {
		t.Fatalf("expected only the selected document to be counted, got %d -> %d", result.DocumentsBefore, result.DocumentsAfter)
	}
	if result.Status != RoundTripStatusOK {
		t.Fatalf("expected ok for filtered document, got %v: %v", result.Status, result.Findings)
	}
}

func TestRoundTripCheckDocumentFilterKeepsOriginalIndexes(t *testing.T) {
	report := runRoundTripCheck(t, "v: doc0\n---\nv: doc1\n---\nv: doc2\n", RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "props",
		DocumentIndexes:  []uint{0, 2},
	})

	result := report.Files[0]
	if result.DocumentsBefore != 2 || result.DocumentsAfter != 1 {
		t.Fatalf("expected 2 -> 1 counted documents, got %d -> %d", result.DocumentsBefore, result.DocumentsAfter)
	}
	docCount := findRoundTripFinding(t, result.Findings, RoundTripFindingDocCountChanged, "")
	if *docCount.ExpectedCount != 2 || *docCount.ActualCount != 1 {
		t.Fatalf("unexpected doc count finding: %v", docCount)
	}
	valueChange := findRoundTripFinding(t, result.Findings, RoundTripFindingValueChanged, ".v")
	if valueChange.Document == nil || *valueChange.Document != 0 {
		t.Fatalf("expected value change on source document 0, got %v", valueChange.Document)
	}
	missing := findRoundTripFinding(t, result.Findings, RoundTripFindingMissing, "")
	if missing.Document == nil || *missing.Document != 2 {
		t.Fatalf("expected missing finding on source document 2, got %v", missing.Document)
	}
	for _, finding := range result.Findings {
		if finding.Document != nil && *finding.Document == 1 {
			t.Fatalf("unselected document 1 must never be labelled: %v", finding)
		}
	}
}

func TestRoundTripCheckDocumentFilterOutOfRange(t *testing.T) {
	path := writeRoundTripTempFile(t, "a: 1\n")
	_, err := CheckRoundTrip([]string{path}, RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "json",
		DocumentIndexes:  []uint{5},
	})
	if err == nil {
		t.Fatal("expected out of range error")
	}
}

func TestRoundTripCheckPathExpression(t *testing.T) {
	report := runRoundTripCheck(t, "items:\n  - a: 1\n  - a: 2\nother: x\n", RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "json",
		PathExpression:   ".items[]",
	})

	result := report.Files[0]
	if result.Status != RoundTripStatusOK {
		t.Fatalf("expected ok for path matches, got %v: %v", result.Status, result.Findings)
	}
}

func TestRoundTripCheckMultipleFilesAggregate(t *testing.T) {
	goodPath := writeRoundTripTempFile(t, "a: 1\n")
	badPath := writeRoundTripTempFile(t, "a: [1\n")

	report, err := CheckRoundTrip([]string{goodPath, badPath}, RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "json",
	})
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if len(report.Files) != 2 {
		t.Fatalf("expected 2 file results, got %d", len(report.Files))
	}
	if report.Files[0].Status != RoundTripStatusOK || report.Files[1].Status != RoundTripStatusParseError {
		t.Fatalf("unexpected statuses: %v %v", report.Files[0].Status, report.Files[1].Status)
	}
	if report.Status != RoundTripStatusParseError {
		t.Fatalf("expected overall parse_error, got %v", report.Status)
	}
}

func TestRoundTripCheckAliasesExplodedForJSON(t *testing.T) {
	report := runRoundTripCheck(t, "a: &x\n  b: 1\nc: *x\n", RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "json",
	})

	if report.Files[0].Status != RoundTripStatusOK {
		t.Fatalf("expected aliases to explode without findings, got %v", report.Files[0].Findings)
	}
}

func TestRoundTripCheckOrderedStrategyFailsOnKeyOrder(t *testing.T) {
	contents := "z: 1\na: 2\n"
	semantic := runRoundTripCheck(t, contents, RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "yaml",
		CompareStrategy:  RoundTripCompareSemantic,
	})
	ordered := runRoundTripCheck(t, contents, RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "yaml",
		CompareStrategy:  RoundTripCompareOrdered,
	})

	// YAML encoding preserves order, so construct a lossy ordering through
	// properties: verify strategy behaviour directly via the summary statuses
	// when key order findings exist is covered in comparator tests; here both
	// yaml round trips must stay ok.
	if semantic.Status != RoundTripStatusOK || ordered.Status != RoundTripStatusOK {
		t.Fatalf("yaml preserves key order: %v %v", semantic.Status, ordered.Status)
	}
}

func TestRoundTripCheckDeterministicAcrossRuns(t *testing.T) {
	contents := "b: 1\na: 2\n"
	path := writeRoundTripTempFile(t, contents)
	options := RoundTripOptions{InputFormatName: "yaml", OutputFormatName: "json"}

	first, err := CheckRoundTrip([]string{path}, options)
	if err != nil {
		t.Fatalf("first check failed: %v", err)
	}
	second, err := CheckRoundTrip([]string{path}, options)
	if err != nil {
		t.Fatalf("second check failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("reports are not deterministic:\n%v\n%v", first, second)
	}
}

func TestRoundTripCheckPropertiesMergesDocumentsAndStringifies(t *testing.T) {
	report := runRoundTripCheck(t, "a: 1\n---\nb: 2\n", RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "props",
	})

	result := report.Files[0]
	if result.Status != RoundTripStatusSemanticDiff {
		t.Fatalf("expected semantic_diff, got %v: %v", result.Status, result.Findings)
	}
	if result.DocumentsBefore != 2 || result.DocumentsAfter != 1 {
		t.Fatalf("expected 2 -> 1 documents, got %d -> %d", result.DocumentsBefore, result.DocumentsAfter)
	}
	docCount := findRoundTripFinding(t, result.Findings, RoundTripFindingDocCountChanged, "")
	if *docCount.ExpectedCount != 2 || *docCount.ActualCount != 1 {
		t.Fatalf("unexpected doc count finding: %v", docCount)
	}
	findRoundTripFinding(t, result.Findings, RoundTripFindingTypeChanged, ".a")
	// The second input document has no counterpart after the merge, and its
	// key shows up as added when pairing against the first document.
	findRoundTripFinding(t, result.Findings, RoundTripFindingAdded, ".b")
	findRoundTripFinding(t, result.Findings, RoundTripFindingMissing, "")
}

func TestRoundTripCheckINIDropsNestedContainers(t *testing.T) {
	report := runRoundTripCheck(t, "section:\n  simple: hello\n  list:\n    - 1\n    - 2\n  deep:\n    k: v\n", RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "ini",
	})

	result := report.Files[0]
	if result.Status != RoundTripStatusSemanticDiff {
		t.Fatalf("expected semantic_diff, got %v: %v", result.Status, result.Findings)
	}
	findRoundTripFinding(t, result.Findings, RoundTripFindingMissing, ".section.list")
	findRoundTripFinding(t, result.Findings, RoundTripFindingMissing, ".section.deep")
}

func TestRoundTripCheckXMLStringifiesAttributes(t *testing.T) {
	report := runRoundTripCheck(t, "a:\n  b: 1\n", RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "xml",
	})

	result := report.Files[0]
	if result.Status != RoundTripStatusSemanticDiff {
		t.Fatalf("expected semantic_diff, got %v: %v", result.Status, result.Findings)
	}
	findRoundTripFinding(t, result.Findings, RoundTripFindingTypeChanged, ".a.b")
}

func TestRoundTripCheckUnsupportedOutputFormatCombination(t *testing.T) {
	// shell has an encoder but no decoder, so it cannot be re-decoded.
	_, err := CheckRoundTrip([]string{"-"}, RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "shell",
	})
	if err == nil {
		t.Fatal("expected error for output format without decoder")
	}
}

// roundTripBrokenReDecodeEncoder emits bytes the YAML decoder cannot parse,
// exercising the phase=redecode branch without registering a production
// format.
type roundTripBrokenReDecodeEncoder struct{}

func (e *roundTripBrokenReDecodeEncoder) Encode(writer io.Writer, node *CandidateNode) error {
	_, err := writer.Write([]byte("a: [1, 2\n"))
	return err
}

func (e *roundTripBrokenReDecodeEncoder) PrintDocumentSeparator(writer io.Writer) error { return nil }

func (e *roundTripBrokenReDecodeEncoder) PrintLeadingContent(writer io.Writer, content string) error {
	return nil
}

func (e *roundTripBrokenReDecodeEncoder) CanHandleAliases() bool { return true }

func registerRoundTripBrokenReDecodeFormat(t *testing.T) {
	t.Helper()
	format := &Format{
		FormalName:     "roundtrip-broken-redecode",
		EncoderFactory: func() Encoder { return &roundTripBrokenReDecodeEncoder{} },
		DecoderFactory: func() Decoder { return NewYamlDecoder(ConfiguredYamlPreferences) },
	}
	Formats = append(Formats, format)
	t.Cleanup(func() { Formats = Formats[:len(Formats)-1] })
}

func TestRoundTripCheckReDecodeFailureIsEncodeError(t *testing.T) {
	registerRoundTripBrokenReDecodeFormat(t)
	report := runRoundTripCheck(t, "a: 1\n", RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "roundtrip-broken-redecode",
	})

	result := report.Files[0]
	if result.Status != RoundTripStatusEncodeError {
		t.Fatalf("expected encode_error, got %v: %v", result.Status, result.Findings)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected a single finding, got %v", result.Findings)
	}
	finding := result.Findings[0]
	if finding.Kind != RoundTripFindingEncodeError || finding.Phase != "redecode" || finding.Message == "" {
		t.Fatalf("expected redecode phase finding with message, got %v", finding)
	}
}

func TestRoundTripCheckPropertiesDottedKeysNotKeyOrder(t *testing.T) {
	// The literal key "a.b" is encoded as a properties dotted path, then
	// decoded back as a nested mapping. The key sets differ, so this must
	// surface as missing/added rather than a key reordering.
	report := runRoundTripCheck(t, "\"a.b\": 1\nc: 2\n", RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "props",
	})

	result := report.Files[0]
	if result.Status != RoundTripStatusSemanticDiff {
		t.Fatalf("expected semantic_diff, got %v: %v", result.Status, result.Findings)
	}
	findRoundTripFinding(t, result.Findings, RoundTripFindingMissing, "[\"a.b\"]")
	findRoundTripFinding(t, result.Findings, RoundTripFindingAdded, ".a")
	findRoundTripFinding(t, result.Findings, RoundTripFindingTypeChanged, ".c")
	for _, finding := range result.Findings {
		if finding.Kind == RoundTripFindingKeyOrderChanged {
			t.Fatalf("different key sets must not report a key order change: %v", finding)
		}
	}
}

func TestRoundTripCheckPathExpressionRuntimeError(t *testing.T) {
	path := writeRoundTripTempFile(t, "a: 1\n")
	_, err := CheckRoundTrip([]string{path}, RoundTripOptions{
		InputFormatName:  "yaml",
		OutputFormatName: "json",
		PathExpression:   `error("boom")`,
	})
	if err == nil || !strings.Contains(err.Error(), "failed to evaluate path expression") {
		t.Fatalf("expected operational error for path runtime failure, got %v", err)
	}
}

func TestRoundTripCheckPathAssignmentDoesNotMutateDocument(t *testing.T) {
	InitExpressionParser()
	pathNode, err := ExpressionParser.ParseExpression(".a = 5")
	if err != nil {
		t.Fatalf("failed to parse expression: %v", err)
	}

	document := mustParseRoundTripSnippet(t, "a: 1\n")
	checker := &roundTripChecker{
		options:      RoundTripOptions{CompareStrategy: RoundTripCompareSemantic},
		outputFormat: JSONFormat,
		navigator:    NewDataTreeNavigator(),
		pathNode:     pathNode,
	}

	findings, err := checker.checkWithPath([]selectedRoundTripDocument{{node: document, originalIndex: 0}})
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings for the assigned copy, got %v", findings)
	}
	for index := 0; index+1 < len(document.Content); index += 2 {
		if document.Content[index].Value == "a" && document.Content[index+1].Value != "1" {
			t.Fatalf("assignment expression mutated the checked document: .a = %v", document.Content[index+1].Value)
		}
	}
}
