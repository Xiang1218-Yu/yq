package yqlib

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// RoundTripReportFormat selects the deterministic report serialisation.
type RoundTripReportFormat string

const (
	RoundTripReportFormatText RoundTripReportFormat = "text"
	RoundTripReportFormatJSON RoundTripReportFormat = "json"
)

// RenderRoundTripReport serialises a report in the requested format. The
// output is deterministic: findings are pre-sorted, JSON object keys are
// emitted in lexical order and there are no timestamps or random fields.
func RenderRoundTripReport(report *RoundTripReport, format RoundTripReportFormat) ([]byte, error) {
	switch format {
	case RoundTripReportFormatText, "":
		return []byte(renderRoundTripTextReport(report)), nil
	case RoundTripReportFormatJSON:
		return renderRoundTripJSONReport(report)
	default:
		return nil, fmt.Errorf("unknown report format %q, please use text or json", format)
	}
}

// WriteRoundTripReport writes the report to stdout when destination is empty,
// or atomically replaces the destination file: data is written to a temporary
// file in the same directory, fsynced and renamed into place. Any failure
// removes the temporary file and leaves the destination untouched.
func WriteRoundTripReport(report *RoundTripReport, format RoundTripReportFormat, destination string, stdout io.Writer) error {
	data, err := RenderRoundTripReport(report, format)
	if err != nil {
		return err
	}
	if destination == "" {
		_, err := stdout.Write(data)
		return err
	}
	return writeRoundTripReportFileAtomically(destination, data)
}

func writeRoundTripReportFileAtomically(destination string, data []byte) (err error) {
	directory := filepath.Dir(destination)
	tempFile, err := os.CreateTemp(directory, ".roundtrip-report-*")
	if err != nil {
		return err
	}
	tempName := tempFile.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tempName)
		}
	}()

	if _, err = tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err = tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err = tempFile.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tempName, 0o644); err != nil {
		return err
	}
	return os.Rename(tempName, destination)
}

func renderRoundTripJSONReport(report *RoundTripReport) ([]byte, error) {
	for index := range report.Files {
		sortFindings(report.Files[index].Findings)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// roundTripTextStatusOrder and roundTripTextFindingOrder fix the summary
// ordering so text output is deterministic.
var roundTripTextStatusOrder = []RoundTripStatus{
	RoundTripStatusOK,
	RoundTripStatusSemanticDiff,
	RoundTripStatusEncodeError,
	RoundTripStatusParseError,
}

var roundTripTextFindingOrder = []RoundTripFindingKind{
	RoundTripFindingParseError,
	RoundTripFindingEncodeError,
	RoundTripFindingDocCountChanged,
	RoundTripFindingTypeChanged,
	RoundTripFindingValueChanged,
	RoundTripFindingMissing,
	RoundTripFindingAdded,
	RoundTripFindingKeyOrderChanged,
}

func renderRoundTripTextReport(report *RoundTripReport) string {
	for index := range report.Files {
		sortFindings(report.Files[index].Findings)
	}
	var builder strings.Builder

	builder.WriteString("round-trip check report\n")
	builder.WriteString("input_format: " + report.Parameters.InputFormat + "\n")
	builder.WriteString("output_format: " + report.Parameters.OutputFormat + "\n")
	builder.WriteString("compare: " + string(report.Parameters.Compare) + "\n")
	if report.Parameters.Path != "" {
		builder.WriteString("path: " + report.Parameters.Path + "\n")
	}
	if len(report.Parameters.Documents) > 0 {
		indexes := make([]string, len(report.Parameters.Documents))
		for index, document := range report.Parameters.Documents {
			indexes[index] = strconv.FormatUint(uint64(document), 10)
		}
		builder.WriteString("documents: " + strings.Join(indexes, ",") + "\n")
	}

	for _, file := range report.Files {
		builder.WriteString("file: " + file.Filename + "\n")
		builder.WriteString("  status: " + string(file.Status) + "\n")
		builder.WriteString(fmt.Sprintf("  documents: %d -> %d\n", file.DocumentsBefore, file.DocumentsAfter))
		for _, finding := range file.Findings {
			builder.WriteString("  " + renderRoundTripTextFinding(finding) + "\n")
		}
	}

	builder.WriteString("files:")
	for index, status := range roundTripTextStatusOrder {
		if index > 0 {
			builder.WriteString(";")
		}
		builder.WriteString(" " + string(status) + "=" + strconv.Itoa(report.Summary.StatusCounts[string(status)]))
	}
	builder.WriteString("\n")
	builder.WriteString("findings:")
	for index, kind := range roundTripTextFindingOrder {
		if index > 0 {
			builder.WriteString(";")
		}
		builder.WriteString(" " + string(kind) + "=" + strconv.Itoa(report.Summary.FindingCounts[string(kind)]))
	}
	builder.WriteString("\n")
	builder.WriteString("overall_status: " + string(report.Status) + "\n")

	return builder.String()
}

func renderRoundTripTextFinding(finding RoundTripFinding) string {
	var builder strings.Builder

	hasLocation := finding.Document != nil || finding.Path != ""
	if hasLocation {
		if finding.Document != nil {
			builder.WriteString(fmt.Sprintf("D%d ", *finding.Document))
		} else {
			builder.WriteString("   ")
		}
		path := finding.Path
		if path == "" {
			path = "."
		}
		builder.WriteString(path + ": ")
	}

	switch finding.Kind {
	case RoundTripFindingParseError:
		builder.WriteString("parse_error: " + finding.Message)
	case RoundTripFindingEncodeError:
		phase := finding.Phase
		if phase == "" {
			phase = "encode"
		}
		builder.WriteString("encode_error (" + phase + "): " + finding.Message)
	case RoundTripFindingDocCountChanged:
		builder.WriteString(fmt.Sprintf("doc_count_changed: %d -> %d", *finding.ExpectedCount, *finding.ActualCount))
	case RoundTripFindingTypeChanged:
		builder.WriteString("type_changed: " + finding.Before + " -> " + finding.After)
	case RoundTripFindingValueChanged:
		builder.WriteString("value_changed: " + finding.Before + " -> " + finding.After)
	case RoundTripFindingMissing:
		builder.WriteString("missing")
	case RoundTripFindingAdded:
		builder.WriteString("added")
	case RoundTripFindingKeyOrderChanged:
		builder.WriteString("key_order_changed: " + renderRoundTripKeyList(finding.BeforeKeys) + " -> " + renderRoundTripKeyList(finding.AfterKeys))
		if finding.Informational {
			builder.WriteString(" (informational)")
		}
	}
	return builder.String()
}

func renderRoundTripKeyList(keys []string) string {
	quoted := make([]string, len(keys))
	for index, key := range keys {
		quoted[index] = strconv.Quote(key)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
