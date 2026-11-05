package yqlib

import (
	"container/list"
	"errors"
	"fmt"
	"io"
)

// ErrMaxResultsExceeded is returned when an evaluation would hand more
// top-level results to the output writer than the configured limit allows.
// It is distinct from decoding, expression and output writing errors.
var ErrMaxResultsExceeded = errors.New("maximum number of results exceeded")

// MaxResultsExceededError describes a --max-results limit breach. The
// diagnosis is scoped to a single command invocation and identifies the
// result that would have been produced next.
type MaxResultsExceededError struct {
	// Limit is the configured maximum number of top-level results.
	Limit uint64
	// Produced is the number of top-level results already written.
	Produced uint64
	// Filename is the file the next result came from ("-" for stdin,
	// empty for null input).
	Filename string
	// FileIndex is the zero based index of the next result's file.
	FileIndex int
	// DocumentIndex is the zero based index of the next result's document.
	DocumentIndex uint
}

func (e *MaxResultsExceededError) Error() string {
	return fmt.Sprintf(
		"%v: --max-results is %v and %v already been produced; the next result is from file %q (file index %v, document index %v)",
		ErrMaxResultsExceeded,
		e.Limit,
		producedResultsPhrase(e.Produced),
		e.Filename,
		e.FileIndex,
		e.DocumentIndex,
	)
}

func producedResultsPhrase(count uint64) string {
	if count == 1 {
		return "1 result has"
	}
	return fmt.Sprintf("%v results have", count)
}

// Unwrap allows errors.Is(err, ErrMaxResultsExceeded) checks while
// errors.As exposes the structured MaxResultsExceededError fields.
func (e *MaxResultsExceededError) Unwrap() error {
	return ErrMaxResultsExceeded
}

// NewMaxResultsPrinter wraps the given printer so that at most limit
// top-level results are printed during the lifetime of the returned
// printer. Counting spans every PrintResults call, so it accumulates
// across documents, files and standard input for one command invocation.
// When the limit is reached, the allowed prefix is printed and the next
// result triggers a *MaxResultsExceededError instead of being written.
// A limit of 0 rejects the first result; callers disable the wrapper when the flag is absent.
func NewMaxResultsPrinter(printer Printer, limit uint64) Printer {
	return &maxResultsPrinter{
		inner: printer,
		limit: limit,
	}
}

type maxResultsPrinter struct {
	inner    Printer
	limit    uint64
	produced uint64
}

func (p *maxResultsPrinter) PrintedAnything() bool {
	return p.inner.PrintedAnything()
}

func (p *maxResultsPrinter) SetAppendix(reader io.Reader) {
	p.inner.SetAppendix(reader)
}

func (p *maxResultsPrinter) SetNulSepOutput(nulSepOutput bool) {
	p.inner.SetNulSepOutput(nulSepOutput)
}

func (p *maxResultsPrinter) PrintResults(matchingNodes *list.List) error {
	if matchingNodes == nil || matchingNodes.Len() == 0 {
		return p.inner.PrintResults(matchingNodes)
	}

	allowed := list.New()
	var boundaryNode *CandidateNode
	for el := matchingNodes.Front(); el != nil; el = el.Next() {
		if p.produced+uint64(allowed.Len()) < p.limit {
			allowed.PushBack(el.Value)
			continue
		}
		boundaryNode = el.Value.(*CandidateNode)
		break
	}

	if boundaryNode == nil {
		if err := p.inner.PrintResults(matchingNodes); err != nil {
			return err
		}
		p.produced += uint64(matchingNodes.Len())
		return nil
	}

	if allowed.Len() > 0 {
		if err := p.inner.PrintResults(allowed); err != nil {
			return err
		}
		p.produced += uint64(allowed.Len())
	}

	return &MaxResultsExceededError{
		Limit:         p.limit,
		Produced:      p.produced,
		Filename:      boundaryNode.GetFilename(),
		FileIndex:     boundaryNode.GetFileIndex(),
		DocumentIndex: boundaryNode.GetDocument(),
	}
}
