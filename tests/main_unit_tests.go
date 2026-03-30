package tests

import (
	"fmt"
	"lexarch"
	"shield"
)

/*
Features I want to support:
- Pre-Lex
- Lex On-Demand
- Helpers for keeping track of columns/lines
- Multiple lexer states
- Clear disambiguation (longest then priority)
- Consume vs. Peek
- Snapshot & Restore

Targets:
- At a bare minimum, 100k tokens per second
- Ideally 1-10m tokens per second
*/

func GetMainLexerUnits(order int) []shield.Unit {
	mainUnit := shield.UnitCreate(order, "Lexer Comprehensive")

	stringAtom := shield.AtomCreate(0, "Strings", lexRunner)
	shield.AtomRegisterCase(stringAtom, shield.CaseCreate("empty_string", "", func(output lexarch.LexerLexResult) shield.AtomResult {
		numTokens := len(output.Tokens)

		if output.LexingError != nil {
			return *shield.AtomResultFailureCreate("empty string produced error")
		}

		if numTokens != 0 {
			return *shield.AtomResultFailureCreate("empty string produced tokens")
		}

		if !output.EOF {
			return *shield.AtomResultFailureCreate("empty string had no eof")
		}

		return *shield.AtomResultSuccessCreate()
	}))

	falseCaseInput := "hello"
	falseCase := shield.CaseCreate("false_input", falseCaseInput, func(output lexarch.LexerLexResult) shield.AtomResult {
		if output.LexingError == nil {
			return *shield.AtomResultFailureCreate("invalid input did not produce error")
		}

		lexErr, ok := output.LexingError.(*lexarch.LexerError)
		if !ok {
			return *shield.AtomResultFailureCreate(fmt.Sprintf("invalid input produced wrong error type: %T", output.LexingError))
		}

		span := lexErr.Span()
		expectedOffset := uint32(0) // 'h' is at index 0
		expectedLength := uint32(1) // 'h' is 1 byte long

		if span.Offset != expectedOffset || span.Length != expectedLength {
			return *shield.AtomResultFailureCreate(fmt.Sprintf(
				"span mismatch: expected [%d:%d], got [%d:%d]",
				expectedOffset, expectedLength, span.Offset, span.Length,
			))
		}

		return *shield.AtomResultSuccessCreate()
	})
	shield.CaseSetDescription(falseCase, "enforces invalid input results in a proper error")

	shield.AtomRegisterCase(stringAtom, falseCase)

	shield.UnitRegisterAtom(mainUnit, stringAtom)

	return []shield.Unit{
		*mainUnit,
	}
}

func lexRunner(input string) lexarch.LexerLexResult {
	lexer := lexarch.LexerCreate()
	return lexarch.LexerLexContent(lexer, input)
}
