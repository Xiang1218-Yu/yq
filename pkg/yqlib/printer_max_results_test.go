package yqlib

import (
	"container/list"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

type fakeResultPrinter struct {
	printed       []string
	batches       int
	nulSepOutput  bool
	appendix      io.Reader
	failOnPrinted int
	err           error
}

func (f *fakeResultPrinter) PrintResults(matchingNodes *list.List) error {
	f.batches = f.batches + 1
	if matchingNodes == nil {
		return nil
	}
	for el := matchingNodes.Front(); el != nil; el = el.Next() {
		node := el.Value.(*CandidateNode)
		f.printed = append(f.printed, node.Value)
		if f.err != nil && f.failOnPrinted == len(f.printed) {
			return f.err
		}
	}
	return nil
}

func (f *fakeResultPrinter) PrintedAnything() bool {
	return len(f.printed) > 0
}

func (f *fakeResultPrinter) SetAppendix(reader io.Reader) {
	f.appendix = reader
}

func (f *fakeResultPrinter) SetNulSepOutput(nulSepOutput bool) {
	f.nulSepOutput = nulSepOutput
}

func maxResultsTestNode(value string, filename string, fileIndex int, documentIndex uint) *CandidateNode {
	node := createScalarNode(nil, value)
	node.filename = filename
	node.fileIndex = fileIndex
	node.document = documentIndex
	return node
}

func maxResultsTestList(values ...*CandidateNode) *list.List {
	resultList := list.New()
	for _, value := range values {
		resultList.PushBack(value)
	}
	return resultList
}

func assertMaxResultsError(t *testing.T, err error, limit uint64, produced uint64, filename string, fileIndex int, documentIndex uint) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a max results error, got nil")
	}
	if !errors.Is(err, ErrMaxResultsExceeded) {
		t.Fatalf("expected errors.Is(err, ErrMaxResultsExceeded), got %v", err)
	}
	var maxResultsErr *MaxResultsExceededError
	if !errors.As(err, &maxResultsErr) {
		t.Fatalf("expected errors.As to *MaxResultsExceededError, got %T %v", err, err)
	}
	if maxResultsErr.Limit != limit {
		t.Errorf("expected limit %v, got %v", limit, maxResultsErr.Limit)
	}
	if maxResultsErr.Produced != produced {
		t.Errorf("expected produced %v, got %v", produced, maxResultsErr.Produced)
	}
	if maxResultsErr.Filename != filename {
		t.Errorf("expected filename %q, got %q", filename, maxResultsErr.Filename)
	}
	if maxResultsErr.FileIndex != fileIndex {
		t.Errorf("expected file index %v, got %v", fileIndex, maxResultsErr.FileIndex)
	}
	if maxResultsErr.DocumentIndex != documentIndex {
		t.Errorf("expected document index %v, got %v", documentIndex, maxResultsErr.DocumentIndex)
	}
}

func TestMaxResultsPrinterZeroLimit(t *testing.T) {
	inner := &fakeResultPrinter{}
	printer := NewMaxResultsPrinter(inner, 0)

	err := printer.PrintResults(maxResultsTestList(
		maxResultsTestNode("a", "f.yml", 0, 0),
		maxResultsTestNode("b", "f.yml", 0, 0),
	))
	assertMaxResultsError(t, err, 0, 0, "f.yml", 0, 0)
	if len(inner.printed) != 0 {
		t.Errorf("expected no results to be printed, got %v", len(inner.printed))
	}
}

func TestMaxResultsPrinterWithinLimitAcrossBatches(t *testing.T) {
	inner := &fakeResultPrinter{}
	printer := NewMaxResultsPrinter(inner, 4)

	for batch := 0; batch < 2; batch++ {
		err := printer.PrintResults(maxResultsTestList(
			maxResultsTestNode(fmt.Sprintf("a%v", batch), "f.yml", 0, 0),
			maxResultsTestNode(fmt.Sprintf("b%v", batch), "f.yml", 0, 0),
		))
		if err != nil {
			t.Fatalf("expected no error within limit, got %v", err)
		}
	}
	if len(inner.printed) != 4 {
		t.Errorf("expected 4 printed results, got %v", inner.printed)
	}
}

func TestMaxResultsPrinterExactLimit(t *testing.T) {
	inner := &fakeResultPrinter{}
	printer := NewMaxResultsPrinter(inner, 2)

	err := printer.PrintResults(maxResultsTestList(
		maxResultsTestNode("a", "f.yml", 0, 0),
		maxResultsTestNode("b", "f.yml", 0, 0),
	))
	if err != nil {
		t.Fatalf("expected no error when result count equals limit, got %v", err)
	}
}

