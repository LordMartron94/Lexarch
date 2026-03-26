package lexarch

import (
	"autarch/pattern"
	"cmp"
)

type lexingRule[TObservation cmp.Ordered, TToken, TTokenRole comparable] struct {
	pattern  pattern.RegulaAST[TObservation]
	token    TToken
	role     TTokenRole
	priority int
}

/*
DelimitedRuleSpec holds open and close patterns for a token that represents a
delimited region (e.g. block comment with open and close). Used by downstream
consumers (e.g. editor IR) to generate push/body/pop state machines; lexer
scanning is unchanged and still uses the token's main pattern.
*/
type DelimitedRuleSpec[TObservation cmp.Ordered] struct {
	Open  pattern.RegulaAST[TObservation]
	Close pattern.RegulaAST[TObservation]
}

/*
TokenOutcome stores both the token type and its priority, used as the outcome type
for DFAs to eliminate runtime priority lookups.

Use cases:
- Storing token and priority together in DFA states
- Eliminating hash map lookups during token scanning
- Enabling efficient priority-based resolution

Time complexity: N/A - data structure
Space complexity: O(1) - stores token and priority

Prerequisites:
- Token must be comparable
- Priority is an integer value

Edge cases:
- Priority can be any integer value (0 is default)
- Token must be distinct from error token
*/
type TokenOutcome[TToken, TTokenRole comparable] struct {
	Token     TToken
	TokenRole TTokenRole
	Priority  int
}

/*
NewlineDetector determines if an observation represents a newline character.
Returns true if the observation is a newline, false otherwise.
This allows the library to work with any observation type (runes, bytes, etc.)
while maintaining flexibility for different newline conventions.

Use cases:
- Detecting newlines in rune-based text lexing
- Detecting newlines in byte-based lexing
- Supporting custom newline conventions (e.g., \r\n, \n, etc.)

Time complexity: O(1) - single observation check
Space complexity: O(1)

Prerequisites:
- Must correctly identify newline characters for the observation type

Edge cases:
- Should return false for non-newline observations
- Should handle all newline variants consistently
*/
type NewlineDetector[TObservation cmp.Ordered] func(obs TObservation) bool

/*
NewlineDetectorRune returns a newline detector for rune observations that detects '\n' characters.
This is the standard newline character for Unix/Linux systems and most text formats.

Use cases:
- Lexing text files with Unix-style line endings
- Processing rune-based input streams
- Standard newline detection for most use cases

Time complexity: O(1)
Space complexity: O(1)
*/
func NewlineDetectorRune() NewlineDetector[rune] {
	return func(obs rune) bool {
		return obs == '\n'
	}
}

/*
NewlineDetectorByte returns a newline detector for byte observations that detects '\n' characters.
This is the standard newline character for Unix/Linux systems and most binary formats.

Use cases:
- Lexing binary files or byte streams
- Processing byte-based input streams
- Standard newline detection for byte-level lexing

Time complexity: O(1)
Space complexity: O(1)
*/
func NewlineDetectorByte() NewlineDetector[byte] {
	return func(obs byte) bool {
		return obs == '\n'
	}
}

/* ColumnAdvanceFn takes an observation and the current column and outputs the next column. */
type ColumnAdvanceFn[TObservation any] func(
	obs TObservation,
	currentColumn int,
) int

func ColumnAdvanceRune(tabWidth int) ColumnAdvanceFn[rune] {
	if tabWidth <= 0 {
		panic("tabWidth must be > 0")
	}

	return func(r rune, col int) int {
		switch r {
		case '\t':
			return col + (tabWidth - ((col - 1) % tabWidth))
		default:
			return col + 1
		}
	}
}

