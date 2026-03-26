package lexarch

import (
	"autarch"
	"autarch/pattern"
	"cmp"
	"fmt"
)

/*
LexerDebugDFA returns a human-readable debug string for the DFA of a specific lexer state.
This is useful for understanding the compiled automaton structure, transitions, and outcomes.

Use cases:
- Debugging lexer compilation issues
- Understanding automaton structure for a state
- Verifying pattern compilation correctness
- Inspecting transition tables and state outcomes

Time complexity: O(s * a) where s is states, a is alphabet size
Space complexity: O(s * a) for string building

Prerequisites:
- lexer must be a valid, non-closed lexer
- state must be a valid state that exists in the lexer

Edge cases:
- Returns empty string if state doesn't exist
- Safe to call on closed lexer (returns empty string)
- If formatter is nil, uses default formatting
*/
func LexerDebugDFA[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	state TState,
	formatter *autarch.DFADebugFormatter[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]],
) string {
	dfa, ok := lexer.ruleSets[state]
	if !ok {
		return fmt.Sprintf("No DFA found for state: %v\n", state)
	}

	return autarch.DFADebugPrint(dfa, formatter)
}

/*
LexerDebugFormatterCreateRune creates a default formatter for rune-based lexers that converts
numeric symbol names to character representations and formats TokenOutcome structures.

The formatter handles:
- Rune values: "105" → "'i'" or "105 ('i')" for better readability
- TokenOutcome: Formats as "{Token: X, Priority: Y}"
- IDs: Keeps as numeric strings for table alignment

Use cases:
- Improving readability of DFA debug output for rune-based lexers
- Converting ASCII codes to character representations
- Formatting token outcomes with priority information

Time complexity: O(1) per formatting call
Space complexity: O(1) per formatting call

Prerequisites:
- TState and TToken must be comparable types
- Formatter is designed for rune-based observations

Edge cases:
- Handles non-printable characters gracefully
- Falls back to numeric representation if parsing fails
- Maintains table alignment with fixed-width formatting
*/
func LexerDebugFormatterCreateRune[TState, TToken, TTokenRole comparable]() *autarch.DFADebugFormatter[rune, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]] {

	// Helper to format a single rune beautifully
	formatRune := func(r rune) string {
		switch {
		case r == '\n':
			return "'\\n'"
		case r == '\t':
			return "'\\t'"
		case r == '\r':
			return "'\\r'"
		case r == ' ':
			return "SPACE"
		case r >= 32 && r < 127:
			return fmt.Sprintf("'%c'", r)
		default:
			return fmt.Sprintf("\\u%04x", r)
		}
	}

	return &autarch.DFADebugFormatter[rune, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]]{
		FormatSymbolName: func(symbolID uint64, def autarch.SymbolDefinition[rune]) string {
			if def.Observation != nil {
				return fmt.Sprintf("Lit: %s", formatRune(*def.Observation))
			}

			if def.GapLo != nil && def.GapHi != nil {
				if *def.GapHi <= *def.GapLo+1 {
					return fmt.Sprintf("Gap: (EMPTY) between %s and %s", formatRune(*def.GapLo), formatRune(*def.GapHi))
				}

				return fmt.Sprintf("Gap: %s < ... < %s", formatRune(*def.GapLo), formatRune(*def.GapHi))
			}

			return def.Name
		},

		FormatStateOutcome: func(outcome pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]) string {
			return fmt.Sprintf("{Token: %v, Priority: %d}", outcome.Value.Token, outcome.Value.Priority)
		},

		FormatSymbolID: func(symbolID uint64) string {
			return fmt.Sprintf("%d", symbolID)
		},

		FormatStateID: func(stateID uint64) string {
			return fmt.Sprintf("%d", stateID)
		},
	}
}
