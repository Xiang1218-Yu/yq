package yqlib

import (
	"bufio"
	"bytes"
	"container/list"
	"errors"
	"fmt"
	"io"
	"sort"
)

// CheckRoundTrip runs the offline decode -> encode -> decode check for the
// given files ("-" denotes stdin). Check outcomes are returned in the report;
// operational problems (unknown formats, unreadable files, bad expressions,
// out of range document indexes) are returned as errors.
func CheckRoundTrip(filenames []string, options RoundTripOptions) (*RoundTripReport, error) {
	if options.CompareStrategy == "" {
		options.CompareStrategy = RoundTripCompareSemantic
	}
	if options.CompareStrategy != RoundTripCompareSemantic && options.CompareStrategy != RoundTripCompareOrdered {
		return nil, fmt.Errorf("unknown compare strategy %q, please use semantic or ordered", options.CompareStrategy)
	}

	inputFormat, err := FormatFromString(options.InputFormatName)
	if err != nil {
		return nil, err
	}
	outputFormat, err := FormatFromString(options.OutputFormatName)
	if err != nil {
		return nil, err
	}
	if inputFormat.DecoderFactory == nil {
		return nil, fmt.Errorf("no support for %s input format", options.InputFormatName)
	}
	if outputFormat.EncoderFactory == nil || outputFormat.DecoderFactory == nil {
		return nil, fmt.Errorf("round-trip check requires both an encoder and a decoder for %s output format", options.OutputFormatName)
	}

	checker := &roundTripChecker{
		options:      options,
		inputFormat:  inputFormat,
		outputFormat: outputFormat,
		navigator:    NewDataTreeNavigator(),
		pathNode:     nil,
	}

	if options.PathExpression != "" {
		InitExpressionParser()
		checker.pathNode, err = ExpressionParser.ParseExpression(options.PathExpression)
		if err != nil {
			return nil, fmt.Errorf("invalid path expression: %w", err)
		}
	}

	report := &RoundTripReport{
		Parameters: RoundTripParameters{
			InputFormat:  inputFormat.FormalName,
			OutputFormat: outputFormat.FormalName,
			Compare:      options.CompareStrategy,
			Path:         options.PathExpression,
			Documents:    normaliseRoundTripDocumentIndexes(options.DocumentIndexes),
		},
		Status: RoundTripStatusOK,
		Files:  []RoundTripFileResult{},
	}

	for _, filename := range filenames {
		result, checkError := checker.checkFile(filename)
		if checkError != nil {
			return nil, checkError
		}
		report.Files = append(report.Files, result)
		report.Status = worstRoundTripStatus(report.Status, result.Status)
	}

	report.Summary = buildRoundTripSummary(report)
	return report, nil
}

type roundTripChecker struct {
	options      RoundTripOptions
	inputFormat  *Format
	outputFormat *Format
	navigator    DataTreeNavigator
	pathNode     *ExpressionNode
}

func (c *roundTripChecker) checkFile(filename string) (RoundTripFileResult, error) {
	displayName := resolveFilename(filename)
	result := RoundTripFileResult{
		Filename: displayName,
		Status:   RoundTripStatusOK,
		Findings: []RoundTripFinding{},
	}

	reader, cleanup, err := readStream(filename)
	if err != nil {
		return result, err
	}
	defer cleanup()

	documents, parseFinding := c.decodeDocuments(reader)
	if parseFinding != nil {
		result.DocumentsBefore = len(documents)
		result.DocumentsAfter = 0
		result.Findings = append(result.Findings, *parseFinding)
		result.Status = fileStatusFromFindings(result.Findings)
		sortFindings(result.Findings)
		return result, nil
	}

	selected, err := selectRoundTripDocuments(documents, c.options.DocumentIndexes)
	if err != nil {
		return result, err
	}

	result.DocumentsBefore = len(selected)
	result.DocumentsAfter = len(selected)
	if c.pathNode != nil {
		findings, checkError := c.checkWithPath(selected)
		if checkError != nil {
			return result, checkError
		}
		result.Findings = append(result.Findings, findings...)
	} else {
		findings, afterCount := c.checkWholeDocuments(selected)
		result.DocumentsAfter = afterCount
		result.Findings = append(result.Findings, findings...)
	}

	sortFindings(result.Findings)
	result.Status = fileStatusFromFindings(result.Findings)
	return result, nil
}

