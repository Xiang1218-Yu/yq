package yqlib

import (
	"fmt"
	"strconv"
)

// RoundTripPathString renders a node path (as returned by GetPath) in a
// deterministic yq-style notation: array indexes as [i], simple identifier
// keys as .key and anything else as ["quoted"]. An empty path renders as "".
func RoundTripPathString(parts []interface{}) string {
	var result string
	for _, part := range parts {
		switch value := part.(type) {
		case int:
			result += fmt.Sprintf("[%d]", value)
		default:
			key := fmt.Sprintf("%v", value)
			if isSimpleRoundTripKey(key) {
				result += "." + key
			} else {
				result += "[" + strconv.Quote(key) + "]"
			}
		}
	}
	return result
}

func isSimpleRoundTripKey(key string) bool {
	if key == "" {
		return false
	}
	for index, r := range key {
		if r == '_' || r == '-' {
			continue
		}
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			continue
		}
		if index > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// roundTripEffectiveTag returns a deterministic type name for a node:
// container kinds map to !!map/!!seq, scalars to their resolved YAML tag.
func roundTripEffectiveTag(node *CandidateNode) string {
	switch node.Kind {
	case MappingNode:
		return "!!map"
	case SequenceNode:
		return "!!seq"
	case AliasNode:
		return "!!alias"
	default:
		tag := node.guessTagFromCustomType()
		if tag == "" {
			return "!!str"
		}
		return tag
	}
}

// roundTripScalarRepr renders a scalar value deterministically: strings are
// double quoted, null renders as null, everything else uses its raw value.
func roundTripScalarRepr(node *CandidateNode) string {
	tag := roundTripEffectiveTag(node)
	switch tag {
	case "!!null":
		return "null"
	case "!!str":
		return strconv.Quote(node.Value)
	default:
		return node.Value
	}
}

func roundTripScalarsEqual(before *CandidateNode, after *CandidateNode) (bool, error) {
	tag := roundTripEffectiveTag(before)
	switch tag {
	case "!!int":
		_, beforeNumber, err := parseInt64(before.Value)
		if err != nil {
			return false, err
		}
		_, afterNumber, err := parseInt64(after.Value)
		if err != nil {
			return false, err
		}
		return beforeNumber == afterNumber, nil
	case "!!float":
		beforeNumber, err := strconv.ParseFloat(before.Value, 64)
		if err != nil {
			return false, err
		}
		afterNumber, err := strconv.ParseFloat(after.Value, 64)
		if err != nil {
			return false, err
		}
		return beforeNumber == afterNumber, nil
	case "!!bool":
		return isTruthyNode(before) == isTruthyNode(after), nil
	case "!!null":
		return true, nil
	default:
		return before.Value == after.Value, nil
	}
}

type roundTripComparator struct {
	document uint
	strategy RoundTripCompareStrategy
	findings []RoundTripFinding
}

// compareRoundTripTrees recursively compares two node trees and returns path
// level findings. basePath is the path of the compared roots within the
// original document (empty for whole documents).
func compareRoundTripTrees(before *CandidateNode, after *CandidateNode, document uint, basePath []interface{}, strategy RoundTripCompareStrategy) []RoundTripFinding {
	comparator := &roundTripComparator{document: document, strategy: strategy}
	comparator.compare(before, after, basePath)
	sortFindings(comparator.findings)
	return comparator.findings
}

func (c *roundTripComparator) add(finding RoundTripFinding) {
	document := c.document
	finding.Document = &document
	c.findings = append(c.findings, finding)
}

func (c *roundTripComparator) compare(before *CandidateNode, after *CandidateNode, path []interface{}) {
	beforeTag := roundTripEffectiveTag(before)
	afterTag := roundTripEffectiveTag(after)

	if before.Kind != after.Kind || beforeTag != afterTag {
		c.add(RoundTripFinding{
			Kind:   RoundTripFindingTypeChanged,
			Path:   RoundTripPathString(path),
			Before: beforeTag,
			After:  afterTag,
		})
		return
	}

	switch before.Kind {
	case MappingNode:
		c.compareMappings(before, after, path)
	case SequenceNode:
		c.compareSequences(before, after, path)
	default:
		equal, err := roundTripScalarsEqual(before, after)
		if err != nil {
			// Values that cannot be parsed are compared verbatim.
			if before.Value != after.Value {
				c.add(RoundTripFinding{
					Kind:   RoundTripFindingValueChanged,
					Path:   RoundTripPathString(path),
					Before: roundTripScalarRepr(before),
					After:  roundTripScalarRepr(after),
				})
			}
			return
		}
		if !equal {
			c.add(RoundTripFinding{
				Kind:   RoundTripFindingValueChanged,
				Path:   RoundTripPathString(path),
				Before: roundTripScalarRepr(before),
				After:  roundTripScalarRepr(after),
			})
		}
	}
}

func mappingKeyValueEntries(node *CandidateNode) ([]string, map[string]*CandidateNode) {
	keys := make([]string, 0, len(node.Content)/2)
	values := make(map[string]*CandidateNode, len(node.Content)/2)
	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index].Value
		if _, seen := values[key]; !seen {
			keys = append(keys, key)
		}
		values[key] = node.Content[index+1]
	}
	return keys, values
}

