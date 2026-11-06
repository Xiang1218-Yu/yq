package yqlib

import (
	"container/list"
	"fmt"
)

func foreachOperator(d *dataTreeNavigator, context Context, expressionNode *ExpressionNode) (Context, error) {
	log.Debugf("foreachOp")
	// .a as $var foreach (0; . + $var; .)
	// lhs is the assignment operator
	// rhs is the foreach block: (init; update) or (init; update; extract)
	// '.' refers to the current accumulator, initialised by init.
	// $var references a single element from the input expression.

	if expressionNode.LHS.Operation.OperationType != assignVariableOpType {
		return Context{}, fmt.Errorf("foreach must be given a variables assignment, got %v instead", expressionNode.LHS.Operation.OperationType.Type)
	} else if expressionNode.RHS.Operation.OperationType != blockOpType {
		return Context{}, fmt.Errorf("foreach must be given a block, got %v instead", expressionNode.RHS.Operation.OperationType.Type)
	}

	initExp, updateExp, extractExp, err := getForeachBlockExpressions(expressionNode)
	if err != nil {
		return Context{}, err
	}

	assignExp := expressionNode.LHS

	// Same EvaluateTogether split as `ireduce` / `as` / collect / crossFunction:
	// file documents share one rolling state (eval-all), while pipeline
	// results such as match(...) get their own state per input, like jq.
	var evaluateAllTogether = true
	for matchEl := context.MatchingNodes.Front(); matchEl != nil; matchEl = matchEl.Next() {
		evaluateAllTogether = evaluateAllTogether && matchEl.Value.(*CandidateNode).EvaluateTogether
		if !evaluateAllTogether {
			break
		}
	}
	if evaluateAllTogether {
		return foreachOperatorSingle(d, context, assignExp, initExp, updateExp, extractExp)
	}

	results := list.New()
	for el := context.MatchingNodes.Front(); el != nil; el = el.Next() {
		result, err := foreachOperatorSingle(d, context.SingleChildContext(el.Value.(*CandidateNode)), assignExp, initExp, updateExp, extractExp)
		if err != nil {
			return Context{}, err
		}
		results.PushBackList(result.MatchingNodes)
	}
	return context.ChildContext(results), nil
}

// blocks are right associative, so (init; update; extract) parses as
// BLOCK(init, BLOCK(update, extract)). The two part form (init; update)
// parses as BLOCK(init, update) and defaults the extract expression to '.'.
func getForeachBlockExpressions(expressionNode *ExpressionNode) (*ExpressionNode, *ExpressionNode, *ExpressionNode, error) {
	block := expressionNode.RHS
	initExp := block.LHS

	if block.RHS.Operation.OperationType != blockOpType {
		return initExp, block.RHS, nil, nil
	}

	updateExp := block.RHS.LHS
	extractExp := block.RHS.RHS
	if extractExp.Operation.OperationType == blockOpType {
		return nil, nil, nil, fmt.Errorf("foreach only takes an init, update and optional extract expression, got too many block expressions")
	}

	return initExp, updateExp, extractExp, nil
}

func foreachOperatorSingle(d *dataTreeNavigator, context Context, assignExp *ExpressionNode, initExp *ExpressionNode, updateExp *ExpressionNode, extractExp *ExpressionNode) (Context, error) {
	// When all the file documents are processed together (eval-all), the
	// init expression is evaluated once against the first document so a
	// single rolling state is shared across every input, regardless of the
	// number of documents.
	initContext := context
	if context.MatchingNodes.Len() > 1 {
		initContext = context.SingleChildContext(context.MatchingNodes.Front().Value.(*CandidateNode))
	}

	// the init expression is evaluated before the collection, matching jq:
	// init errors are returned even when the collection is empty.
	accum, err := d.GetMatchingNodes(initContext, initExp)
	if err != nil {
		return Context{}, err
	}

	// evaluate the collection of items read only, so iterating over a
	// missing path does not autovivify it (same as the `as` operator).
	collection, err := d.GetMatchingNodes(context.ReadOnlyClone(), assignExp.LHS)
	if err != nil {
		return Context{}, err
	}

	variableName := assignExp.RHS.Operation.StringValue

	log.Debugf("foreach with variable %v", variableName)

	results := list.New()

	for el := collection.MatchingNodes.Front(); el != nil; el = el.Next() {
		candidate := el.Value.(*CandidateNode)
		log.Debugf("FOREACHING WITH %v", NodeToString(candidate))
		varValue := list.New()
		varValue.PushBack(candidate)
		accum.SetVariable(variableName, varValue)

		accum, err = d.GetMatchingNodes(accum, updateExp)
		if err != nil {
			return Context{}, err
		}

		var extracted *list.List
		if extractExp == nil {
			extracted = accum.MatchingNodes
		} else {
			extractContext, err := d.GetMatchingNodes(accum, extractExp)
			if err != nil {
				return Context{}, err
			}
			extracted = extractContext.MatchingNodes
		}

		for extractEl := extracted.Front(); extractEl != nil; extractEl = extractEl.Next() {
			// take a copy of each result, otherwise later update
			// expressions that mutate the accumulator in place would
			// change the results already emitted.
			results.PushBack(extractEl.Value.(*CandidateNode).Copy())
		}
	}

	return context.ChildContext(results), nil
}
