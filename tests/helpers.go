package tests

import (
	"fmt"
	"lexarch"
	"shield"
	"strings"
)

const errMarker = "‸" // U+2038 CARET, highly unlikely to appear in actual code

func parseMarkedInput(marked string) (clean string, offset uint32) {
	idx := strings.Index(marked, errMarker)
	if idx == -1 {
		panic(fmt.Sprintf("Test setup flaw: missing marker '%s' in %q", errMarker, marked))
	}

	clean = strings.Replace(marked, errMarker, "", 1)
	return clean, uint32(idx)
}

type TokenSpec struct {
	kind lexarch.TokenKind
	role lexarch.TokenRole
	text string
}

func assertTokenSpecs(actual []lexarch.Token, specs []TokenSpec) *shield.AtomResult {
	if len(actual) != len(specs) {
		return shield.AtomResultFailureCreate(fmt.Sprintf(
			"token count mismatch: expected %d, got %d", len(specs), len(actual),
		))
	}

	currentOffset := uint32(0)
	for i, spec := range specs {
		act := actual[i]
		expectedLength := uint32(len(spec.text)) // len() natively counts bytes in Go

		if act.Kind != spec.kind {
			return shield.AtomResultFailureCreate(fmt.Sprintf(
				"token[%d] kind mismatch: expected %d, got %d", i, spec.kind, act.Kind,
			))
		}
		if act.Role != spec.role {
			return shield.AtomResultFailureCreate(fmt.Sprintf(
				"token[%d] role mismatch: expected %d, got %d", i, spec.role, act.Role,
			))
		}
		if act.Span.Offset != currentOffset || act.Span.Length != expectedLength {
			return shield.AtomResultFailureCreate(fmt.Sprintf(
				"token[%d] span mismatch: expected [%d:%d], got [%d:%d]",
				i, currentOffset, expectedLength, act.Span.Offset, act.Span.Length,
			))
		}

		currentOffset += expectedLength
	}

	return shield.AtomResultSuccessCreate()
}
