#!/bin/bash

setUp() {
  rm -f maxresults_f1.yml maxresults_f2.yml maxresults_inplace.yml maxresults_err.txt maxresults_out.txt maxresults_f1.json maxresults_f1.csv maxresults_f1.tsv maxresults_f1.xml
  printf -- '- a\n- b\n- c\n- d\n- e\n' > maxresults_f1.yml
  printf -- '- c\n- d\n' > maxresults_f2.yml
}

tearDown() {
  rm -f maxresults_f1.yml maxresults_f2.yml maxresults_inplace.yml maxresults_err.txt maxresults_out.txt maxresults_f1.json maxresults_f1.csv maxresults_f1.tsv maxresults_f1.xml
}

assertContains() {
  if printf '%s' "$1" | grep -qF -- "$2"; then
    return 0
  fi
  fail "expected output to contain [$2], got [$1]"
}

testMaxResultsArrayExpansion() {
  output=$(./yq eval --max-results 2 '.[]' maxresults_f1.yml 2>maxresults_err.txt)
  rc=$?
  assertEquals 1 "$rc"
  assertEquals "$(printf 'a\nb')" "$output"
  stderr=$(cat maxresults_err.txt)
  assertContains "$stderr" "Error: maximum number of results exceeded"
  assertContains "$stderr" "--max-results is 2"
  assertContains "$stderr" "2 results have already been produced"
  assertContains "$stderr" 'file "maxresults_f1.yml"'
  assertContains "$stderr" "file index 0"
  assertContains "$stderr" "document index 0"
}

testMaxResultsCrossFileAccumulation() {
  printf -- '- a\n- b\n' > maxresults_f1.yml
  output=$(./yq eval --max-results 3 '.[]' maxresults_f1.yml maxresults_f2.yml 2>maxresults_err.txt)
  rc=$?
  assertEquals 1 "$rc"
  assertEquals "$(printf 'a\nb\n---\nc')" "$output"
  stderr=$(cat maxresults_err.txt)
  assertContains "$stderr" 'file "maxresults_f2.yml"'
  assertContains "$stderr" "file index 1"
}

testMaxResultsEvalAll() {
  printf -- '- a\n- b\n' > maxresults_f1.yml
  output=$(./yq eval-all --max-results 2 '.[]' maxresults_f1.yml maxresults_f2.yml 2>maxresults_err.txt)
  rc=$?
  assertEquals 1 "$rc"
  assertEquals "$(printf 'a\nb')" "$output"
  assertContains "$(cat maxresults_err.txt)" 'file "maxresults_f2.yml"'
}

testMaxResultsStdin() {
  output=$(printf '[1,2,3]\n' | ./yq eval --max-results 1 '.[]' - 2>maxresults_err.txt)
  rc=$?
  assertEquals 1 "$rc"
  assertEquals "1" "$output"
  assertContains "$(cat maxresults_err.txt)" 'file "-"'
}

testMaxResultsMultipleDocuments() {
  printf -- '- a\n- b\n---\n- c\n' > maxresults_f1.yml
  output=$(./yq eval --max-results 2 '.[]' maxresults_f1.yml 2>maxresults_err.txt)
  rc=$?
  assertEquals 1 "$rc"
  assertEquals "$(printf 'a\nb')" "$output"
  assertContains "$(cat maxresults_err.txt)" "document index 1"
}

testMaxResultsInputFormats() {
  printf '[{"a":"x"},{"a":"y"},{"a":"z"}]' > maxresults_f1.json
  output=$(./yq eval --max-results 2 '.[].a' maxresults_f1.json 2>maxresults_err.txt)
  assertEquals 1 $?
  assertEquals "$(printf '"x"\n"y"')" "$output"

  printf 'a,b\n1,2\n3,4\n5,6\n' > maxresults_f1.csv
  output=$(./yq eval --max-results 2 '.[].a' maxresults_f1.csv 2>maxresults_err.txt)
  assertEquals 1 $?
  assertEquals "$(printf '1\n3')" "$output"

  printf 'a\tb\n1\t2\n3\t4\n5\t6\n' > maxresults_f1.tsv
  output=$(./yq eval --max-results 2 '.[].a' maxresults_f1.tsv 2>maxresults_err.txt)
  assertEquals 1 $?
  assertEquals "$(printf '1\n3')" "$output"

  printf '<root><a>1</a><a>2</a><a>3</a></root>' > maxresults_f1.xml
  output=$(./yq eval -p xml -o yaml --max-results 2 '.root.a[]' maxresults_f1.xml 2>maxresults_err.txt)
  assertEquals 1 $?
  assertEquals "$(printf '1\n2')" "$output"
}

testMaxResultsInPlaceDoesNotOverwrite() {
  original=$(cat maxresults_f1.yml)
  output=$(./yq eval --inplace --max-results 1 '.[]' maxresults_f1.yml 2>maxresults_err.txt)
  rc=$?
  assertEquals 1 "$rc"
  assertEquals "" "$output"
  assertEquals "$original" "$(cat maxresults_f1.yml)"
  assertContains "$(cat maxresults_err.txt)" "maximum number of results exceeded"
}

testMaxResultsExactLimitSucceeds() {
  output=$(./yq eval --max-results 5 '.[]' maxresults_f1.yml 2>maxresults_err.txt)
  rc=$?
  assertEquals 0 "$rc"
  assertEquals "$(printf 'a\nb\nc\nd\ne')" "$output"
}

testMaxResultsUnspecifiedIsUnchanged() {
  output=$(./yq eval '.[]' maxresults_f1.yml 2>maxresults_err.txt)
  rc=$?
  assertEquals 0 "$rc"
  assertEquals "$(printf 'a\nb\nc\nd\ne')" "$output"
}

testMaxResultsDeterministic() {
  printf -- '- a\n- b\n' > maxresults_f1.yml
  ./yq eval --max-results 3 '.[]' maxresults_f1.yml maxresults_f2.yml >maxresults_out.txt 2>&1 || true
  first=$(cat maxresults_out.txt)
  ./yq eval --max-results 3 '.[]' maxresults_f1.yml maxresults_f2.yml >maxresults_out.txt 2>&1 || true
  second=$(cat maxresults_out.txt)
  assertEquals "$first" "$second"
}

testMaxResultsNegativeRejected() {
  output=$(./yq eval --max-results=-1 '.[]' maxresults_f1.yml 2>&1)
  assertEquals 1 $?
  assertEquals "Error: max-results must not be negative" "$output"
}

source ./scripts/shunit2