/*
LexingRuleset represents a collection of pattern-to-token mappings that define how to recognize
tokens in a specific lexer state. Rules are compiled into a single DFA that matches the longest
possible token at each position.

Use cases:
- Defining token recognition rules for a specific lexer state
- Building context-sensitive lexers with different rules per state
- Creating reusable token pattern definitions

Time complexity: O(1) per rule addition
Space complexity: O(n) where n is the number of rules

Prerequisites:
- Patterns must be valid RegulaAST structures from autarch/pattern
- Token type must be comparable

Edge cases:
- Empty rulesets will create a DFA that never accepts
- Rules are evaluated in order, with longest match taking precedence
- Multiple rules matching the same input will prefer the longest match
*/
type LexingRuleset[TObservation cmp.Ordered, TToken, TTokenRole comparable] struct {
	precompiledRules    []lexingRule[TObservation, TToken, TTokenRole]
	tokenResolutionStep TokenResolutionStepFn[TToken]

	tokenPatternMapping map[TToken]pattern.RegulaAST[TObservation]
	delimitedRules      map[TToken]DelimitedRuleSpec[TObservation]
}

/*
WithRule adds a pattern-to-token mapping to the ruleset. The pattern defines what input sequence
matches this token, and the token value is returned when the pattern is recognized.

Use cases:
- Building up token recognition rules incrementally
- Defining keyword, identifier, number, and operator patterns
- Creating flexible token definitions

Time complexity: O(1) - appends to internal slice
Space complexity: O(1) - stores reference to pattern

Prerequisites:
- pattern must be a valid RegulaAST from autarch/pattern
- token must be a comparable value

Edge cases:
- Patterns are stored by reference, so modifications to the pattern after adding may affect behavior
- Order of rule addition matters for longest match resolution
*/
func (l *LexingRuleset[TObservation, TToken, TTokenRole]) WithRule(pattern pattern.RegulaAST[TObservation], token TToken, role TTokenRole) {
	l.tokenPatternMapping[token] = pattern
	l.precompiledRules = append(l.precompiledRules, lexingRule[TObservation, TToken, TTokenRole]{
		pattern:  pattern,
		token:    token,
		role:     role,
		priority: 0,
	})
}

/*
WithRulePriority adds a pattern-to-token mapping with an explicit priority to the ruleset.
Higher priority values are preferred during priority-based resolution.

Use cases:
- Priority-based token resolution
- Explicit control over token selection order
- Fine-grained conflict resolution

Time complexity: O(1) - appends to internal slice
Space complexity: O(1) - stores reference to pattern

Prerequisites:
- pattern must be a valid RegulaAST from autarch/pattern
- token must be a comparable value
- priority is used only with priority-based resolution functions

Edge cases:
- Default priority is 0 if not specified
- Higher priority values win in priority-based resolution
- Patterns are stored by reference, so modifications may affect behavior
*/
func (l *LexingRuleset[TObservation, TToken, TTokenRole]) WithRulePriority(
	pattern pattern.RegulaAST[TObservation],
	token TToken,
	role TTokenRole,
	priority int,
) {
	l.tokenPatternMapping[token] = pattern
	l.precompiledRules = append(l.precompiledRules, lexingRule[TObservation, TToken, TTokenRole]{
		pattern:  pattern,
		token:    token,
		role:     role,
		priority: priority,
	})
}

func (l *LexingRuleset[TObservation, TToken, TTokenRole]) GetPattern(token TToken) (pattern.RegulaAST[TObservation], bool) {
	pattern, ok := l.tokenPatternMapping[token]
	return pattern, ok
}

/*
WithDelimitedRule annotates an existing token rule with open/close patterns for
delimited regions. The token must already have a rule (via WithRule or
WithRulePriority); this only stores metadata for consumers (e.g. editor IR).
Lexer compilation and scanning are unchanged.
*/
func (l *LexingRuleset[TObservation, TToken, TTokenRole]) WithDelimitedRule(
	open, close pattern.RegulaAST[TObservation],
	token TToken,
) {
	if l.delimitedRules == nil {
		l.delimitedRules = make(map[TToken]DelimitedRuleSpec[TObservation])
	}
	l.delimitedRules[token] = DelimitedRuleSpec[TObservation]{Open: open, Close: close}
}

