#!/bin/bash

oneTimeSetUp() {
  TEST_DIR=$(mktemp -d)
  GOOD_FILE="$TEST_DIR/good.yaml"
  MULTI_FILE="$TEST_DIR/multi.yaml"
  BAD_FILE="$TEST_DIR/bad.yaml"
  CSV_BAD_FILE="$TEST_DIR/csv-bad.yaml"
  printf 'a: 1\nb:\n  - one\n  - two\n' > "$GOOD_FILE"
  printf 'a: 1\n---\nb: 2\n' > "$MULTI_FILE"
  printf 'a: [1\n' > "$BAD_FILE"
  printf 'things:\n  - - 1\n    - 2\n' > "$CSV_BAD_FILE"
}

oneTimeTearDown() {
  rm -rf "$TEST_DIR"
}

testRoundTripOK() {
  result=$(./yq roundtrip -p yaml -o json "$GOOD_FILE")
  assertEquals 0 $?
  assertContains "$result" "status: ok"
  assertContains "$result" "overall_status: ok"
}

testRoundTripStdin() {
  result=$(printf 'a: 1\n' | ./yq roundtrip -p yaml -o json)
  assertEquals 0 $?
  assertContains "$result" "file: -"
  assertContains "$result" "status: ok"
}

testRoundTripSemanticDiffExitCodeOne() {
  result=$(./yq roundtrip -p yaml -o props "$MULTI_FILE")
  assertEquals 1 $?
  assertContains "$result" "doc_count_changed: 2 -> 1"
  assertContains "$result" "type_changed: !!int -> !!str"
}

testRoundTripParseErrorExitCodeOne() {
  result=$(./yq roundtrip -p yaml -o json "$BAD_FILE")
  assertEquals 1 $?
  assertContains "$result" "status: parse_error"
  assertContains "$result" "parse_error:"
}

testRoundTripEncodeErrorExitCodeOne() {
  result=$(./yq roundtrip -p yaml -o csv "$CSV_BAD_FILE")
  assertEquals 1 $?
  assertContains "$result" "encode_error (encode):"
}

testRoundTripBadStrategyExitCodeTwo() {
  result=$(./yq roundtrip -p yaml -o json --compare bogus "$GOOD_FILE" 2>&1)
  assertEquals 2 $?
  assertContains "$result" "Error: unknown compare strategy"
}

testRoundTripDocumentFilter() {
  result=$(./yq roundtrip -p yaml -o json --document 0 "$MULTI_FILE")
  assertEquals 0 $?
  assertContains "$result" "documents: 0"
  assertContains "$result" "status: ok"
}

testRoundTripPathFilter() {
  result=$(./yq roundtrip -p yaml -o json --path '.b[]' "$GOOD_FILE")
  assertEquals 0 $?
  assertContains "$result" "path: .b[]"
  assertContains "$result" "status: ok"
}

testRoundTripJSONReport() {
  status=$(./yq roundtrip -p yaml -o json --report-format json "$GOOD_FILE" | ./yq '.status')
  assertEquals 0 $?
  assertEquals "ok" "$status"
}

testRoundTripReportFileWrittenAtomically() {
  report="$TEST_DIR/report.json"
  ./yq roundtrip -p yaml -o json --report-format json --report-file "$report" "$GOOD_FILE"
  assertEquals 0 $?
  assertTrue "report file should exist" "[ -f '$report' ]"
  status=$(./yq -p json -o yaml '.status' "$report")
  assertEquals "ok" "$status"
}

testRoundTripReportFileFailureLeavesNothing() {
  missing="$TEST_DIR/nested/missing/report.json"
  ./yq roundtrip -p yaml -o json --report-file "$missing" "$GOOD_FILE" 2>/dev/null
  assertEquals 2 $?
  assertTrue "no partial report may exist" "[ ! -e '$missing' ]"
  assertTrue "no parent directory may be created" "[ ! -d '$TEST_DIR/nested' ]"
}

testRoundTripUnknownFlagExitCodeTwo() {
  result=$(./yq roundtrip --nope 2>&1)
  assertEquals 2 $?
  assertContains "$result" "Error: unknown flag"
  assertNotContains "$result" "Usage:"
}

testRoundTripKeyOrderStrategies() {
  input="$TEST_DIR/attrs.yaml"
  printf 'a:\n  child: x\n  +@attr: y\n' > "$input"
  result=$(./yq roundtrip -p yaml -o xml --compare semantic "$input")
  assertEquals 0 $?
  assertContains "$result" "key_order_changed"
  assertContains "$result" "(informational)"
  result=$(./yq roundtrip -p yaml -o xml --compare ordered "$input")
  assertEquals 1 $?
  assertContains "$result" "key_order_changed"
  assertNotContains "$result" "(informational)"
}

testRoundTripDocumentFilterKeepsOriginalIndexes() {
  triple="$TEST_DIR/triple.yaml"
  printf 'v: doc0\n---\nv: doc1\n---\nv: doc2\n' > "$triple"
  result=$(./yq roundtrip -p yaml -o props --document 0,2 "$triple")
  assertEquals 1 $?
  assertContains "$result" "documents: 2 -> 1"
  assertContains "$result" "doc_count_changed: 2 -> 1"
  assertContains "$result" "D2 .: missing"
  assertNotContains "$result" "D1 "
}

testRoundTripDoesNotChangeConversion() {
  result=$(./yq eval -p yaml -o json '.a' "$GOOD_FILE")
  assertEquals 0 $?
  assertEquals "1" "$result"
}

source ./scripts/shunit2