func (c *roundTripComparator) compareMappings(before *CandidateNode, after *CandidateNode, path []interface{}) {
	beforeKeys, beforeValues := mappingKeyValueEntries(before)
	afterKeys, afterValues := mappingKeyValueEntries(after)

	afterKeySet := make(map[string]bool, len(afterKeys))
	for _, key := range afterKeys {
		afterKeySet[key] = true
	}
	beforeKeySet := make(map[string]bool, len(beforeKeys))
	for _, key := range beforeKeys {
		beforeKeySet[key] = true
	}

	for _, key := range beforeKeys {
		childPath := append(append([]interface{}{}, path...), key)
		if !afterKeySet[key] {
			c.add(RoundTripFinding{
				Kind: RoundTripFindingMissing,
				Path: RoundTripPathString(childPath),
			})
			continue
		}
		c.compare(beforeValues[key], afterValues[key], childPath)
	}

	for _, key := range afterKeys {
		if !beforeKeySet[key] {
			childPath := append(append([]interface{}{}, path...), key)
			c.add(RoundTripFinding{
				Kind: RoundTripFindingAdded,
				Path: RoundTripPathString(childPath),
			})
		}
	}

	// Only report key reordering when both sides carry the same key set;
	// missing/added keys already have findings of their own.
	sameKeySet := len(beforeKeys) == len(afterKeys)
	if sameKeySet {
		for _, key := range beforeKeys {
			if !afterKeySet[key] {
				sameKeySet = false
				break
			}
		}
	}
	if sameKeySet && len(beforeKeys) > 1 {
		orderChanged := false
		for index := range beforeKeys {
			if beforeKeys[index] != afterKeys[index] {
				orderChanged = true
				break
			}
		}
		if orderChanged {
			c.add(RoundTripFinding{
				Kind:          RoundTripFindingKeyOrderChanged,
				Path:          RoundTripPathString(path),
				BeforeKeys:    append([]string{}, beforeKeys...),
				AfterKeys:     append([]string{}, afterKeys...),
				Informational: c.strategy != RoundTripCompareOrdered,
			})
		}
	}
}

func (c *roundTripComparator) compareSequences(before *CandidateNode, after *CandidateNode, path []interface{}) {
	beforeLength := len(before.Content)
	afterLength := len(after.Content)

	commonLength := beforeLength
	if afterLength < commonLength {
		commonLength = afterLength
	}

	for index := 0; index < commonLength; index++ {
		childPath := append(append([]interface{}{}, path...), index)
		c.compare(before.Content[index], after.Content[index], childPath)
	}

	for index := commonLength; index < beforeLength; index++ {
		childPath := append(append([]interface{}{}, path...), index)
		c.add(RoundTripFinding{
			Kind: RoundTripFindingMissing,
			Path: RoundTripPathString(childPath),
		})
	}

	for index := commonLength; index < afterLength; index++ {
		childPath := append(append([]interface{}{}, path...), index)
		c.add(RoundTripFinding{
			Kind: RoundTripFindingAdded,
			Path: RoundTripPathString(childPath),
		})
	}
}
