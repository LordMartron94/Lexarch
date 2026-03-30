package tests

import (
	"autarch/pattern"
	"fmt"
	"foundation/domain"
	"lexarch"
	"shield"
)

// Mocking the rule definitions for the test.
const (
	TokIdentifier lexarch.TokenKind = iota + 1
	TokInteger
	TokWhitespace
	TokOperator
	TokPunctuation
)

func setupTestLexer() *lexarch.Lexer {
	cfg := lexarch.LexerConfigurationCreate()

	// 1. Initialize Regula pattern infrastructure
	obsDomain := domain.DiscreteDomainRuneCreate()
	factory := pattern.RegulaASTFactoryCreate(obsDomain)
	templates := pattern.RegulaTemplatesCreate(factory)

	// 2. Define the baseline grammar using STRICT factories
	rules := []lexarch.LexingRule{
		lexarch.LexingRuleCreate(
			templates.Whitespace().Plus(),
			1, // Priority
			TokWhitespace,
			0, // Role
		),
		lexarch.LexingRuleCreate(
			templates.Identifier(), // Matches 'let', 'var', 'x', 'y', 'validToken'
			2,
			TokIdentifier,
			0,
		),
		lexarch.LexingRuleCreate(
			templates.Integer(), // Matches '10', '5'
			2,
			TokInteger,
			0,
		),
		lexarch.LexingRuleCreate(
			factory.AnyOf(
				pattern.LiteralString(factory, "="),
				pattern.LiteralString(factory, "+"),
				pattern.LiteralString(factory, "*"),
			),
			3,
			TokOperator,
			0,
		),
		lexarch.LexingRuleCreate(
			pattern.LiteralString(factory, ";"),
			3,
			TokPunctuation,
			0,
		),
	}

	// 3. Register state using the factory
	state := lexarch.LexingStateCreate("INITIAL", rules)
	lexarch.LexerConfigurationRegisterState(cfg, state, true)

	return lexarch.LexerCreate(cfg)
}

func GetMainLexerUnits(order int) []shield.Unit {
	mainUnit := shield.UnitCreate(order, "Lexer Comprehensive")

	var sharedLexer *lexarch.Lexer
	shield.UnitSetSetupAndTeardown(mainUnit,
		func() { sharedLexer = setupTestLexer() },
		func() {
			if sharedLexer != nil {
				lexarch.LexerDestroy(sharedLexer)
				sharedLexer = nil
			}
		},
	)

	lexRunner := func(input string) lexarch.LexerLexResult {
		return lexarch.LexerLexContentFull(sharedLexer, input)
	}

	// ---------------------------------------------------------
	// ATOM 1: Boundary Conditions
	// ---------------------------------------------------------
	boundaryAtom := shield.AtomCreate(0, "Boundary Conditions", lexRunner)

	emptyCase := shield.CaseCreate("empty_string", "", func(output lexarch.LexerLexResult) shield.AtomResult {
		if output.LexingError != nil {
			return *shield.AtomResultFailureCreate("empty string produced unexpected error")
		}
		if len(output.Tokens) != 0 {
			return *shield.AtomResultFailureCreate(fmt.Sprintf("expected 0 tokens, got %d", len(output.Tokens)))
		}
		if !output.EOF {
			return *shield.AtomResultFailureCreate("empty string failed to flag EOF")
		}
		return *shield.AtomResultSuccessCreate()
	})
	shield.CaseSetDescription(emptyCase, "ensures lexer handles zero-byte input cleanly")
	shield.AtomRegisterCase(boundaryAtom, emptyCase)
	shield.UnitRegisterAtom(mainUnit, boundaryAtom)

	// ---------------------------------------------------------
	// ATOM 2: Invalid Character Handling
	// ---------------------------------------------------------
	errorAtom := shield.AtomCreate(1, "Invalid Character Handling", lexRunner)

	tests := []struct {
		name   string
		marked string
		length uint32
	}{
		{"start_of_file", "‸$", 1},
		{"after_identifier", "let x = ‸#;", 1},
		{"inside_expression", "var y = 10 + ‸@ * 5;", 1},
		{"trailing_garbage", "validToken ‸\\", 1},
	}

	for _, tc := range tests {
		tc := tc
		cleanInput, expectedOffset := parseMarkedInput(tc.marked)

		testCase := shield.CaseCreate(tc.name, cleanInput, func(output lexarch.LexerLexResult) shield.AtomResult {
			if output.LexingError == nil {
				return *shield.AtomResultFailureCreate("expected lexing error, but got nil")
			}

			lexErr, ok := output.LexingError.(*lexarch.LexerError)
			if !ok {
				return *shield.AtomResultFailureCreate(fmt.Sprintf("wrong error type: %T", output.LexingError))
			}

			span := lexErr.Span()
			if span.Offset != expectedOffset || span.Length != tc.length {
				return *shield.AtomResultFailureCreate(fmt.Sprintf(
					"span mismatch: expected [%d:%d], got [%d:%d]",
					expectedOffset, tc.length, span.Offset, span.Length,
				))
			}

			return *shield.AtomResultSuccessCreate()
		})

		shield.CaseSetDescription(testCase, fmt.Sprintf("validates error offset for %q", cleanInput))
		shield.AtomRegisterCase(errorAtom, testCase)
	}
	shield.UnitRegisterAtom(mainUnit, errorAtom)

	return []shield.Unit{*mainUnit}
}
