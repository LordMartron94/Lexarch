package lexarch

import (
	"autarch"
	"autarch/regex"
	"fmt"
	"memarch"
	"memcore"
)

// TextLexerRule defines a rule for lexing.
// The tokenType T ID must be placed in priority order.
// Meaning those with lower IDs have higher priority and take precedence over those with higher IDs.
type TextLexerRule[TTokenType comparable] struct {
	pattern   string
	tokenType TTokenType
}

// TextLexerRuleCreate creates a rule for the lexer.
//
// The pattern is a given RegEx string that specifies the pattern for this lexeme.
//
// The tokenType T ID must be placed in priority order.
// Meaning those with lower IDs have higher priority and take precedence over those with higher IDs.
func TextLexerRuleCreate[TTokenType comparable](pattern string, tokenType TTokenType) TextLexerRule[TTokenType] {
	return TextLexerRule[TTokenType]{
		pattern:   pattern,
		tokenType: tokenType,
	}
}

// Token represents a single token inside the text.
type Token[TTokenType comparable] struct {
	Lexeme    string
	TokenType TTokenType
}

// TextLexer is an engine to transform raw text into tokens using user-defined rules.
type TextLexer[TTokenType comparable] struct {
	stateMachine *autarch.DFA[rune, TTokenType]
	invalidToken TTokenType
}

// TextLexerCreate creates an instance of the text lexer.
func TextLexerCreate[TTokenType comparable](
	allocationFn memarch.AllocationFn,
	invalidToken TTokenType,
	tempAllocationMinSize, tempAllocationMaxSize memcore.MemoryUnitBytes,
	rules ...TextLexerRule[TTokenType],
) *TextLexer[TTokenType] {
	if len(rules) == 0 {
		panic("text lexer must have at least one rule")
	}

	firstRule := rules[0]
	var nfaStateMachine = produceNFAForRegex(
		allocationFn,
		firstRule.pattern,
		firstRule.tokenType,
		invalidToken,
	)

	if len(rules) > 1 {
		for i := 1; i < len(rules); i++ {
			nfaStateMachine = autarch.NFAMergeOr(
				nfaStateMachine,
				produceNFAForRegex(
					allocationFn,
					rules[i].pattern,
					rules[i].tokenType,
					invalidToken,
				),
				allocationFn,
				invalidToken,
				func(r rune) rune {
					return r
				},
			)
		}
	}

	dfaStateMachine := minimizeNFA(
		allocationFn,
		tempAllocationMinSize, tempAllocationMaxSize,
		nfaStateMachine,
		invalidToken,
	)

	return &TextLexer[TTokenType]{
		stateMachine: dfaStateMachine,
		invalidToken: invalidToken,
	}
}

// TextLexerLex lexes a given text input into a sequence of tokens.
// It uses a single-pass (O(n)) "maximal munch" (longest match) strategy.
func TextLexerLex[TTokenType comparable](
	lexer *TextLexer[TTokenType],
	input string,
) ([]Token[TTokenType], error) {

	tokens := make([]Token[TTokenType], 0)
	runes := []rune(input)
	cursor := 0

	transCursor := autarch.DFACursorGet(lexer.stateMachine)

	for cursor < len(runes) {
		scanPos := cursor
		currentState := uint64(0) // Always start from DFA state 0

		var lastValidToken = lexer.invalidToken
		lastValidPos := -1 // -1 means no valid token found yet

		for scanPos < len(runes) {
			observation := runes[scanPos]

			nextState, err := autarch.DFAStep(
				lexer.stateMachine,
				currentState,
				observation,
				transCursor,
			)

			if err != nil {
				break
			}

			outcome := autarch.DFAStateOutcome(lexer.stateMachine, nextState)

			if outcome == lexer.invalidToken {
				break
			}

			lastValidToken = outcome
			lastValidPos = scanPos

			currentState = nextState
			scanPos++
		}

		if lastValidPos == -1 {
			return nil, fmt.Errorf(
				"invalid token at position %d: %q",
				cursor,
				string(runes[cursor]),
			)
		}

		lexemeRunes := runes[cursor : lastValidPos+1]

		tokens = append(tokens, Token[TTokenType]{
			Lexeme:    string(lexemeRunes),
			TokenType: lastValidToken,
		})

		cursor = lastValidPos + 1
	}

	return tokens, nil
}

func produceNFAForRegex[TTokenType comparable](
	allocationFn memarch.AllocationFn,
	regexLiteral string,
	acceptToken TTokenType, invalidToken TTokenType,
) *autarch.NFA[rune, TTokenType] {
	nfa, err := regex.RegexToNFA(
		allocationFn,
		regexLiteral,
		acceptToken,
		invalidToken,
	)

	if err != nil {
		panic(err)
	}
	return nfa
}

func minimizeNFA[TTokenType comparable](
	allocationFn memarch.AllocationFn,
	tempAllocationMinSize, tempAllocationMaxSize memcore.MemoryUnitBytes,
	nfa *autarch.NFA[rune, TTokenType],
	invalidToken TTokenType,
) *autarch.DFA[rune, TTokenType] {
	dfa := autarch.NFAToDFA(
		nfa,
		tempAllocationMinSize, tempAllocationMaxSize,
		allocationFn,
		invalidToken,
	)

	minimized := autarch.DFAMinimize(
		dfa,
		allocationFn,
		tempAllocationMinSize, tempAllocationMaxSize,
		invalidToken,
	)

	return minimized
}