/*
GetDelimitedRule returns the open/close patterns for a token that has a
delimited rule, if any. ok is false if the token was not registered with
WithDelimitedRule.
*/
func (l *LexingRuleset[TObservation, TToken, TTokenRole]) GetDelimitedRule(token TToken) (open, close pattern.RegulaAST[TObservation], ok bool) {
	if l.delimitedRules == nil {
		return open, close, false
	}
	spec, ok := l.delimitedRules[token]
	if !ok {
		return open, close, false
	}
	return spec.Open, spec.Close, true
}

/*
LexingRulesetCreate creates a new empty ruleset ready for pattern-to-token mappings.

Use cases:
- Initializing rulesets for lexer state definitions
- Building token recognition rules programmatically
- Creating reusable token pattern collections

Time complexity: O(1)
Space complexity: O(1) - allocates empty slice

Prerequisites:
- None

Edge cases:
- Returns empty ruleset that must have rules added before use
- Empty rulesets will compile to DFAs that never accept
*/
func LexingRulesetCreate[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	tokenResolutionStep TokenResolutionStepFn[TToken],
) *LexingRuleset[TObservation, TToken, TTokenRole] {
	if tokenResolutionStep == nil {
		tokenResolutionStep = TokenResolutionStepLongest[TToken]
	}
	return &LexingRuleset[TObservation, TToken, TTokenRole]{
		precompiledRules:    make([]lexingRule[TObservation, TToken, TTokenRole], 0),
		tokenResolutionStep: tokenResolutionStep,
		tokenPatternMapping: make(map[TToken]pattern.RegulaAST[TObservation]),
	}
}

/*
WithTokenResolution sets the token resolution function for the ruleset. This function
determines which token is selected when multiple tokens match at the same position.

Use cases:
- Configuring custom token resolution strategies
- Switching between longest, shortest, or first match
- Implementing priority-based resolution

Time complexity: O(1)
Space complexity: O(1)

Prerequisites:
- resolutionStep must be a valid TokenResolutionStepFn
- If nil, defaults to TokenResolutionStepLongest

Edge cases:
- Setting nil resolution uses default (longest match)
- Resolution function is used during scanning
*/
func (l *LexingRuleset[TObservation, TToken, TTokenRole]) WithTokenResolution(
	resolutionStep TokenResolutionStepFn[TToken],
) *LexingRuleset[TObservation, TToken, TTokenRole] {
	if resolutionStep == nil {
		resolutionStep = TokenResolutionStepLongest[TToken]
	}
	l.tokenResolutionStep = resolutionStep
	return l
}

/* LexerRuleReadOnly provides a readonly view into a lexing rule. */
type LexerRuleReadOnly[TObservation cmp.Ordered, TToken, TTokenRole comparable] struct {
	Pattern  pattern.RegulaAST[TObservation]
	Token    TToken
	Role     TTokenRole
	Priority int
}

/* PatternToRegEx returns the rule's pattern as a RegEx string. */
func (l *LexerRuleReadOnly[TObservation, TToken, TTokenRole]) PatternToRegEx() (string, error) {
	return l.Pattern.ToRegEx()
}

/*
GetRules returns the currently precompiled rules in a readonly fashion.
*/
func (l *LexingRuleset[TObservation, TToken, TTokenRole]) GetRules() []LexerRuleReadOnly[TObservation, TToken, TTokenRole] {
	out := make([]LexerRuleReadOnly[TObservation, TToken, TTokenRole], len(l.precompiledRules))

	for i, precompiled := range l.precompiledRules {
		out[i] = LexerRuleReadOnly[TObservation, TToken, TTokenRole]{
			Pattern:  precompiled.pattern,
			Token:    precompiled.token,
			Role:     precompiled.role,
			Priority: precompiled.priority,
		}
	}

	return out
}
