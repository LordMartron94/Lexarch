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

	emptyStringAtom := shield.AtomCreate(0, "EmptyString", lexRunner)
	shield.AtomRegisterCase(emptyStringAtom, shield.CaseCreate("empty_string", "", func(output lexarch.LexerLexResult) shield.AtomResult {
		numTokens := len(output.Tokens)
		if output.EOF && numTokens == 0 {
			return *shield.AtomResultSuccessCreate()
		}

		if output.EOF && numTokens != 0 {
			return *shield.AtomResultFailureCreate("empty string had eof but produced tokens")
		}

		if numTokens != 0 {
			return *shield.AtomResultFailureCreate("empty string produced tokens")
		}

		if !output.EOF {
			return *shield.AtomResultFailureCreate("empty string had no eof")
		}

		return *shield.AtomResultFailureCreate("unknown failure")
	}))
	shield.UnitRegisterAtom(mainUnit, emptyStringAtom)

	return []shield.Unit{
		*mainUnit,
	}
}

func lexRunner(input string) lexarch.LexerLexResult {
	lexer := lexarch.LexerCreate()
	return lexarch.LexerLexContent(lexer, input)
}