// decodeDocuments decodes every document of an input stream. A decode error
// becomes a parse_error finding carrying the 0 based index of the document
// that failed.
func (c *roundTripChecker) decodeDocuments(reader io.Reader) ([]*CandidateNode, *RoundTripFinding) {
	decoder := c.inputFormat.DecoderFactory()
	if err := decoder.Init(reader); err != nil {
		return nil, &RoundTripFinding{
			Kind:    RoundTripFindingParseError,
			Path:    "",
			Message: err.Error(),
		}
	}

	documents := []*CandidateNode{}
	for {
		node, err := decoder.Decode()
		if errors.Is(err, io.EOF) {
			return documents, nil
		}
		if err != nil {
			index := uint(len(documents))
			return documents, &RoundTripFinding{
				Kind:     RoundTripFindingParseError,
				Document: &index,
				Message:  err.Error(),
			}
		}
		documents = append(documents, node)
	}
}

// selectedRoundTripDocument pairs a checked document with its 0 based index
// in the original input stream, so that findings keep reporting source
// document indexes after --document filtering.
type selectedRoundTripDocument struct {
	node          *CandidateNode
	originalIndex uint
}

func selectRoundTripDocuments(documents []*CandidateNode, indexes []uint) ([]selectedRoundTripDocument, error) {
	if len(indexes) == 0 {
		selected := make([]selectedRoundTripDocument, 0, len(documents))
		for index, document := range documents {
			selected = append(selected, selectedRoundTripDocument{node: document, originalIndex: uint(index)})
		}
		return selected, nil
	}
	selected := make([]selectedRoundTripDocument, 0, len(indexes))
	for _, index := range normaliseRoundTripDocumentIndexes(indexes) {
		if int(index) >= len(documents) {
			return nil, fmt.Errorf("document index %d is out of range, the input contains %d document(s)", index, len(documents))
		}
		selected = append(selected, selectedRoundTripDocument{node: documents[index], originalIndex: index})
	}
	return selected, nil
}

