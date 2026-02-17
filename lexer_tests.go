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
	QuotedStringToken

	ErrorToken
	EOFToken
)

type TokenRole int

const (
	DefaultTokenRole TokenRole = iota + 1
)

// ============================================================
// Test formatter
// ============================================================

func createTestFormatter() *autarch.DFADebugFormatter[rune, TokenOutcome[TestToken, TokenRole]] {

	tokenNames := map[TestToken]string{
		WhitespaceToken:   "WhitespaceToken",
		WordToken:         "WordToken",
		KeywordIfToken:    "KeywordIfToken",
		QuotedStringToken: "QuotedStringToken",
		ErrorToken:        "ErrorToken",
		EOFToken:          "EOFToken",
	}

	stateNames := map[LexerState]string{
		NormalState: "NormalState",
	}

	baseFormatter := LexerDebugFormatterCreateRune[LexerState, TestToken, TokenRole]()

	return &autarch.DFADebugFormatter[rune, TokenOutcome[TestToken, TokenRole]]{
		FormatSymbolName: baseFormatter.FormatSymbolName,
		FormatSymbolID:   baseFormatter.FormatSymbolID,
		FormatStateOutcome: func(outcome TokenOutcome[TestToken, TokenRole]) string {
			name := tokenNames[outcome.Token]
			if name == "" {
				name = fmt.Sprintf("Token(%d)", outcome.Token)
			}
			return fmt.Sprintf("{Token: %s, Priority: %d}", name, outcome.Priority)
		},
		FormatStateID: func(stateID uint64) string {
			state := LexerState(stateID)
			stateName := stateNames[state]
			if stateName == "" {
				return fmt.Sprintf("%3d", stateID)
			}
			return fmt.Sprintf("%-3d", stateID)
		},
	}
}

// ============================================================
// Build lexer
// ============================================================

func buildTestLexer() (lexer *Lexer[rune, LexerState, TestToken, TokenRole], allocator memcore.MarkRaw) {

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

	cleanupNeeded := true
	defer func() {
		if cleanupNeeded {
			memforge.DynamicLinearAllocatorDestroy(allocator)
		}
	}()

	rules := LexingRulesetCreate[rune, TestToken, TokenRole](
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

	// ------------------------
	// Quoted string pattern
	// ------------------------
	quote := pattern.Literal('"')

	notQuote := pattern.Class(
		pattern.Range(0, '"'-1),
		pattern.Range('"'+1, rune(0x10FFFF)),
	)

	quotedString :=
		pattern.Sequence(
			quote,
			notQuote.Star(),
			quote,
		)

	rules.WithRulePriority(keywordIf, KeywordIfToken, DefaultTokenRole, 10)
	rules.WithRule(quotedString, QuotedStringToken, DefaultTokenRole)
	rules.WithRule(word, WordToken, DefaultTokenRole)
	rules.WithRule(whitespace, WhitespaceToken, DefaultTokenRole)

	lexer = LexerCreate(
		map[LexerState]LexingRuleset[rune, TestToken, TokenRole]{
			NormalState: *rules,
		},
		EOFToken,
		func(sizeBytes, alignment uint64) memcore.MarkRaw {
			return memforge.DynamicLinearAllocatorMallocUnsafe(
				allocator,
				sizeBytes,
				alignment,
			)
		},
		memcore.GigaByte,
		ObservationCTX[rune]{
			Formatter:   RuneFormatterDefault(),
			SuccessorFn: LexarchRuneSuccessorFn(),
		},
	)

	fmt.Println("=== DFA Debug for NormalState ===")
	formatter := createTestFormatter()
	fmt.Print(LexerDebugDFA(lexer, NormalState, formatter))
	fmt.Println("=== End DFA Debug ===")

	cleanupNeeded = false
	return lexer, allocator
}

// ============================================================
// ENTRY
// ============================================================

func LexarchTestLexer(t *testing.T) {

	lexer, allocator := buildTestLexer()

	defer memforge.DynamicLinearAllocatorDestroy(allocator)
	defer LexerClose(lexer)

	testQuotedStringClassic(t, lexer)
	testQuotedStringStreaming(t, lexer)
}

// ============================================================
// Quoted string test (classic path)
// ============================================================

func testQuotedStringClassic(t *testing.T, lexer *Lexer[rune, LexerState, TestToken, TokenRole]) {

	input := []rune(`"hello world"`)

	session := LexerSessionCreate[rune, LexerState, TestToken](
		NormalState,
		input,
		NewlineDetectorRune(),
	)

	lex := LexerConsume(lexer, session)

	ftesting.Assert(
		session.lastError == nil,
		fmt.Sprintf("quoted string consume error: %v", session.lastError),
		"quoted string consume ok",
		t,
	)

	if session.lastError == nil {
		ftesting.Assert(
			lex.Token == QuotedStringToken,
			fmt.Sprintf("expected QuotedStringToken got %v raw=%q", lex.Token, string(lex.Raw)),
			"quoted string token correct",
			t,
		)

		ftesting.Assert(
			string(lex.Raw) == `"hello world"`,
			fmt.Sprintf("quoted content mismatch: %q", string(lex.Raw)),
			"quoted content preserved",
			t,
		)
	}
}

// ============================================================
// Quoted string test (streaming path)
// ============================================================

func testQuotedStringStreaming(t *testing.T, lexer *Lexer[rune, LexerState, TestToken, TokenRole]) {

	input := []rune(`"hello world"`)

	pos := 0
	producer := func(dst []rune) (n int, eof bool, err error) {

		if pos >= len(input) {
			return 0, true, nil
		}

		n = min(len(dst), len(input)-pos)
		copy(dst, input[pos:pos+n])
		pos += n

		if pos >= len(input) {
			return n, true, nil
		}

		return n, false, nil
	}

	session := StreamingLexerSessionCreate[rune, LexerState, TestToken](
		NormalState,
		producer,
		NewlineDetectorRune(),
		2, // intentionally small
		64,
	)

	lex := LexerConsumeStreaming(lexer, session)

	ftesting.Assert(
		session.lastError == nil,
		fmt.Sprintf("stream quoted consume error: %v", session.lastError),
		"stream quoted consume ok",
		t,
	)

	if session.lastError == nil {
		ftesting.Assert(
			lex.Token == QuotedStringToken,
			fmt.Sprintf("stream expected QuotedStringToken got %v raw=%q", lex.Token, string(lex.Raw)),
			"stream quoted token correct",
			t,
		)
	}
}
