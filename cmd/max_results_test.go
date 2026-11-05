package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikefarah/yq/v4/pkg/yqlib"
)

type maxResultsCommandResult struct {
	stdout string
	stderr string
	err    error
}

func pipeStdin(t *testing.T, content string) {
	t.Helper()
	originalStdin := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdin pipe: %v", err)
	}
	if _, err := writer.WriteString(content); err != nil {
		t.Fatalf("failed to write stdin pipe: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close stdin pipe: %v", err)
	}
	os.Stdin = reader
	t.Cleanup(func() {
		os.Stdin = originalStdin
		_ = reader.Close()
	})
}

func runMaxResultsCommand(t *testing.T, args []string, stdin ...string) maxResultsCommandResult {
	t.Helper()
	// mimic the initial global state of a fresh process: these variables
	// are mutated during a run but not reset when a new command is built.
	completedSuccessfully = false
	unwrapScalar = false
	if len(stdin) > 0 {
		pipeStdin(t, stdin[0])
	}

	command := New()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs(args)

	err := command.Execute()
	return maxResultsCommandResult{
		stdout: stdout.String(),
		stderr: stderr.String(),
		err:    err,
	}
}

func writeTestFile(t *testing.T, dir string, name string, content string) string {
	t.Helper()
	filename := filepath.Join(dir, name)
	if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write %v: %v", filename, err)
	}
	return filename
}

