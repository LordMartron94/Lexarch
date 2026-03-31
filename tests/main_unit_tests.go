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
- Pre-Lex - Done
- Lex On-Demand - Done
- Helpers for keeping track of columns/lines
- Multiple lexer states - Done
- Clear disambiguation (longest then priority) - Done
- Consume vs. Peek - Done
- Snapshot & Restore - Done

Targets:
- At a bare minimum, 100k tokens per second
- Ideally 1-10m tokens per second
*/

const (
	TokIdentifier lexarch.TokenKind = iota + 2
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

	rules := []*lexarch.LexingRule{
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

type LexerLexResult struct {
	Tokens      []lexarch.Token
	LexingError error
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

	lexRunner := func(input string) LexerLexResult {
		return runLexerSessionToEnd(sharedLexer, input, false)
	}

	// ---------------------------------------------------------
	// Boundary Conditions
	// ---------------------------------------------------------
	boundaryAtom := shield.AtomCreate(0, "Boundary Conditions", lexRunner)

	emptyCase := shield.CaseCreate("empty_string", "", func(output LexerLexResult) shield.AtomResult {
		if output.LexingError != nil {
			return *shield.AtomResultFailureCreate("empty string produced unexpected error")
		}
		if len(output.Tokens) != 0 {
			return *shield.AtomResultFailureCreate(fmt.Sprintf("expected 0 tokens, got %d", len(output.Tokens)))
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

		testCase := shield.CaseCreate(tc.name, cleanInput, func(output LexerLexResult) shield.AtomResult {
			if output.LexingError == nil {
				return *shield.AtomResultFailureCreate("expected lexing error, but got nil")
			}

			lexErr, ok := output.LexingError.(*lexarch.LexerRuntimeError)
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
	// Error Recovery (Continue After Invalid Character)
	// ---------------------------------------------------------
	type ContinuingLexResult struct {
		Tokens      []lexarch.Token
		Errors      []error
		ReachedEOF  bool
		LexingError error
	}

	continueAfterErrorRunner := func(input string) ContinuingLexResult {
		session := lexarch.LexerLexingSessionCreate(sharedLexer, input, 0)
		out := lexarch.LexingSessionNextResultCreate()
		result := ContinuingLexResult{}

		// Safety guard against infinite loops in recovery behavior.
		for i := 0; i < 128; i++ {
			lexarch.LexingSessionConsume(session, out)

			if out.Token != nil && out.Token.Kind == lexarch.TokenKindEOF {
				result.ReachedEOF = true
				break
			}
			if out.Token != nil {
				result.Tokens = append(result.Tokens, *out.Token)
			}
			if out.LexingError != nil {
				result.Errors = append(result.Errors, out.LexingError)
			}
		}

		if !result.ReachedEOF {
			result.LexingError = fmt.Errorf("did not reach EOF while testing continue-after-error behavior")
		}

		return result
	}

	continueAfterErrorAtom := shield.AtomCreate(2, "Continue After Invalid Character", continueAfterErrorRunner)
	continueCase := shield.CaseCreate("continues_after_invalid_character", "let # x", func(output ContinuingLexResult) shield.AtomResult {
		if output.LexingError != nil {
			return *shield.AtomResultFailureCreate(output.LexingError.Error())
		}
		if !output.ReachedEOF {
			return *shield.AtomResultFailureCreate("expected EOF after recovery flow")
		}
		if len(output.Errors) != 1 {
			return *shield.AtomResultFailureCreate(fmt.Sprintf("expected exactly 1 lexing error, got %d", len(output.Errors)))
		}

		lexErr, ok := output.Errors[0].(*lexarch.LexerRuntimeError)
		if !ok {
			return *shield.AtomResultFailureCreate(fmt.Sprintf("expected LexerRuntimeError, got %T", output.Errors[0]))
		}
		span := lexErr.Span()
		if span.Offset != 4 || span.Length != 1 {
			return *shield.AtomResultFailureCreate(fmt.Sprintf("expected invalid char span [4:1], got [%d:%d]", span.Offset, span.Length))
		}

		expected := []TokenSpec{
			{kind: TokIdentifier, text: "let"},
			{kind: TokWhitespace, text: " "},
			{kind: lexarch.TokenKindError, role: lexarch.TokenRoleSentinel, text: "#"},
			{kind: TokWhitespace, text: " "},
			{kind: TokIdentifier, text: "x"},
		}
		return *assertTokenSpecs(output.Tokens, expected)
	})
	shield.CaseSetDescription(continueCase, "validates that lexer reports invalid character and still continues lexing")
	shield.AtomRegisterCase(continueAfterErrorAtom, continueCase)
	shield.UnitRegisterAtom(mainUnit, continueAfterErrorAtom)

	// ---------------------------------------------------------
	// Valid Token Sequences (Lazy)
	// ---------------------------------------------------------
	validTokenAtomLazy := shield.AtomCreate(3, "Valid Token Sequences (Lazy)", lexRunner)

	// Create a second runner specifically for testing the prefill cache
	prefillLexRunner := func(input string) LexerLexResult {
		return runLexerSessionToEnd(sharedLexer, input, true)
	}

	validTokenAtomPrefill := shield.AtomCreate(4, "Valid Token Sequences (Prefilled)", prefillLexRunner)

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

		input := tokenSpecsToInput(tc.specs)

		// 1. Register for Lazy Evaluation
		lazyCase := shield.CaseCreate(tc.name, input, func(output LexerLexResult) shield.AtomResult {
			if output.LexingError != nil {
				return *shield.AtomResultFailureCreate(fmt.Sprintf("unexpected lexing error: %v", output.LexingError))
			}
			return *assertTokenSpecs(output.Tokens, tc.specs)
		})
		shield.CaseSetDescription(lazyCase, fmt.Sprintf("validates sequence for synthesized input %q (lazy)", input))
		shield.AtomRegisterCase(validTokenAtomLazy, lazyCase)

		// 2. Register for Prefilled Evaluation
		prefillCase := shield.CaseCreate(tc.name, input, func(output LexerLexResult) shield.AtomResult {
			if output.LexingError != nil {
				return *shield.AtomResultFailureCreate(fmt.Sprintf("unexpected lexing error: %v", output.LexingError))
			}
			return *assertTokenSpecs(output.Tokens, tc.specs)
		})
		shield.CaseSetDescription(prefillCase, fmt.Sprintf("validates sequence for synthesized input %q (prefilled)", input))
		shield.AtomRegisterCase(validTokenAtomPrefill, prefillCase)
	}

	shield.UnitRegisterAtom(mainUnit, validTokenAtomLazy)
	shield.UnitRegisterAtom(mainUnit, validTokenAtomPrefill)

	// ---------------------------------------------------------
	// BYTE SPAN -> POSITION MAPPING
	// ---------------------------------------------------------
	type PositionMappingCase struct {
		span     lexarch.ByteSpan
		source   string
		expected lexarch.LexingPosition
	}

	positionRunner := func(tc PositionMappingCase) lexarch.LexingPosition {
		return lexarch.LexerByteSpanToPosition(tc.span, tc.source, 4)
	}

	positionAtom := shield.AtomCreate(5, "Byte Span To Position", positionRunner)

	positionCases := []struct {
		name string
		tc   PositionMappingCase
	}{
		{
			name: "single_line_ascii",
			tc: PositionMappingCase{
				span:   lexarch.ByteSpan{Offset: 4, Length: 3}, // "def"
				source: "abc def",
				expected: lexarch.LexingPosition{
					StartLine:   1,
					StartColumn: 5,
					EndLine:     1,
					EndColumn:   8,
				},
			},
		},
		{
			name: "zero_length_at_start",
			tc: PositionMappingCase{
				span:   lexarch.ByteSpan{Offset: 0, Length: 0},
				source: "abc",
				expected: lexarch.LexingPosition{
					StartLine:   1,
					StartColumn: 1,
					EndLine:     1,
					EndColumn:   1,
				},
			},
		},
		{
			name: "cross_line_span",
			tc: PositionMappingCase{
				span:   lexarch.ByteSpan{Offset: 1, Length: 4}, // "b\ncd"
				source: "ab\ncd\nef",
				expected: lexarch.LexingPosition{
					StartLine:   1,
					StartColumn: 2,
					EndLine:     2,
					EndColumn:   3,
				},
			},
		},
		{
			name: "utf8_rune_column_tracking",
			tc: PositionMappingCase{
				span:   lexarch.ByteSpan{Offset: 4, Length: 2}, // "β" (2 bytes)
				source: "aé\nβz",
				expected: lexarch.LexingPosition{
					StartLine:   2,
					StartColumn: 1,
					EndLine:     2,
					EndColumn:   2,
				},
			},
		},
	}

	for _, c := range positionCases {
		c := c
		testCase := shield.CaseCreate(c.name, c.tc, func(output lexarch.LexingPosition) shield.AtomResult {
			if output.StartLine != c.tc.expected.StartLine ||
				output.StartColumn != c.tc.expected.StartColumn ||
				output.EndLine != c.tc.expected.EndLine ||
				output.EndColumn != c.tc.expected.EndColumn {
				return *shield.AtomResultFailureCreate(fmt.Sprintf(
					"position mismatch: expected [%d:%d -> %d:%d], got [%d:%d -> %d:%d]",
					c.tc.expected.StartLine, c.tc.expected.StartColumn,
					c.tc.expected.EndLine, c.tc.expected.EndColumn,
					output.StartLine, output.StartColumn,
					output.EndLine, output.EndColumn,
				))
			}

			return *shield.AtomResultSuccessCreate()
		})

		shield.CaseSetDescription(testCase, "validates byte span to line/column conversion")
		shield.AtomRegisterCase(positionAtom, testCase)
	}

	shield.UnitRegisterAtom(mainUnit, positionAtom)

	// ---------------------------------------------------------
	// SESSION RESET REUSE
	// ---------------------------------------------------------
	type SessionResetResult struct {
		BeforeReset []lexarch.Token
		AfterReset  []lexarch.Token
		LexingError error
	}

	resetRunner := func(_ string) SessionResetResult {
		session := lexarch.LexerLexingSessionCreate(sharedLexer, "let x = 10;", 0)
		out := lexarch.LexingSessionNextResultCreate()
		result := SessionResetResult{}

		consumeAll := func(target *[]lexarch.Token) error {
			for i := 0; i < 128; i++ {
				lexarch.LexingSessionConsume(session, out)
				if out.LexingError != nil {
					return out.LexingError
				}
				if out.Token == nil {
					return fmt.Errorf("expected token, got nil")
				}
				if out.Token.Kind == lexarch.TokenKindEOF {
					return nil
				}
				*target = append(*target, *out.Token)
			}
			return fmt.Errorf("consume guard tripped while reading token stream")
		}

		if err := consumeAll(&result.BeforeReset); err != nil {
			result.LexingError = err
			return result
		}

		lexarch.LexerLexingSessionReset(sharedLexer, session, "a+b", 0)

		if err := consumeAll(&result.AfterReset); err != nil {
			result.LexingError = err
			return result
		}

		return result
	}

	resetAtom := shield.AtomCreate(6, "Session Reset Reuse", resetRunner)
	resetCase := shield.CaseCreate("reuses_session_after_reset", "", func(output SessionResetResult) shield.AtomResult {
		if output.LexingError != nil {
			return *shield.AtomResultFailureCreate(output.LexingError.Error())
		}

		beforeExpected := []TokenSpec{
			{kind: TokIdentifier, text: "let"},
			{kind: TokWhitespace, text: " "},
			{kind: TokIdentifier, text: "x"},
			{kind: TokWhitespace, text: " "},
			{kind: TokOperator, text: "="},
			{kind: TokWhitespace, text: " "},
			{kind: TokInteger, text: "10"},
			{kind: TokPunctuation, text: ";"},
		}
		afterExpected := []TokenSpec{
			{kind: TokIdentifier, text: "a"},
			{kind: TokOperator, text: "+"},
			{kind: TokIdentifier, text: "b"},
		}

		if len(output.BeforeReset) != len(beforeExpected) {
			return *shield.AtomResultFailureCreate(fmt.Sprintf("before reset token count mismatch: expected %d, got %d", len(beforeExpected), len(output.BeforeReset)))
		}
		for i, spec := range beforeExpected {
			tok := output.BeforeReset[i]
			if tok.Kind != spec.kind {
				return *shield.AtomResultFailureCreate(fmt.Sprintf("before reset token[%d] kind mismatch: expected %d, got %d", i, spec.kind, tok.Kind))
			}
		}

		if len(output.AfterReset) != len(afterExpected) {
			return *shield.AtomResultFailureCreate(fmt.Sprintf("after reset token count mismatch: expected %d, got %d", len(afterExpected), len(output.AfterReset)))
		}
		for i, spec := range afterExpected {
			tok := output.AfterReset[i]
			if tok.Kind != spec.kind {
				return *shield.AtomResultFailureCreate(fmt.Sprintf("after reset token[%d] kind mismatch: expected %d, got %d", i, spec.kind, tok.Kind))
			}
		}

		return *shield.AtomResultSuccessCreate()
	})
	shield.CaseSetDescription(resetCase, "validates session can be reset and reused for a new input stream")
	shield.AtomRegisterCase(resetAtom, resetCase)
	shield.UnitRegisterAtom(mainUnit, resetAtom)

	return []shield.Unit{*mainUnit}
}