func TestMaxResultsPrinterStopsWithinArrayExpansionBatch(t *testing.T) {
	inner := &fakeResultPrinter{}
	printer := NewMaxResultsPrinter(inner, 2)

	err := printer.PrintResults(maxResultsTestList(
		maxResultsTestNode("a", "f.yml", 0, 0),
		maxResultsTestNode("b", "f.yml", 0, 0),
		maxResultsTestNode("c", "f.yml", 0, 0),
		maxResultsTestNode("d", "f.yml", 0, 0),
		maxResultsTestNode("e", "f.yml", 0, 0),
	))

	assertMaxResultsError(t, err, 2, 2, "f.yml", 0, 0)
	if result := fmt.Sprint(inner.printed); result != "[a b]" {
		t.Errorf("expected only the first 2 results printed, got %v", result)
	}
}

func TestMaxResultsPrinterAccumulatesAcrossFilesAndDocuments(t *testing.T) {
	inner := &fakeResultPrinter{}
	printer := NewMaxResultsPrinter(inner, 3)

	err := printer.PrintResults(maxResultsTestList(
		maxResultsTestNode("a", "f1.yml", 0, 0),
		maxResultsTestNode("b", "f1.yml", 0, 0),
	))
	if err != nil {
		t.Fatalf("expected first batch within limit, got %v", err)
	}

	err = printer.PrintResults(maxResultsTestList(
		maxResultsTestNode("c", "f2.yml", 1, 0),
		maxResultsTestNode("d", "f2.yml", 1, 1),
	))

	assertMaxResultsError(t, err, 3, 3, "f2.yml", 1, 1)
	if result := fmt.Sprint(inner.printed); result != "[a b c]" {
		t.Errorf("expected prefix [a b c] printed, got %v", result)
	}

	// the next batch must not print anything once the limit has tripped
	err = printer.PrintResults(nodeToList(maxResultsTestNode("e", "f2.yml", 1, 1)))
	assertMaxResultsError(t, err, 3, 3, "f2.yml", 1, 1)
	if len(inner.printed) != 3 {
		t.Errorf("expected no further results printed, got %v", inner.printed)
	}
}

func TestMaxResultsPrinterStdinAndNullInputLocations(t *testing.T) {
	inner := &fakeResultPrinter{}
	printer := NewMaxResultsPrinter(inner, 1)

	err := printer.PrintResults(nodeToList(maxResultsTestNode("a", "-", 0, 0)))
	if err != nil {
		t.Fatalf("expected first stdin result allowed, got %v", err)
	}

	err = printer.PrintResults(nodeToList(maxResultsTestNode("b", "-", 0, 0)))
	assertMaxResultsError(t, err, 1, 1, "-", 0, 0)

	inner = &fakeResultPrinter{}
	printer = NewMaxResultsPrinter(inner, 1)
	err = printer.PrintResults(nodeToList(maxResultsTestNode("c", "", 0, 0)))
	if err != nil {
		t.Fatalf("expected null input result within limit, got %v", err)
	}
}

func TestMaxResultsPrinterPropagatesWriteError(t *testing.T) {
	writeError := errors.New("disk on fire")
	inner := &fakeResultPrinter{failOnPrinted: 1, err: writeError}
	printer := NewMaxResultsPrinter(inner, 5)

	err := printer.PrintResults(maxResultsTestList(
		maxResultsTestNode("a", "f.yml", 0, 0),
		maxResultsTestNode("b", "f.yml", 0, 0),
	))
	if !errors.Is(err, writeError) {
		t.Fatalf("expected the underlying write error, got %v", err)
	}
	if errors.Is(err, ErrMaxResultsExceeded) {
		t.Fatalf("write error must not be reported as a max results error")
	}
}

func TestMaxResultsPrinterEmptyAndNilBatches(t *testing.T) {
	inner := &fakeResultPrinter{}
	printer := NewMaxResultsPrinter(inner, 1)

	if err := printer.PrintResults(list.New()); err != nil {
		t.Fatalf("expected no error for empty batch, got %v", err)
	}
	if err := printer.PrintResults(nil); err != nil {
		t.Fatalf("expected no error for nil batch, got %v", err)
	}
	if inner.batches != 2 {
		t.Errorf("expected empty batches delegated to inner printer, got %v batches", inner.batches)
	}
}

func TestMaxResultsPrinterDelegatesConfiguration(t *testing.T) {
	inner := &fakeResultPrinter{}
	printer := NewMaxResultsPrinter(inner, 1)

	printer.SetNulSepOutput(true)
	if !inner.nulSepOutput {
		t.Error("expected NUL separator setting delegated to inner printer")
	}
	reader := strings.NewReader("appendix")
	printer.SetAppendix(reader)
	if inner.appendix == nil {
		t.Error("expected appendix delegated to inner printer")
	}
	if printer.PrintedAnything() {
		t.Error("expected PrintedAnything delegated to inner printer")
	}
}