func assertMaxResultsLimitError(t *testing.T, result maxResultsCommandResult, limit int, produced int, filename string, fileIndex int, documentIndex uint) *yqlib.MaxResultsExceededError {
	t.Helper()
	if result.err == nil {
		t.Fatalf("expected a max results error, got nil; stdout=%q stderr=%q", result.stdout, result.stderr)
	}
	var maxResultsErr *yqlib.MaxResultsExceededError
	if !errors.As(result.err, &maxResultsErr) {
		t.Fatalf("expected *yqlib.MaxResultsExceededError, got %T: %v", result.err, result.err)
	}
	if !errors.Is(result.err, yqlib.ErrMaxResultsExceeded) {
		t.Error("expected errors.Is(err, yqlib.ErrMaxResultsExceeded)")
	}
	if maxResultsErr.Limit != uint64(limit) {
		t.Errorf("expected limit %v, got %v", limit, maxResultsErr.Limit)
	}
	if maxResultsErr.Produced != uint64(produced) {
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
	if result.stderr == "" || !bytes.Contains([]byte(result.stderr), []byte("Error: maximum number of results exceeded")) {
		t.Errorf("expected a stable max results error on stderr, got %q", result.stderr)
	}
	return maxResultsErr
}

func TestMaxResultsArrayExpansion(t *testing.T) {
	dir := t.TempDir()
	filename := writeTestFile(t, dir, "array.yml", "- a\n- b\n- c\n- d\n- e\n")

	result := runMaxResultsCommand(t, []string{"eval", "--max-results", "2", ".[]", filename})

	if result.stdout != "a\nb\n" {
		t.Errorf("expected only the first 2 results, got %q", result.stdout)
	}
	assertMaxResultsLimitError(t, result, 2, 2, filename, 0, 0)
}

func TestMaxResultsZeroRejectsFirstResult(t *testing.T) {
	dir := t.TempDir()
	filename := writeTestFile(t, dir, "array.yml", "- a\n")

	result := runMaxResultsCommand(t, []string{"eval", "--max-results", "0", ".[]", filename})

	if result.stdout != "" {
		t.Errorf("expected no output for a zero-result limit, got %q", result.stdout)
	}
	assertMaxResultsLimitError(t, result, 0, 0, filename, 0, 0)
}

func TestMaxResultsCrossFileAccumulation(t *testing.T) {
	dir := t.TempDir()
	file1 := writeTestFile(t, dir, "f1.yml", "- a\n- b\n")
	file2 := writeTestFile(t, dir, "f2.yml", "- c\n- d\n")

	result := runMaxResultsCommand(t, []string{"eval", "--max-results", "3", ".[]", file1, file2})

	if result.stdout != "a\nb\n---\nc\n" {
		t.Errorf("expected cross-file prefix, got %q", result.stdout)
	}
	assertMaxResultsLimitError(t, result, 3, 3, file2, 1, 0)
}

func TestMaxResultsMultipleDocuments(t *testing.T) {
	dir := t.TempDir()
	filename := writeTestFile(t, dir, "docs.yml", "- a\n- b\n---\n- c\n- d\n")

	result := runMaxResultsCommand(t, []string{"eval", "--max-results", "2", ".[]", filename})

	if result.stdout != "a\nb\n" {
		t.Errorf("expected first document results only, got %q", result.stdout)
	}
	assertMaxResultsLimitError(t, result, 2, 2, filename, 0, 1)
}

func TestMaxResultsStdin(t *testing.T) {
	result := runMaxResultsCommand(t, []string{"eval", "--max-results", "1", ".[]", "-"}, "[1,2,3]\n")

	if result.stdout != "1\n" {
		t.Errorf("expected first stdin result only, got %q", result.stdout)
	}
	assertMaxResultsLimitError(t, result, 1, 1, "-", 0, 0)
}

func TestMaxResultsEvalAll(t *testing.T) {
	dir := t.TempDir()
	file1 := writeTestFile(t, dir, "f1.yml", "- a\n- b\n")
	file2 := writeTestFile(t, dir, "f2.yml", "- c\n- d\n")

	result := runMaxResultsCommand(t, []string{"eval-all", "--max-results", "2", ".[]", file1, file2})

	if result.stdout != "a\nb\n" {
		t.Errorf("expected eval-all prefix, got %q", result.stdout)
	}
	assertMaxResultsLimitError(t, result, 2, 2, file2, 1, 0)
}

func TestMaxResultsInputFormats(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		content    string
		args       []string
		expression string
		prefix     string
	}{
		{
			name:       "yaml",
			filename:   "data.yaml",
			content:    "- a\n- b\n- c\n",
			expression: ".[]",
			prefix:     "a\nb\n",
		},
		{
			name:       "json",
			filename:   "data.json",
			content:    `[{"a":"x"},{"a":"y"},{"a":"z"}]`,
			expression: ".[].a",
			prefix:     "\"x\"\n\"y\"\n",
		},
		{
			name:       "xml",
			filename:   "data.xml",
			content:    "<root><a>1</a><a>2</a><a>3</a></root>",
			args:       []string{"-p", "xml", "-o", "yaml"},
			expression: ".root.a[]",
			prefix:     "1\n2\n",
		},
		{
			name:       "csv",
			filename:   "data.csv",
			content:    "a,b\n1,2\n3,4\n5,6\n",
			expression: ".[].a",
			prefix:     "1\n3\n",
		},
		{
			name:       "tsv",
			filename:   "data.tsv",
			content:    "a\tb\n1\t2\n3\t4\n5\t6\n",
			expression: ".[].a",
			prefix:     "1\n3\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			filename := writeTestFile(t, dir, tt.filename, tt.content)

			limitedArgs := append([]string{"eval"}, tt.args...)
			limitedArgs = append(limitedArgs, "--max-results", "2", tt.expression, filename)
			result := runMaxResultsCommand(t, limitedArgs)

			if result.stdout != tt.prefix {
				t.Errorf("expected prefix %q, got %q", tt.prefix, result.stdout)
			}
			assertMaxResultsLimitError(t, result, 2, 2, filename, 0, 0)

			// the limited output must be a byte-prefix of the unlimited output
			unlimitedArgs := append([]string{"eval"}, tt.args...)
			unlimitedArgs = append(unlimitedArgs, tt.expression, filename)
			full := runMaxResultsCommand(t, unlimitedArgs)
			if full.err != nil {
				t.Fatalf("unlimited run failed: %v", full.err)
			}
			if !bytes.HasPrefix([]byte(full.stdout), []byte(result.stdout)) {
				t.Errorf("limited output %q is not a prefix of unlimited output %q", result.stdout, full.stdout)
			}
		})
	}
}

func TestMaxResultsWriteInPlaceKeepsOriginalFile(t *testing.T) {
	originalContent := "- a\n- b\n- c\n"

	for _, command := range []string{"eval", "eval-all"} {
		t.Run(command, func(t *testing.T) {
			inputDir := t.TempDir()
			tempDir := t.TempDir()
			filename := writeTestFile(t, inputDir, "input.yaml", originalContent)
			for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
				t.Setenv(key, tempDir)
			}

			result := runMaxResultsCommand(t, []string{command, "--inplace", "--max-results", "1", ".[]", filename})

			assertMaxResultsLimitError(t, result, 1, 1, filename, 0, 0)

			content, err := os.ReadFile(filename)
			if err != nil {
				t.Fatalf("failed to read input file: %v", err)
			}
			if string(content) != originalContent {
				t.Errorf("original file must not be overwritten, expected %q, got %q", originalContent, string(content))
			}
			entries, err := os.ReadDir(tempDir)
			if err != nil {
				t.Fatalf("failed to read temp dir: %v", err)
			}
			if len(entries) != 0 {
				t.Errorf("expected no temporary files left behind, got %v", entries)
			}
		})
	}
}

