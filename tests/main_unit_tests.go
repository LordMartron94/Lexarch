package tests

import (
	"autarch/pattern"
	"fmt"
	"foundation/domain"
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

const (
	TokIdentifier lexarch.TokenKind = iota + 1
	TokInteger
	TokWhitespace
	TokOperator
	TokPunctuation
)

func setupTestLexer() *lexarch.Lexer {
	cfg := lexarch.LexerConfigurationCreate()

	obsDomain := domain.DiscreteDomainRuneCreate()
	factory := pattern.RegulaASTFactoryCreate(obsDomain)
	templates := pattern.RegulaTemplatesCreate(factory)

	rules := []lexarch.LexingRule{
		lexarch.LexingRuleCreate(
			templates.Whitespace().Plus(),
			1, // Priority
			TokWhitespace,
			0, // Role
		),
		lexarch.LexingRuleCreate(
			templates.Identifier(),
			2,
			TokIdentifier,
			0,
		),
		lexarch.LexingRuleCreate(
			templates.Integer(),
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
	// Boundary Conditions
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
	// Invalid Character Handling
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

	// ---------------------------------------------------------
	// Valid Token Sequences
	// ---------------------------------------------------------
	validTokenAtom := shield.AtomCreate(2, "Valid Token Sequences", lexRunner)

	validTests := []struct {
		name  string
		specs []TokenSpec
	}{
		{
			name: "single_identifier",
			specs: []TokenSpec{
				{kind: TokIdentifier, text: "hello"},
			},
		},
		{
			name: "variable_declaration",
			specs: []TokenSpec{
				{kind: TokIdentifier, text: "let"},
				{kind: TokWhitespace, text: " "},
				{kind: TokIdentifier, text: "x"},
				{kind: TokWhitespace, text: " "},
				{kind: TokOperator, text: "="},
				{kind: TokWhitespace, text: " "},
				{kind: TokInteger, text: "10"},
				{kind: TokPunctuation, text: ";"},
			},
		},
		{
			name: "consecutive_operators_no_space",
			specs: []TokenSpec{
				{kind: TokIdentifier, text: "a"},
				{kind: TokOperator, text: "+"},
				{kind: TokIdentifier, text: "b"},
				{kind: TokOperator, text: "*"},
				{kind: TokIdentifier, text: "c"},
			},
		},
	}

	for _, tc := range validTests {
		tc := tc

		input := ""
		for _, s := range tc.specs {
			input += s.text
		}

		testCase := shield.CaseCreate(tc.name, input, func(output lexarch.LexerLexResult) shield.AtomResult {
			if output.LexingError != nil {
				return *shield.AtomResultFailureCreate(fmt.Sprintf("unexpected lexing error: %v", output.LexingError))
			}
			return *assertTokenSpecs(output.Tokens, tc.specs)
		})

		shield.CaseSetDescription(testCase, fmt.Sprintf("validates sequence for synthesized input %q", input))
		shield.AtomRegisterCase(validTokenAtom, testCase)
	}
	shield.UnitRegisterAtom(mainUnit, validTokenAtom)

	return []shield.Unit{*mainUnit}
}
