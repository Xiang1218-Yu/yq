package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"github.com/spf13/cobra"
)

// ExitCodeError carries an explicit process exit code. yq.go reads the code
// with errors.As; any other error keeps the historical exit code of 1.
type ExitCodeError struct {
	Code  int
	cause error
}

func (e *ExitCodeError) Error() string {
	if e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *ExitCodeError) Unwrap() error {
	return e.cause
}

func newSilentExitCodeError(code int) *ExitCodeError {
	return &ExitCodeError{Code: code}
}

func newExitCodeError(code int, err error) *ExitCodeError {
	return &ExitCodeError{Code: code, cause: err}
}

var (
	roundTripCompare      string
	roundTripDocumentArgs []string
	roundTripPath         string
	roundTripReportFormat string
	roundTripReportFile   string
)

func createRoundTripCommand() *cobra.Command {
	command := &cobra.Command{
		Use:     "roundtrip [file...]",
		Aliases: []string{"rt"},
		Short:   "offline round-trip check: decode, re-encode and re-decode input files and report semantic changes",
		Long: `Decodes each input file, re-encodes it with the output format and decodes
the result again, then compares the two internal node trees.

The check reports parse failures, encode failures (nodes the output format
cannot represent), document count changes and path level type, value, missing
and key order changes as deterministic text or JSON. It never writes to the
input files and does not affect regular eval/eval-all conversion behaviour.`,
		Args:          cobra.ArbitraryArgs,
		RunE:          evaluateRoundTrip,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	command.SetFlagErrorFunc(func(command *cobra.Command, err error) error {
		fmt.Fprintln(command.ErrOrStderr(), "Error: "+err.Error())
		return newExitCodeError(2, err)
	})

	command.Flags().StringVar(&roundTripCompare, "compare", "semantic", "[semantic|ordered] comparison strategy: ordered also treats mapping key order changes as failures")
	command.Flags().StringArrayVar(&roundTripDocumentArgs, "document", nil, "only check the given 0 based input document indexes (comma separated or repeated)")
	command.Flags().StringVar(&roundTripPath, "path", "", "only round-trip nodes matching this yq expression (e.g. '.items[]')")
	command.Flags().StringVar(&roundTripReportFormat, "report-format", "text", "[text|json] report output format")
	command.Flags().StringVar(&roundTripReportFile, "report-file", "", "write the report to this file atomically instead of stdout")
	if err := command.MarkFlagFilename("report-file"); err != nil {
		panic(err)
	}
	if err := command.RegisterFlagCompletionFunc("compare", cobra.FixedCompletions([]string{"semantic", "ordered"}, cobra.ShellCompDirectiveNoFileComp)); err != nil {
		panic(err)
	}
	if err := command.RegisterFlagCompletionFunc("report-format", cobra.FixedCompletions([]string{"text", "json"}, cobra.ShellCompDirectiveNoFileComp)); err != nil {
		panic(err)
	}

	return command
}

func evaluateRoundTrip(cmd *cobra.Command, originalArgs []string) error {
	args := processStdInArgs(originalArgs)

	if err := configureFormats(args); err != nil {
		return failRoundTrip(cmd, err)
	}

	// Apply the same encoding option globals (indent, separators, format
	// preferences) as regular conversion, but never colourise the
	// intermediate bytes so reports stay deterministic.
	colorsEnabled = false
	if _, err := configureEncoder(); err != nil {
		return failRoundTrip(cmd, err)
	}

	if roundTripCompare == "" {
		return failRoundTrip(cmd, fmt.Errorf("compare strategy cannot be empty, please use semantic or ordered"))
	}

	documentIndexes, err := parseRoundTripDocumentArgs(roundTripDocumentArgs)
	if err != nil {
		return failRoundTrip(cmd, err)
	}

	reportFormat := yqlib.RoundTripReportFormat(roundTripReportFormat)

	options := yqlib.RoundTripOptions{
		InputFormatName:  inputFormat,
		OutputFormatName: outputFormat,
		CompareStrategy:  yqlib.RoundTripCompareStrategy(roundTripCompare),
		DocumentIndexes:  documentIndexes,
		PathExpression:   roundTripPath,
	}

	report, err := yqlib.CheckRoundTrip(args, options)
	if err != nil {
		return failRoundTrip(cmd, err)
	}

	if err := yqlib.WriteRoundTripReport(report, reportFormat, roundTripReportFile, cmd.OutOrStdout()); err != nil {
		return failRoundTrip(cmd, fmt.Errorf("failed to write report: %w", err))
	}

	if report.Status != yqlib.RoundTripStatusOK {
		return newSilentExitCodeError(1)
	}
	return nil
}

// failRoundTrip reports an operational/usage error on stderr (exit code 2).
// Check findings are not errors and return a silent code 1 instead.
func failRoundTrip(cmd *cobra.Command, err error) error {
	fmt.Fprintln(cmd.ErrOrStderr(), "Error: "+err.Error())
	return newExitCodeError(2, err)
}

func parseRoundTripDocumentArgs(values []string) ([]uint, error) {
	var indexes []uint
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				return nil, fmt.Errorf("invalid document index %q: must be a 0 based integer", token)
			}
			index, err := strconv.ParseUint(token, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid document index %q: %w", token, err)
			}
			indexes = append(indexes, uint(index))
		}
	}
	return indexes, nil
}
