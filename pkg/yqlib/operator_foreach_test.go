package yqlib

import "testing"

var foreachOperatorScenarios = []expressionScenario{
	{
		description: "Rolling state, output the state each step",
		document:    `[10, 2, 5, 3]`,
		expression:  `.[] as $item foreach (0; . + $item)`,
		expected: []string{
			"D0, P[], (!!int)::10\n",
			"D0, P[], (!!int)::12\n",
			"D0, P[], (!!int)::17\n",
			"D0, P[], (!!int)::20\n",
		},
	},
	{
		description:    "Use an extract expression to output something different to the state",
		subdescription: "The third expression is evaluated against the updated accumulator each step.",
		document:       `[10, 2, 5, 3]`,
		expression:     `.[] as $item foreach (0; . + $item; . * 2)`,
		expected: []string{
			"D0, P[], (!!int)::20\n",
			"D0, P[], (!!int)::24\n",
			"D0, P[], (!!int)::34\n",
			"D0, P[], (!!int)::40\n",
		},
	},
	{
		description: "Empty collection produces no output",
		document:    `[]`,
		expression:  `.[] as $item foreach (0; . + $item)`,
		expected:    []string{},
	},
	{
		description: "Single value input",
		document:    `5`,
		expression:  `. as $item foreach (0; . + $item)`,
		expected: []string{
			"D0, P[], (!!int)::5\n",
		},
	},
	{
		description: "Any multi-node expression can be used as the input",
		expression:  `(1, 2, 3) as $item foreach (0; . + $item)`,
		expected: []string{
			"D0, P[], (!!int)::1\n",
			"D0, P[], (!!int)::3\n",
			"D0, P[], (!!int)::6\n",
		},
	},
	{
		skipDoc:     true,
		description: "Recursive descent input keeps traversal order",
		document:    "a:\n  b: 1\nc: 2\n",
		expression:  `(.. | select(tag == "!!int")) as $item foreach (0; . + $item)`,
		expected: []string{
			"D0, P[], (!!int)::1\n",
			"D0, P[], (!!int)::3\n",
		},
	},
	{
		skipDoc:     true,
		description: "foreach after a pipe keeps its own state per pipeline input (like jq)",
		document:    "a:\n  b: 1\nc: 2\n",
		expression:  `.. | select(tag == "!!int") as $item foreach (0; . + $item)`,
		expected: []string{
			"D0, P[], (!!int)::1\n",
			"D0, P[], (!!int)::2\n",
		},
	},
	{
		description: "eval-all shares one rolling state across all documents",
		subdescription: "When run with eval-all, the documents are processed together in file and document order, " +
			"with a single accumulator. With eval (not shown), the state is reset for each document.",
		document:   `x: 1`,
		document2:  `x: 2`,
		expression: `.x as $item foreach (0; . + $item)`,
		expected: []string{
			"D0, P[], (!!int)::1\n",
			"D0, P[], (!!int)::3\n",
		},
	},
	{
		skipDoc:     true,
		description: "Extract can produce multiple outputs per step",
		document:    `[1, 2]`,
		expression:  `.[] as $item foreach (0; . + $item; (., . * 10))`,
		expected: []string{
			"D0, P[], (!!int)::1\n",
			"D0, P[], (!!int)::10\n",
			"D0, P[], (!!int)::3\n",
			"D0, P[], (!!int)::30\n",
		},
	},
	{
		skipDoc:     true,
		description: "Collect the per-step outputs into an array",
		document:    `[1, 2, 3]`,
		expression:  `[.[] as $item foreach (0; . + $item)]`,
		expected: []string{
			"D0, P[], (!!seq)::- 1\n- 3\n- 6\n",
		},
	},
	{
		skipDoc:     true,
		description: "Nested foreach with outer variable capture",
		document:    `[[1, 2], [3, 4]]`,
		expression:  `[.[] as $outer foreach ([]; . + [($outer[] as $inner foreach (0; . + $inner))])]`,
		expected: []string{
			"D0, P[], (!!seq)::- - 1\n  - 3\n- - 1\n  - 3\n  - 3\n  - 7\n",
		},
	},
	{
		skipDoc:     true,
		description: "Variables from outside the foreach are captured",
		document:    `[1, 2]`,
		expression:  `100 as $base | .[] as $item foreach (0; . + $item + $base)`,
		expected: []string{
			"D0, P[], (!!int)::101\n",
			"D0, P[], (!!int)::203\n",
		},
	},
	{
		description: "Build up an object, each step emits the current state",
		subdescription: "The update mutates the accumulator in place; foreach takes a copy each step " +
			"so the earlier outputs are not affected by later updates.",
		document:   `[{name: Cathy, has: apples},{name: Bob, has: bananas}]`,
		expression: `.[] as $item foreach ({}; .[$item.name] = $item.has)`,
		expected: []string{
			"D0, P[], (!!map)::Cathy: apples\n",
			"D0, P[], (!!map)::Cathy: apples\nBob: bananas\n",
		},
	},
	{
		description: "Comments and styles are preserved in each output",
		document:    "- cat # meow\n- dog # woof\n",
		expression:  `.[] as $item foreach ([]; . + [$item])`,
		expected: []string{
			"D0, P[], (!!seq)::- cat # meow\n",
			"D0, P[], (!!seq)::- cat # meow\n- dog # woof\n",
		},
	},
	{
		skipDoc:     true,
		description: "Pipeline results get their own state each, like jq",
		document:    `["a=1", "b=2"]`,
		expression:  `.[] | match("(.)=(.)") | .captures[] as $capture foreach (""; . + $capture.string)`,
		expected: []string{
			"D0, P[], (!!str)::a\n",
			"D0, P[], (!!str)::a1\n",
			"D0, P[], (!!str)::b\n",
			"D0, P[], (!!str)::b2\n",
		},
	},
	{
		skipDoc:       true,
		description:   "Init expression error returns an error, even when the collection is empty",
		document:      `[]`,
		expression:    `.[] as $item foreach (error("init boom"); . + $item)`,
		expectedError: "init boom",
	},
	{
		skipDoc:       true,
		description:   "Update expression error returns an error with no output",
		document:      `[1, 2, 3]`,
		expression:    `.[] as $item foreach (0; if $item == 2 then error("update boom") else . + $item end)`,
		expectedError: "update boom",
	},
	{
		skipDoc:       true,
		description:   "Extract expression error returns an error with no output",
		document:      `[1, 2, 3]`,
		expression:    `.[] as $item foreach (0; . + $item; if $item == 2 then error("extract boom") else . end)`,
		expectedError: "extract boom",
	},
	{
		skipDoc:       true,
		description:   "Input expression error returns an error",
		document:      `[1]`,
		expression:    `(.[] | error("input boom")) as $item foreach (0; . + $item)`,
		expectedError: "input boom",
	},
	{
		skipDoc:       true,
		description:   "foreach must be given a variables assignment",
		document:      `[1]`,
		expression:    `.[] foreach (0; .)`,
		expectedError: "foreach must be given a variables assignment, got TRAVERSE_ARRAY instead",
	},
	{
		skipDoc:       true,
		description:   "foreach must be given a block",
		document:      `[1]`,
		expression:    `.[] as $item foreach 0`,
		expectedError: "foreach must be given a block, got VALUE instead",
	},
	{
		skipDoc:       true,
		description:   "foreach only takes up to three block expressions",
		document:      `[1]`,
		expression:    `.[] as $item foreach (0; .; .; .)`,
		expectedError: "foreach only takes an init, update and optional extract expression, got too many block expressions",
	},
}

func TestForeachOperatorScenarios(t *testing.T) {
	for _, tt := range foreachOperatorScenarios {
		testScenario(t, &tt)
	}
	documentOperatorScenarios(t, "foreach", foreachOperatorScenarios)
}