func normaliseRoundTripDocumentIndexes(indexes []uint) []uint {
	if len(indexes) == 0 {
		return nil
	}
	unique := map[uint]bool{}
	for _, index := range indexes {
		unique[index] = true
	}
	result := make([]uint, 0, len(unique))
	for index := range unique {
		result = append(result, index)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

// checkWholeDocuments encodes the selected documents in a single stream
// (mirroring normal multi-document printing) and re-decodes it.
func (c *roundTripChecker) checkWholeDocuments(documents []selectedRoundTripDocument) ([]RoundTripFinding, int) {
	findings := []RoundTripFinding{}

	encoder := c.outputFormat.EncoderFactory()
	buffer := bytes.NewBuffer(nil)
	// Mirror the production printer, which always encodes through a buffered
	// writer and flushes it after each document: some encoders (e.g. XML)
	// retain tokens until the surrounding writer is flushed.
	bufferedWriter := bufio.NewWriter(buffer)
	for index, document := range documents {
		node := document.node
		if !encoder.CanHandleAliases() {
			exploded, err := roundTripExplode(document.node)
			if err != nil {
				docIndex := document.originalIndex
				findings = append(findings, RoundTripFinding{
					Kind:     RoundTripFindingEncodeError,
					Document: &docIndex,
					Phase:    "encode",
					Message:  err.Error(),
				})
				return findings, 0
			}
			node = exploded
		}
		if index > 0 {
			if err := encoder.PrintDocumentSeparator(bufferedWriter); err != nil {
				docIndex := document.originalIndex
				findings = append(findings, RoundTripFinding{
					Kind:     RoundTripFindingEncodeError,
					Document: &docIndex,
					Phase:    "encode",
					Message:  err.Error(),
				})
				return findings, 0
			}
		}
		if err := encoder.Encode(bufferedWriter, node); err != nil {
			docIndex := document.originalIndex
			findings = append(findings, RoundTripFinding{
				Kind:     RoundTripFindingEncodeError,
				Document: &docIndex,
				Phase:    "encode",
				Message:  err.Error(),
			})
			return findings, 0
		}
	}
	if err := bufferedWriter.Flush(); err != nil {
		findings = append(findings, RoundTripFinding{
			Kind:    RoundTripFindingEncodeError,
			Phase:   "encode",
			Message: err.Error(),
		})
		return findings, 0
	}

	afterDocuments, err := c.redecode(buffer)
	if err != nil {
		findings = append(findings, RoundTripFinding{
			Kind:    RoundTripFindingEncodeError,
			Phase:   "redecode",
			Message: err.Error(),
		})
		return findings, 0
	}

	expectedCount := len(documents)
	actualCount := len(afterDocuments)
	if expectedCount != actualCount {
		expected := expectedCount
		actual := actualCount
		findings = append(findings, RoundTripFinding{
			Kind:          RoundTripFindingDocCountChanged,
			ExpectedCount: &expected,
			ActualCount:   &actual,
		})
	}

	commonCount := expectedCount
	if actualCount < commonCount {
		commonCount = actualCount
	}
	for index := 0; index < commonCount; index++ {
		findings = append(findings, compareRoundTripTrees(documents[index].node, afterDocuments[index], documents[index].originalIndex, nil, c.options.CompareStrategy)...)
	}
	for index := commonCount; index < expectedCount; index++ {
		docIndex := documents[index].originalIndex
		findings = append(findings, RoundTripFinding{
			Kind:     RoundTripFindingMissing,
			Document: &docIndex,
			Path:     "",
		})
	}
	// Extra re-decoded documents have no source document counterpart, so
	// they carry no document index.
	for index := commonCount; index < actualCount; index++ {
		findings = append(findings, RoundTripFinding{
			Kind: RoundTripFindingAdded,
			Path: "",
		})
	}

	return findings, actualCount
}

// checkWithPath evaluates the path expression per document and round-trips
// each matched subtree independently. The expression is evaluated against a
// detached deep copy with auto-creation disabled, so assignment style
// expressions (e.g. ".a = 5") can never mutate the document being checked.
// Expression evaluation errors are operational errors, not check findings.
func (c *roundTripChecker) checkWithPath(documents []selectedRoundTripDocument) ([]RoundTripFinding, error) {
	findings := []RoundTripFinding{}
	for _, document := range documents {
		inputList := list.New()
		inputList.PushBack(document.node.Copy())
		readOnlyContext := Context{MatchingNodes: inputList, DontAutoCreate: true}
		matchContext, err := c.navigator.GetMatchingNodes(readOnlyContext, c.pathNode)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate path expression for document %d: %w", document.originalIndex, err)
		}
		for element := matchContext.MatchingNodes.Front(); element != nil; element = element.Next() {
			matched := element.Value.(*CandidateNode)
			findings = append(findings, c.roundTripUnit(matched, document.originalIndex)...)
		}
	}
	return findings, nil
}

// roundTripUnit encodes a single matched node, re-decodes the output and
// compares the re-decoded node against the match.
func (c *roundTripChecker) roundTripUnit(matched *CandidateNode, document uint) []RoundTripFinding {
	findings := []RoundTripFinding{}
	basePath := matched.GetPath()

	node := matched
	encoder := c.outputFormat.EncoderFactory()
	if !encoder.CanHandleAliases() {
		exploded, err := roundTripExplode(matched)
		if err != nil {
			findings = append(findings, RoundTripFinding{
				Kind:     RoundTripFindingEncodeError,
				Document: &document,
				Path:     RoundTripPathString(basePath),
				Phase:    "encode",
				Message:  err.Error(),
			})
			return findings
		}
		node = exploded
	}

	buffer := bytes.NewBuffer(nil)
	bufferedWriter := bufio.NewWriter(buffer)
	if err := encoder.Encode(bufferedWriter, node); err != nil {
		findings = append(findings, RoundTripFinding{
			Kind:     RoundTripFindingEncodeError,
			Document: &document,
			Path:     RoundTripPathString(basePath),
			Phase:    "encode",
			Message:  err.Error(),
		})
		return findings
	}
	if err := bufferedWriter.Flush(); err != nil {
		findings = append(findings, RoundTripFinding{
			Kind:     RoundTripFindingEncodeError,
			Document: &document,
			Path:     RoundTripPathString(basePath),
			Phase:    "encode",
			Message:  err.Error(),
		})
		return findings
	}

	afterDocuments, err := c.redecode(buffer)
	if err != nil {
		findings = append(findings, RoundTripFinding{
			Kind:     RoundTripFindingEncodeError,
			Document: &document,
			Path:     RoundTripPathString(basePath),
			Phase:    "redecode",
			Message:  err.Error(),
		})
		return findings
	}
	if len(afterDocuments) == 0 {
		findings = append(findings, RoundTripFinding{
			Kind:     RoundTripFindingMissing,
			Document: &document,
			Path:     RoundTripPathString(basePath),
		})
		return findings
	}

	findings = append(findings, compareRoundTripTrees(node, afterDocuments[0], document, basePath, c.options.CompareStrategy)...)
	return findings
}

func (c *roundTripChecker) redecode(buffer *bytes.Buffer) ([]*CandidateNode, error) {
	decoder := c.outputFormat.DecoderFactory()
	if err := decoder.Init(bytes.NewReader(buffer.Bytes())); err != nil {
		return nil, err
	}
	documents := []*CandidateNode{}
	for {
		node, err := decoder.Decode()
		if errors.Is(err, io.EOF) {
			return documents, nil
		}
		if err != nil {
			return nil, err
		}
		documents = append(documents, node)
	}
}

func buildRoundTripSummary(report *RoundTripReport) RoundTripSummary {
	summary := RoundTripSummary{
		Files:         len(report.Files),
		StatusCounts:  map[string]int{},
		FindingCounts: map[string]int{},
	}
	for _, file := range report.Files {
		summary.StatusCounts[string(file.Status)]++
		for _, finding := range file.Findings {
			summary.FindingCounts[string(finding.Kind)]++
		}
	}
	return summary
}
