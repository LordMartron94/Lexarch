package lexarch

import (
	"fmt"
	ftesting "foundation/testing"
	"memcore"
	"memforge"
	"testing"

	"autarch"
	"autarch/pattern"
)

// ============================================================
// Test grammar
// ============================================================

type LexerState int

const (
	NormalState LexerState = iota + 1
)

type TestToken int

const (
	WhitespaceToken TestToken = iota + 1
	WordToken
	KeywordIfToken

	ErrorToken
	EOFToken
)

// ============================================================
// Test formatter with readable token and state names
// ============================================================

func createTestFormatter() *autarch.DFADebugFormatter[rune, TokenOutcome[TestToken]] {
	// Map token values to names
	tokenNames := map[TestToken]string{
		WhitespaceToken: "WhitespaceToken",
		WordToken:       "WordToken",
		KeywordIfToken:  "KeywordIfToken",
		ErrorToken:      "ErrorToken",
		EOFToken:        "EOFToken",
	}

	// Map state values to names
	stateNames := map[LexerState]string{
		NormalState: "NormalState",
	}

	baseFormatter := LexerDebugFormatterCreateRune[LexerState, TestToken]()

	return &autarch.DFADebugFormatter[rune, TokenOutcome[TestToken]]{
		FormatSymbolName: baseFormatter.FormatSymbolName,
		FormatStateOutcome: func(outcome TokenOutcome[TestToken]) string {
			tokenName := tokenNames[outcome.Token]
			if tokenName == "" {
				tokenName = fmt.Sprintf("Token(%d)", outcome.Token)
			}
			return fmt.Sprintf("{Token: %s, Priority: %d}", tokenName, outcome.Priority)
		},
		FormatSymbolID: baseFormatter.FormatSymbolID,
		FormatStateID: func(stateID uint64) string {
			state := LexerState(stateID)
			stateName := stateNames[state]
			if stateName == "" {
				return fmt.Sprintf("%3d", stateID)
			}
			return fmt.Sprintf("%-3d", stateID) // Keep numeric for alignment, but could add name
		},
	}
}

// ============================================================
// Build lexer
// ============================================================

func buildTestLexer() (lexer *Lexer[rune, LexerState, TestToken], allocator memcore.MarkRaw) {
	allocator = memforge.DynamicLinearAllocatorCreateFunction(
		uint64(memcore.KiloByte),
		func(currentCap, neededCap uint64) uint64 {
			next := min(currentCap*2, neededCap)
			if next > uint64(memcore.GigaByte) {
				panic("test allocator overflow")
			}
			return next
		},
	)

	// Set up cleanup in case of panic - this will be overridden by successful return
	cleanupNeeded := true
	defer func() {
		if cleanupNeeded {
			// If we panic, clean up the allocator
			// The caller's recovery will handle the test failure
			memforge.DynamicLinearAllocatorDestroy(allocator)
		}
	}()

	rules := LexingRulesetCreate[rune](
		TokenResolutionStepPriority[TestToken],
	)

	lower := pattern.Class(pattern.Range('a', 'z'))

	whitespace :=
		pattern.AnyOf(
			pattern.Literal(' '),
			pattern.Literal('\n'),
			pattern.Literal('\t'),
		).Plus()

	word := lower.Plus()
	keywordIf := pattern.Literal('i', 'f')

	rules.WithRulePriority(keywordIf, KeywordIfToken, 10)
	rules.WithRule(word, WordToken)
	rules.WithRule(whitespace, WhitespaceToken)

	lexer = LexerCreate(
		map[LexerState]LexingRuleset[rune, TestToken]{
			NormalState: *rules,
		},
		ErrorToken,
		EOFToken,
		func(sizeBytes, alignment uint64) memcore.MarkRaw {
			return memforge.DynamicLinearAllocatorMallocUnsafe(
				allocator,
				sizeBytes,
				alignment,
			)
		},
		memcore.GigaByte,
	)

	// Debug the DFA for the normal state with formatter
	fmt.Println("=== DFA Debug for NormalState ===")
	formatter := createTestFormatter()
	fmt.Print(LexerDebugDFA(lexer, NormalState, formatter))
	fmt.Println("=== End DFA Debug ===")

	// Success - disable cleanup defer, caller will handle it
	cleanupNeeded = false
	return lexer, allocator
}

// ============================================================
// ENTRY
// ============================================================

func LexarchTestLexer(t *testing.T) {
	var lexer *Lexer[rune, LexerState, TestToken]
	var allocator memcore.MarkRaw
	allocatorCreated := false

	// Wrap lexer creation in panic recovery to ensure cleanup happens
	func() {
		defer func() {
			if r := recover(); r != nil {
				// Cleanup allocator if lexer creation failed
				if allocatorCreated {
					memforge.DynamicLinearAllocatorDestroy(allocator)
				}
				// Convert panic to test failure
				t.Fatalf("lexer creation panicked: %v", r)
			}
		}()
		lexer, allocator = buildTestLexer()
		allocatorCreated = true
	}()

	// Ensure cleanup happens even if tests fail
	defer func() {
		if allocatorCreated {
			memforge.DynamicLinearAllocatorDestroy(allocator)
		}
	}()
	defer LexerClose(lexer)

	testClassicPath(t, lexer)
	testStreamingPath(t, lexer)
	testPeekPath(t, lexer)
	testPriorityResolution(t, lexer)
	testErrorHandling(t, lexer)
}

// ============================================================
// Classic path with full diagnostics
// ============================================================

