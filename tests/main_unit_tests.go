package tests

import (
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

	falseCase := shield.CaseCreate("false_input", "hello", func(output lexarch.LexerLexResult) shield.AtomResult {
		if output.LexingError == nil {
			return *shield.AtomResultFailureCreate("invalid input did not produce error")
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
