package yqlib

import (
	"strings"
	"testing"
)

func mustParseRoundTripSnippet(t *testing.T, input string) *CandidateNode {
	t.Helper()
	decoder := NewYamlDecoder(ConfiguredYamlPreferences)
	if err := decoder.Init(strings.NewReader(input)); err != nil {
		t.Fatalf("failed to init decoder: %v", err)
	}
	node, err := decoder.Decode()
	if err != nil {
		t.Fatalf("failed to decode snippet %q: %v", input, err)
	}
	return node
}

func findingKindsRoundTrip(findings []RoundTripFinding) []RoundTripFindingKind {
	kinds := make([]RoundTripFindingKind, 0, len(findings))
	for _, finding := range findings {
		kinds = append(kinds, finding.Kind)
	}
	return kinds
}

func findRoundTripFinding(t *testing.T, findings []RoundTripFinding, kind RoundTripFindingKind, path string) RoundTripFinding {
	t.Helper()
	for _, finding := range findings {
		if finding.Kind == kind && finding.Path == path {
			return finding
		}
	}
	t.Fatalf("could not find finding %v at %q in %v", kind, path, findings)
	return RoundTripFinding{}
}

func TestRoundTripCompareEqualTrees(t *testing.T) {
	before := mustParseRoundTripSnippet(t, "a: 1\nb:\n  - 1\n  - two\nc: true\n")
	after := mustParseRoundTripSnippet(t, "a: 1\nb:\n  - 1\n  - two\nc: true\n")

	findings := compareRoundTripTrees(before, after, 0, nil, RoundTripCompareSemantic)
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %v", findings)
	}
}

func TestRoundTripCompareValueAndTypeChanges(t *testing.T) {
	before := mustParseRoundTripSnippet(t, "a: 1\nb: hello\nc: true\n")
	after := mustParseRoundTripSnippet(t, "a: \"1\"\nb: world\nc: true\n")

	findings := compareRoundTripTrees(before, after, 2, nil, RoundTripCompareSemantic)

	typeChange := findRoundTripFinding(t, findings, RoundTripFindingTypeChanged, ".a")
	if typeChange.Before != "!!int" || typeChange.After != "!!str" {
		t.Errorf("unexpected type change %v -> %v", typeChange.Before, typeChange.After)
	}
	if typeChange.Document == nil || *typeChange.Document != 2 {
		t.Errorf("expected document index 2, got %v", typeChange.Document)
	}

	valueChange := findRoundTripFinding(t, findings, RoundTripFindingValueChanged, ".b")
	if valueChange.Before != `"hello"` || valueChange.After != `"world"` {
		t.Errorf("unexpected value change %v -> %v", valueChange.Before, valueChange.After)
	}
}

func TestRoundTripCompareMissingAndAdded(t *testing.T) {
	before := mustParseRoundTripSnippet(t, "a: 1\nb: 2\n")
	after := mustParseRoundTripSnippet(t, "b: 2\nc: 3\n")

	findings := compareRoundTripTrees(before, after, 0, nil, RoundTripCompareSemantic)

	findRoundTripFinding(t, findings, RoundTripFindingMissing, ".a")
	findRoundTripFinding(t, findings, RoundTripFindingAdded, ".c")
}

func TestRoundTripCompareKeyOrderSemanticIsInformational(t *testing.T) {
	before := mustParseRoundTripSnippet(t, "b: 1\na: 2\n")
	after := mustParseRoundTripSnippet(t, "a: 2\nb: 1\n")

	findings := compareRoundTripTrees(before, after, 0, nil, RoundTripCompareSemantic)
	if len(findings) != 1 || findings[0].Kind != RoundTripFindingKeyOrderChanged {
		t.Fatalf("expected a single key order finding, got %v", findings)
	}
	if !findings[0].Informational {
		t.Errorf("key order change must be informational under semantic strategy")
	}
	if strings.Join(findings[0].BeforeKeys, ",") != "b,a" || strings.Join(findings[0].AfterKeys, ",") != "a,b" {
		t.Errorf("unexpected key sequences: %v -> %v", findings[0].BeforeKeys, findings[0].AfterKeys)
	}
}