func testClassicPath(t *testing.T, lexer *Lexer[rune, LexerState, TestToken]) {

	input := []rune("if test\nif")

	session := LexerSessionCreate(
		NormalState,
		input,
		NewlineDetectorRune(),
	)

	expect := []TestToken{
		KeywordIfToken,
		WhitespaceToken,
		WordToken,
		WhitespaceToken,
		KeywordIfToken,
		EOFToken,
	}

	for i, expected := range expect {

		lex, err := LexerConsume(lexer, session)

		ftesting.Assert(
			err == nil,
			fmt.Sprintf("step %d: consume error: %v", i, err),
			fmt.Sprintf("step %d: consume ok", i),
			t,
		)

		if err == nil {
			ftesting.Assert(
				lex.Token == expected,
				fmt.Sprintf(
					"step %d: expected %v got %v raw=%q pos=%d:%d → %d:%d",
					i,
					expected,
					lex.Token,
					string(lex.Raw),
					lex.StartLine,
					lex.StartColumn,
					lex.EndLine,
					lex.EndColumn,
				),
				fmt.Sprintf("step %d: token correct (%v)", i, expected),
				t,
			)
		}
	}

	ftesting.Assert(
		session.currentLine == 2,
		fmt.Sprintf("newline tracking wrong, ended on line %d", session.currentLine),
		"newline tracking correct",
		t,
	)
}

// ============================================================
// Peek diagnostics
// ============================================================

func testPeekPath(t *testing.T, lexer *Lexer[rune, LexerState, TestToken]) {

	input := []rune("if")

	session := LexerSessionCreate(
		NormalState,
		input,
		NewlineDetectorRune(),
	)

	p1, _ := LexerPeek(lexer, session)
	p2, _ := LexerPeek(lexer, session)

	ftesting.Assert(
		p1.Token == KeywordIfToken,
		fmt.Sprintf("peek1 wrong: got %v raw=%q", p1.Token, string(p1.Raw)),
		"peek1 correct",
		t,
	)

	ftesting.Assert(
		p2.Token == KeywordIfToken,
		fmt.Sprintf("peek2 wrong: got %v raw=%q", p2.Token, string(p2.Raw)),
		"peek stable",
		t,
	)

	c, _ := LexerConsume(lexer, session)

	ftesting.Assert(
		c.Token == KeywordIfToken,
		fmt.Sprintf("consume after peek wrong: got %v raw=%q", c.Token, string(c.Raw)),
		"consume matches peek",
		t,
	)
}

// ============================================================
// Priority test
// ============================================================

func testPriorityResolution(t *testing.T, lexer *Lexer[rune, LexerState, TestToken]) {

	input := []rune("if")

	session := LexerSessionCreate(
		NormalState,
		input,
		NewlineDetectorRune(),
	)

	lex, _ := LexerConsume(lexer, session)

	ftesting.Assert(
		lex.Token == KeywordIfToken,
		fmt.Sprintf("priority failed: got %v raw=%q", lex.Token, string(lex.Raw)),
		"priority override works",
		t,
	)
}

// ============================================================
// Error handling
// ============================================================

func testErrorHandling(t *testing.T, lexer *Lexer[rune, LexerState, TestToken]) {

	input := []rune("@")

	session := LexerSessionCreate(
		NormalState,
		input,
		NewlineDetectorRune(),
	)

	_, err := LexerConsume(lexer, session)

	ftesting.Assert(
		err != nil,
		"invalid char accepted",
		"invalid char rejected",
		t,
	)
}

// ============================================================
// Streaming path (callback-based input)
// ============================================================

func testStreamingPath(t *testing.T, lexer *Lexer[rune, LexerState, TestToken]) {

	input := []rune("if test\nif")

	// Simple streaming producer: feeds chunks of the input
	pos := 0
	producer := func(dst []rune) (n int, eof bool, err error) {

		if pos >= len(input) {
			return 0, true, nil
		}

		remaining := len(input) - pos
		if remaining < len(dst) {
			n = remaining
		} else {
			n = len(dst)
		}

		copy(dst, input[pos:pos+n])
		pos += n

		if pos >= len(input) {
			return n, true, nil
		}

		return n, false, nil
	}

	session := StreamingLexerSessionCreate(
		NormalState,
		producer,
		NewlineDetectorRune(),
		2,  // small chunk size to force boundary cases
		64, // plenty for this grammar
	)

	expect := []TestToken{
		KeywordIfToken,
		WhitespaceToken,
		WordToken,
		WhitespaceToken,
		KeywordIfToken,
		EOFToken,
	}

	for i, expected := range expect {

		lex, err := LexerConsumeStreaming(lexer, session)

		ftesting.Assert(
			err == nil,
			fmt.Sprintf("stream step %d: consume error: %v", i, err),
			fmt.Sprintf("stream step %d: consume ok", i),
			t,
		)

		if err == nil {
			ftesting.Assert(
				lex.Token == expected,
				fmt.Sprintf(
					"stream step %d: expected %v got %v raw=%q pos=%d:%d → %d:%d",
					i,
					expected,
					lex.Token,
					string(lex.Raw),
					lex.StartLine,
					lex.StartColumn,
					lex.EndLine,
					lex.EndColumn,
				),
				fmt.Sprintf("stream step %d: token correct (%v)", i, expected),
				t,
			)
		}
	}

	ftesting.Assert(
		session.currentLine == 2,
		fmt.Sprintf("stream newline tracking wrong, ended on line %d", session.currentLine),
		"stream newline tracking correct",
		t,
	)
}