func TestMaxResultsDeterministic(t *testing.T) {
	dir := t.TempDir()
	file1 := writeTestFile(t, dir, "f1.yml", "- a\n- b\n")
	file2 := writeTestFile(t, dir, "f2.yml", "- c\n- d\n")

	args := []string{"eval", "--max-results", "3", ".[]", file1, file2}
	first := runMaxResultsCommand(t, args)
	second := runMaxResultsCommand(t, args)

	if first.stdout != second.stdout {
		t.Errorf("non-deterministic output prefix: %q vs %q", first.stdout, second.stdout)
	}
	if first.err == nil || second.err == nil || first.err.Error() != second.err.Error() {
		t.Errorf("non-deterministic error: %v vs %v", first.err, second.err)
	}
}

func TestMaxResultsExactLimitSucceeds(t *testing.T) {
	dir := t.TempDir()
	filename := writeTestFile(t, dir, "array.yml", "- a\n- b\n- c\n")

	result := runMaxResultsCommand(t, []string{"eval", "--max-results", "3", ".[]", filename})
	if result.err != nil {
		t.Errorf("expected no error when result count equals limit, got %v", result.err)
	}
	if result.stdout != "a\nb\nc\n" {
		t.Errorf("unexpected output %q", result.stdout)
	}
}

func TestMaxResultsUnspecifiedIsUnchanged(t *testing.T) {
	dir := t.TempDir()
	filename := writeTestFile(t, dir, "array.yml", "- a\n- b\n- c\n- d\n- e\n")

	result := runMaxResultsCommand(t, []string{"eval", ".[]", filename})
	if result.err != nil {
		t.Errorf("expected no error without --max-results, got %v", result.err)
	}
	if result.stdout != "a\nb\nc\nd\ne\n" {
		t.Errorf("expected all results unchanged, got %q", result.stdout)
	}
}

func TestMaxResultsNegativeRejected(t *testing.T) {
	dir := t.TempDir()
	filename := writeTestFile(t, dir, "array.yml", "- a\n")

	result := runMaxResultsCommand(t, []string{"eval", "--max-results=-1", ".[]", filename})
	if result.err == nil || result.err.Error() != "max-results must not be negative" {
		t.Errorf("expected negative limit error, got %v", result.err)
	}
}

func TestMaxResultsErrorIsDistinguishable(t *testing.T) {
	dir := t.TempDir()
	validFile := writeTestFile(t, dir, "ok.yml", "- a\n- b\n")
	badJSONFile := writeTestFile(t, dir, "bad.json", "{broken")

	expressionFailure := runMaxResultsCommand(t, []string{"eval", "--max-results", "5", `error("boom")`, validFile})
	if expressionFailure.err == nil {
		t.Fatal("expected expression error")
	}
	var maxResultsErr *yqlib.MaxResultsExceededError
	if errors.As(expressionFailure.err, &maxResultsErr) {
		t.Errorf("expression error must not be reported as max results error: %v", expressionFailure.err)
	}

	decodeFailure := runMaxResultsCommand(t, []string{"eval", "-p", "json", "-o", "yaml", "--max-results", "5", ".", badJSONFile})
	if decodeFailure.err == nil {
		t.Fatal("expected decode error")
	}
	if errors.As(decodeFailure.err, &maxResultsErr) {
		t.Errorf("decode error must not be reported as max results error: %v", decodeFailure.err)
	}

	noMatches := runMaxResultsCommand(t, []string{"eval", "--exit-status", "--max-results", "5", "select(false)", validFile})
	if noMatches.err == nil || noMatches.err.Error() != "no matches found" {
		t.Errorf("expected no matches found error, got %v", noMatches.err)
	}
	if errors.As(noMatches.err, &maxResultsErr) {
		t.Errorf("exit status error must not be reported as max results error: %v", noMatches.err)
	}
}