func TestRoundTripCompareKeyOrderOrderedIsFailure(t *testing.T) {
	before := mustParseRoundTripSnippet(t, "b: 1\na: 2\n")
	after := mustParseRoundTripSnippet(t, "a: 2\nb: 1\n")

	findings := compareRoundTripTrees(before, after, 0, nil, RoundTripCompareOrdered)
	if len(findings) != 1 || findings[0].Informational {
		t.Fatalf("expected key order finding to be a failure under ordered strategy, got %v", findings)
	}
}

func TestRoundTripCompareDifferentKeySetsNotKeyOrder(t *testing.T) {
	// Equal sized mappings with different keys (mirroring the properties
	// dotted key case) must report missing/added, never key reordering.
	before := mustParseRoundTripSnippet(t, "\"a.b\": 1\nc: 2\n")
	after := mustParseRoundTripSnippet(t, "a:\n  b: \"1\"\nc: \"2\"\n")

	findings := compareRoundTripTrees(before, after, 0, nil, RoundTripCompareOrdered)

	findRoundTripFinding(t, findings, RoundTripFindingMissing, "[\"a.b\"]")
	findRoundTripFinding(t, findings, RoundTripFindingAdded, ".a")
	for _, finding := range findings {
		if finding.Kind == RoundTripFindingKeyOrderChanged {
			t.Fatalf("different key sets must not report a key order change: %v", finding)
		}
	}
}

func TestRoundTripCompareSequenceLengthAndMismatch(t *testing.T) {
	before := mustParseRoundTripSnippet(t, "- 1\n- 2\n- 3\n")
	after := mustParseRoundTripSnippet(t, "- 1\n- 9\n")

	findings := compareRoundTripTrees(before, after, 0, nil, RoundTripCompareSemantic)

	valueChange := findRoundTripFinding(t, findings, RoundTripFindingValueChanged, "[1]")
	if valueChange.Before != "2" || valueChange.After != "9" {
		t.Errorf("unexpected sequence value change: %v", valueChange)
	}
	missing := findRoundTripFinding(t, findings, RoundTripFindingMissing, "[2]")
	if missing.Path != "[2]" {
		t.Errorf("unexpected missing path: %v", missing)
	}
}

func TestRoundTripCompareKindMismatch(t *testing.T) {
	before := mustParseRoundTripSnippet(t, "a:\n  b: 1\n")
	after := mustParseRoundTripSnippet(t, "a:\n  - 1\n")

	findings := compareRoundTripTrees(before, after, 0, nil, RoundTripCompareSemantic)
	typeChange := findRoundTripFinding(t, findings, RoundTripFindingTypeChanged, ".a")
	if typeChange.Before != "!!map" || typeChange.After != "!!seq" {
		t.Fatalf("unexpected kind change: %v -> %v", typeChange.Before, typeChange.After)
	}
	// A kind mismatch reports one finding for the subtree, no children.
	if len(findings) != 1 {
		t.Fatalf("expected exactly one finding for kind mismatch, got %v", findings)
	}
}

func TestRoundTripCompareRespectsBasePath(t *testing.T) {
	before := mustParseRoundTripSnippet(t, "a: 1\n")
	after := mustParseRoundTripSnippet(t, "a: 2\n")

	findings := compareRoundTripTrees(before, after, 1, []interface{}{"items", 0}, RoundTripCompareSemantic)
	valueChange := findRoundTripFinding(t, findings, RoundTripFindingValueChanged, ".items[0].a")
	if valueChange.Document == nil || *valueChange.Document != 1 {
		t.Fatalf("unexpected document for finding: %v", valueChange.Document)
	}
}

func TestRoundTripCompareFindingsSortedDeterministically(t *testing.T) {
	before := mustParseRoundTripSnippet(t, "z: 1\na: 1\nm:\n  z: 1\n  a: 1\n")
	after := mustParseRoundTripSnippet(t, "z: 2\na: 2\nm:\n  a: 1\n  z: 1\n")

	findings := compareRoundTripTrees(before, after, 0, nil, RoundTripCompareOrdered)

	for index := 1; index < len(findings); index++ {
		prev, next := findings[index-1], findings[index]
		if prev.Path > next.Path || (prev.Path == next.Path && string(prev.Kind) > string(next.Kind)) {
			t.Fatalf("findings not sorted: %v before %v", prev, next)
		}
	}
}
