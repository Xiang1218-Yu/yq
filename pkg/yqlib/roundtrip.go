package yqlib

import (
	"container/list"
	"sort"
)

// RoundTripCompareStrategy selects how strictly re-decoded node trees are
// compared against the originals.
type RoundTripCompareStrategy string

const (
	// RoundTripCompareSemantic compares values and types but ignores mapping
	// key order (key order findings are reported as informational only).
	RoundTripCompareSemantic RoundTripCompareStrategy = "semantic"
	// RoundTripCompareOrdered treats mapping key order changes as failures.
	RoundTripCompareOrdered RoundTripCompareStrategy = "ordered"
)

// RoundTripStatus is the outcome for a single input file or the whole run.
type RoundTripStatus string

const (
	RoundTripStatusOK           RoundTripStatus = "ok"
	RoundTripStatusSemanticDiff RoundTripStatus = "semantic_diff"
	RoundTripStatusEncodeError  RoundTripStatus = "encode_error"
	RoundTripStatusParseError   RoundTripStatus = "parse_error"
)

// RoundTripFindingKind categorises a single finding.
type RoundTripFindingKind string

const (
	RoundTripFindingParseError      RoundTripFindingKind = "parse_error"
	RoundTripFindingEncodeError     RoundTripFindingKind = "encode_error"
	RoundTripFindingDocCountChanged RoundTripFindingKind = "doc_count_changed"
	RoundTripFindingTypeChanged     RoundTripFindingKind = "type_changed"
	RoundTripFindingValueChanged    RoundTripFindingKind = "value_changed"
	RoundTripFindingMissing         RoundTripFindingKind = "missing"
	RoundTripFindingAdded           RoundTripFindingKind = "added"
	RoundTripFindingKeyOrderChanged RoundTripFindingKind = "key_order_changed"
)

// RoundTripFinding is a single path- or file-level check result.
type RoundTripFinding struct {
	Kind          RoundTripFindingKind `json:"kind"`
	Document      *uint                `json:"document,omitempty"`
	Path          string               `json:"path,omitempty"`
	Informational bool                 `json:"informational,omitempty"`
	// Phase is set for encode_error findings: "encode" (the output format
	// cannot represent the node) or "redecode" (the encoded output could not
	// be parsed by the output format decoder).
	Phase string `json:"phase,omitempty"`
	// Message carries the raw encoder/decoder error for parse/encode errors.
	Message string `json:"message,omitempty"`
	// Before and After carry deterministic scalar or type renderings for
	// value/type changes.
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	// BeforeKeys and AfterKeys carry the mapping key sequences for a key
	// order change.
	BeforeKeys []string `json:"before_keys,omitempty"`
	AfterKeys  []string `json:"after_keys,omitempty"`
	// ExpectedCount and ActualCount are used by doc_count_changed findings.
	ExpectedCount *int `json:"expected_count,omitempty"`
	ActualCount   *int `json:"actual_count,omitempty"`
}

// RoundTripFileResult is the per-file outcome.
type RoundTripFileResult struct {
	Filename        string             `json:"filename"`
	Status          RoundTripStatus    `json:"status"`
	DocumentsBefore int                `json:"documents_before"`
	DocumentsAfter  int                `json:"documents_after"`
	Findings        []RoundTripFinding `json:"findings"`
}

// RoundTripParameters echoes the options the check ran with.
type RoundTripParameters struct {
	InputFormat  string                   `json:"input_format"`
	OutputFormat string                   `json:"output_format"`
	Compare      RoundTripCompareStrategy `json:"compare"`
	Path         string                   `json:"path,omitempty"`
	Documents    []uint                   `json:"documents,omitempty"`
}

// RoundTripSummary contains aggregate counters across all files.
type RoundTripSummary struct {
	Files         int            `json:"files"`
	StatusCounts  map[string]int `json:"status_counts"`
	FindingCounts map[string]int `json:"finding_counts"`
}

// RoundTripReport is the complete deterministic check result.
type RoundTripReport struct {
	Parameters RoundTripParameters   `json:"parameters"`
	Status     RoundTripStatus       `json:"status"`
	Files      []RoundTripFileResult `json:"files"`
	Summary    RoundTripSummary      `json:"summary"`
}

// RoundTripOptions configures an offline decode -> encode -> decode check.
type RoundTripOptions struct {
	InputFormatName  string
	OutputFormatName string
	CompareStrategy  RoundTripCompareStrategy
	// DocumentIndexes filters the input documents to check (0 based); nil
	// means all documents.
	DocumentIndexes []uint
	// PathExpression is an optional yq expression selecting subtrees to
	// check. Empty means whole-document mode.
	PathExpression string
}

// roundTripStatusRank orders statuses by severity for aggregation.
func roundTripStatusRank(status RoundTripStatus) int {
	switch status {
	case RoundTripStatusParseError:
		return 3
	case RoundTripStatusEncodeError:
		return 2
	case RoundTripStatusSemanticDiff:
		return 1
	default:
		return 0
	}
}

// worstRoundTripStatus returns the more severe of two statuses.
func worstRoundTripStatus(current RoundTripStatus, candidate RoundTripStatus) RoundTripStatus {
	if roundTripStatusRank(candidate) > roundTripStatusRank(current) {
		return candidate
	}
	return current
}

// fileStatusFromFindings derives a file status from the findings collected for
// it.
func fileStatusFromFindings(findings []RoundTripFinding) RoundTripStatus {
	status := RoundTripStatusOK
	for _, finding := range findings {
		switch finding.Kind {
		case RoundTripFindingParseError:
			return RoundTripStatusParseError
		case RoundTripFindingEncodeError:
			status = worstRoundTripStatus(status, RoundTripStatusEncodeError)
		default:
			if !finding.Informational {
				status = worstRoundTripStatus(status, RoundTripStatusSemanticDiff)
			}
		}
	}
	return status
}

// sortFindings orders findings deterministically by document, path and kind.
// Parse/encode errors keep their document/path association when present.
func sortFindings(findings []RoundTripFinding) {
	docRank := func(finding RoundTripFinding) int {
		if finding.Document == nil {
			return -1
		}
		return int(*finding.Document)
	}
	sort.SliceStable(findings, func(i, j int) bool {
		di, dj := docRank(findings[i]), docRank(findings[j])
		if di != dj {
			return di < dj
		}
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return string(findings[i].Kind) < string(findings[j].Kind)
	})
}

// roundTripExplode applies the same explode operation the printer uses when an
// encoder cannot handle aliases, so that merged/aliased nodes are compared by
// their semantic value graph.
func roundTripExplode(node *CandidateNode) (*CandidateNode, error) {
	inputList := list.New()
	inputList.PushBack(node)

	explodeOp := Operation{OperationType: explodeOpType}
	explodeExpressionNode := ExpressionNode{Operation: &explodeOp}

	navigator := NewDataTreeNavigator()
	context, err := navigator.GetMatchingNodes(Context{MatchingNodes: inputList}, &explodeExpressionNode)
	if err != nil {
		return nil, err
	}
	return context.MatchingNodes.Front().Value.(*CandidateNode), nil
}
