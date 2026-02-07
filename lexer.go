package lexarch

import (
	"autarch"
	"fmt"
	"memarch"
	"memcore"
	"regexp"
	"unicode"
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

/*
produceNFAForRegex converts a regular expression pattern to an NFA using Go's built-in regexp package.

The function validates the pattern using regexp.Compile, then constructs an NFA using Thompson's
construction algorithm. Supports common regex features including literals, character classes,
quantifiers, alternation, and grouping.

Use cases:
- Building lexers from regex patterns
- Converting regex patterns to executable automata
- Pattern matching with regular expressions

Time complexity: O(p) where p is pattern length for parsing, O(s) for NFA construction
Space complexity: O(s) where s is number of states in resulting NFA

Prerequisites:
- regexLiteral must be a valid Go regular expression pattern
- allocationFn must provide sufficient memory
- acceptToken and invalidToken must be distinct

Edge cases:
- Panics if pattern is invalid (cannot be compiled by regexp)
- Empty pattern matches empty string
- Anchors (^, $) are treated as literals in this implementation
*/
func produceNFAForRegex[TTokenType comparable](
	allocationFn memarch.AllocationFn,
	regexLiteral string,
	acceptToken TTokenType, invalidToken TTokenType,
) *autarch.NFA[rune, TTokenType] {
	// Validate pattern using Go's regexp
	_, err := regexp.Compile(regexLiteral)
	if err != nil {
		panic(fmt.Errorf("invalid regex pattern %q: %w", regexLiteral, err))
	}

	// Build NFA from pattern
	return regexPatternToNFA(
		allocationFn,
		regexLiteral,
		acceptToken,
		invalidToken,
	)
}

/*
regexPatternToNFA constructs an NFA from a regex pattern using recursive descent parsing
and Thompson's construction algorithm.

This implementation handles:
- Literal characters and escaped sequences
- Character classes [a-z], [^a-z]
- Quantifiers *, +, ?
- Alternation |
- Grouping ()
- Predefined character classes \d, \w, \s and their negations

The algorithm parses the pattern and builds NFA fragments, then combines them
using epsilon transitions according to Thompson's construction.
*/
func regexPatternToNFA[TTokenType comparable](
	allocationFn memarch.AllocationFn,
	pattern string,
	acceptToken TTokenType,
	invalidToken TTokenType,
) *autarch.NFA[rune, TTokenType] {
	parser := &regexParser{
		pattern:  []rune(pattern),
		pos:      0,
		alphabet: make(map[rune]uint64),
		runes:    make([]rune, 0),
	}

	// Parse pattern and build NFA fragment
	fragment := parser.parseExpression()

	// Collect all unique runes from transitions to build alphabet
	alphabetSet := make(map[rune]struct{})
	for _, trans := range fragment.transitions {
		if trans.Symbol.SymbolID != autarch.AutarchEpsilonID {
			// Find the rune for this symbol ID
			for r, id := range parser.alphabet {
				if id == trans.Symbol.SymbolID {
					alphabetSet[r] = struct{}{}
					break
				}
			}
		}
	}

	// Build alphabet slice (sorted for determinism)
	alphabet := make([]rune, 0, len(alphabetSet))
	for r := range alphabetSet {
		alphabet = append(alphabet, r)
	}

	// Create indexer
	indexer := func(observation rune) (autarch.Symbol[rune], bool) {
		id, ok := parser.alphabet[observation]
		if !ok {
			return autarch.Symbol[rune]{}, false
		}
		return autarch.SymbolCreate[rune](string(observation), id), true
	}

	// Build states array
	numStates := parser.nextStateID
	states := make([]TTokenType, numStates)
	for i := uint64(0); i < numStates; i++ {
		states[i] = invalidToken
	}
	states[fragment.accept] = acceptToken

	// Create NFA
	return autarch.NFACreate(
		allocationFn,
		alphabet,
		fragment.transitions,
		[]uint64{fragment.start},
		states,
		indexer,
	)
}

type nfaFragment struct {
	start       uint64
	accept      uint64
	transitions []autarch.Transition[rune]
}

type regexParser struct {
	pattern    []rune
	pos        int
	alphabet   map[rune]uint64
	runes      []rune
	nextStateID uint64
}

func (p *regexParser) allocateStateID() uint64 {
	id := p.nextStateID
	p.nextStateID++
	return id
}

func (p *regexParser) getOrCreateSymbol(r rune) autarch.Symbol[rune] {
	id, exists := p.alphabet[r]
	if !exists {
		id = uint64(len(p.alphabet))
		p.alphabet[r] = id
		p.runes = append(p.runes, r)
	}
	return autarch.SymbolCreate[rune](string(r), id)
}

func (p *regexParser) parseExpression() *nfaFragment {
	fragment := p.parseTerm()
	if p.pos < len(p.pattern) && p.pattern[p.pos] == '|' {
		p.pos++ // consume '|'
		right := p.parseExpression()
		return p.mergeAlternation(fragment, right)
	}
	return fragment
}

func (p *regexParser) parseTerm() *nfaFragment {
	fragment := p.parseFactor()
	for p.pos < len(p.pattern) && p.pattern[p.pos] != '|' && p.pattern[p.pos] != ')' {
		next := p.parseFactor()
		fragment = p.mergeConcatenation(fragment, next)
	}
	return fragment
}

func (p *regexParser) parseFactor() *nfaFragment {
	base := p.parseAtom()
	if p.pos >= len(p.pattern) {
		return base
	}

	switch p.pattern[p.pos] {
	case '*':
		p.pos++
		return p.applyKleeneStar(base)
	case '+':
		p.pos++
		return p.applyKleenePlus(base)
	case '?':
		p.pos++
		return p.applyOptional(base)
	default:
		return base
	}
}

func (p *regexParser) parseAtom() *nfaFragment {
	if p.pos >= len(p.pattern) {
		// Empty pattern - match empty string
		start := p.allocateStateID()
		accept := p.allocateStateID()
		return &nfaFragment{
			start:  start,
			accept: accept,
			transitions: []autarch.Transition[rune]{
				{
					CurrentState: start,
					Symbol:       autarch.EpsilonSymbolCreate[rune](),
					NextState:    accept,
				},
			},
		}
	}

	switch p.pattern[p.pos] {
	case '(':
		p.pos++ // consume '('
		fragment := p.parseExpression()
		if p.pos >= len(p.pattern) || p.pattern[p.pos] != ')' {
			panic(fmt.Errorf("unmatched opening parenthesis at position %d", p.pos))
		}
		p.pos++ // consume ')'
		return fragment

	case '[':
		return p.parseCharacterClass()

	case '\\':
		return p.parseEscapeSequence()

	case '.':
		p.pos++
		return p.parseWildcard()

	default:
		// Literal character
		r := p.pattern[p.pos]
		p.pos++
		return p.createLiteralFragment(r)
	}
}

func (p *regexParser) parseCharacterClass() *nfaFragment {
	p.pos++ // consume '['
	negated := false
	if p.pos < len(p.pattern) && p.pattern[p.pos] == '^' {
		negated = true
		p.pos++
	}

	chars := make(map[rune]bool)
	for p.pos < len(p.pattern) && p.pattern[p.pos] != ']' {
		if p.pos+2 < len(p.pattern) && p.pattern[p.pos+1] == '-' {
			// Character range [a-z]
			start := p.pattern[p.pos]
			end := p.pattern[p.pos+2]
			p.pos += 3
			for r := start; r <= end; r++ {
				chars[r] = true
			}
		} else {
			chars[p.pattern[p.pos]] = true
			p.pos++
		}
	}

	if p.pos >= len(p.pattern) {
		panic(fmt.Errorf("unclosed character class"))
	}
	p.pos++ // consume ']'

	start := p.allocateStateID()
	accept := p.allocateStateID()
	transitions := make([]autarch.Transition[rune], 0)

	// Build transitions for all characters in class
	for r := range chars {
		symbol := p.getOrCreateSymbol(r)
		transitions = append(transitions, autarch.Transition[rune]{
			CurrentState: start,
			Symbol:       symbol,
			NextState:    accept,
		})
	}

	// If negated, we'd need to add transitions for all characters NOT in the class
	// For simplicity, we'll handle common cases
	if negated {
		// This is a simplified implementation - full negation would require
		// knowing the full alphabet, which we don't have yet
		// For now, we'll create transitions for common printable ASCII
		for r := rune(32); r < 127; r++ {
			if !chars[r] {
				symbol := p.getOrCreateSymbol(r)
				transitions = append(transitions, autarch.Transition[rune]{
					CurrentState: start,
					Symbol:       symbol,
					NextState:    accept,
				})
			}
		}
	}

	return &nfaFragment{
		start:       start,
		accept:      accept,
		transitions: transitions,
	}
}

func (p *regexParser) parseEscapeSequence() *nfaFragment {
	p.pos++ // consume '\'
	if p.pos >= len(p.pattern) {
		panic(fmt.Errorf("escape sequence at end of pattern"))
	}

	r := p.pattern[p.pos]
	p.pos++

	switch r {
	case 'd':
		// Digit: [0-9]
		return p.parsePredefinedClass(true, unicode.IsDigit)
	case 'D':
		// Non-digit
		return p.parsePredefinedClass(false, unicode.IsDigit)
	case 'w':
		// Word character: [a-zA-Z0-9_]
		return p.parsePredefinedClass(true, func(r rune) bool {
			return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
		})
	case 'W':
		// Non-word character
		return p.parsePredefinedClass(false, func(r rune) bool {
			return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
		})
	case 's':
		// Whitespace
		return p.parsePredefinedClass(true, unicode.IsSpace)
	case 'S':
		// Non-whitespace
		return p.parsePredefinedClass(false, unicode.IsSpace)
	default:
		// Escaped literal
		return p.createLiteralFragment(r)
	}
}

func (p *regexParser) parsePredefinedClass(positive bool, predicate func(rune) bool) *nfaFragment {
	start := p.allocateStateID()
	accept := p.allocateStateID()
	transitions := make([]autarch.Transition[rune], 0)

	// Add transitions for all matching runes
	// We'll use a reasonable range for common characters
	for r := rune(0); r < 256; r++ {
		matches := predicate(r)
		if (positive && matches) || (!positive && !matches) {
			symbol := p.getOrCreateSymbol(r)
			transitions = append(transitions, autarch.Transition[rune]{
				CurrentState: start,
				Symbol:       symbol,
				NextState:    accept,
			})
		}
	}

	return &nfaFragment{
		start:       start,
		accept:      accept,
		transitions: transitions,
	}
}

func (p *regexParser) parseWildcard() *nfaFragment {
	start := p.allocateStateID()
	accept := p.allocateStateID()
	transitions := make([]autarch.Transition[rune], 0)

	// Wildcard matches any character except newline
	// Add transitions for common printable ASCII
	for r := rune(32); r < 127; r++ {
		if r != '\n' && r != '\r' {
			symbol := p.getOrCreateSymbol(r)
			transitions = append(transitions, autarch.Transition[rune]{
				CurrentState: start,
				Symbol:       symbol,
				NextState:    accept,
			})
		}
	}

	return &nfaFragment{
		start:       start,
		accept:      accept,
		transitions: transitions,
	}
}

func (p *regexParser) createLiteralFragment(r rune) *nfaFragment {
	start := p.allocateStateID()
	accept := p.allocateStateID()
	symbol := p.getOrCreateSymbol(r)

	return &nfaFragment{
		start:  start,
		accept: accept,
		transitions: []autarch.Transition[rune]{
			{
				CurrentState: start,
				Symbol:       symbol,
				NextState:    accept,
			},
		},
	}
}

func (p *regexParser) mergeConcatenation(left, right *nfaFragment) *nfaFragment {
	// Connect left's accept to right's start with epsilon
	epsilon := autarch.EpsilonSymbolCreate[rune]()
	transitions := append(left.transitions, right.transitions...)
	transitions = append(transitions, autarch.Transition[rune]{
		CurrentState: left.accept,
		Symbol:       epsilon,
		NextState:    right.start,
	})

	return &nfaFragment{
		start:       left.start,
		accept:      right.accept,
		transitions: transitions,
	}
}

func (p *regexParser) mergeAlternation(left, right *nfaFragment) *nfaFragment {
	start := p.allocateStateID()
	accept := p.allocateStateID()
	epsilon := autarch.EpsilonSymbolCreate[rune]()

	transitions := append(left.transitions, right.transitions...)
	// Epsilon from new start to both left and right starts
	transitions = append(transitions,
		autarch.Transition[rune]{
			CurrentState: start,
			Symbol:       epsilon,
			NextState:    left.start,
		},
		autarch.Transition[rune]{
			CurrentState: start,
			Symbol:       epsilon,
			NextState:    right.start,
		},
		// Epsilon from both accepts to new accept
		autarch.Transition[rune]{
			CurrentState: left.accept,
			Symbol:       epsilon,
			NextState:    accept,
		},
		autarch.Transition[rune]{
			CurrentState: right.accept,
			Symbol:       epsilon,
			NextState:    accept,
		},
	)

	return &nfaFragment{
		start:       start,
		accept:      accept,
		transitions: transitions,
	}
}

func (p *regexParser) applyKleeneStar(fragment *nfaFragment) *nfaFragment {
	start := p.allocateStateID()
	accept := p.allocateStateID()
	epsilon := autarch.EpsilonSymbolCreate[rune]()

	transitions := append(fragment.transitions,
		// Epsilon from new start to fragment start and accept
		autarch.Transition[rune]{
			CurrentState: start,
			Symbol:       epsilon,
			NextState:    fragment.start,
		},
		autarch.Transition[rune]{
			CurrentState: start,
			Symbol:       epsilon,
			NextState:    accept,
		},
		// Epsilon from fragment accept back to start and to new accept
		autarch.Transition[rune]{
			CurrentState: fragment.accept,
			Symbol:       epsilon,
			NextState:    fragment.start,
		},
		autarch.Transition[rune]{
			CurrentState: fragment.accept,
			Symbol:       epsilon,
			NextState:    accept,
		},
	)

	return &nfaFragment{
		start:       start,
		accept:      accept,
		transitions: transitions,
	}
}

func (p *regexParser) applyKleenePlus(fragment *nfaFragment) *nfaFragment {
	// a+ = a a*
	starred := p.applyKleeneStar(fragment)
	return p.mergeConcatenation(fragment, starred)
}

func (p *regexParser) applyOptional(fragment *nfaFragment) *nfaFragment {
	start := p.allocateStateID()
	accept := p.allocateStateID()
	epsilon := autarch.EpsilonSymbolCreate[rune]()

	transitions := append(fragment.transitions,
		// Epsilon from new start to fragment start and accept
		autarch.Transition[rune]{
			CurrentState: start,
			Symbol:       epsilon,
			NextState:    fragment.start,
		},
		autarch.Transition[rune]{
			CurrentState: start,
			Symbol:       epsilon,
			NextState:    accept,
		},
		// Epsilon from fragment accept to new accept
		autarch.Transition[rune]{
			CurrentState: fragment.accept,
			Symbol:       epsilon,
			NextState:    accept,
		},
	)

	return &nfaFragment{
		start:       start,
		accept:      accept,
		transitions: transitions,
	}
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

// TextLexerStream is a stateful, streaming lexer that processes
// input in chunks.
type TextLexerStream[TTokenType comparable] struct {
	stateMachine *autarch.DFA[rune, TTokenType]
	invalidToken TTokenType

	buffer []rune
	cursor int
	closed bool
}

// TextLexerStreamCreate creates a new streaming lexer.
func TextLexerStreamCreate[TTokenType comparable](
	lexer *TextLexer[TTokenType],
) *TextLexerStream[TTokenType] {
	return &TextLexerStream[TTokenType]{
		stateMachine: lexer.stateMachine,
		invalidToken: lexer.invalidToken,
		buffer:       make([]rune, 0, 4096),
		cursor:       0,
		closed:       false,
	}
}

// TextLexerStreamAdd adds a chunk of text to the stream's buffer.
func TextLexerStreamAdd[TTokenType comparable](
	stream *TextLexerStream[TTokenType],
	chunk string,
) {
	if stream.closed {
		panic("cannot add data to a closed lexer stream")
	}
	stream.buffer = append(stream.buffer, []rune(chunk)...)
}

// TextLexerStreamClose signals the end of the input (EOF).
// This tells the lexer to flush any remaining partial tokens
// on the next call to Next().
func TextLexerStreamClose[TTokenType comparable](
	stream *TextLexerStream[TTokenType],
) {
	stream.closed = true
}

// TextLexerStreamNext attempts to read one token from the stream.
// It returns:
//
//	(token, true, nil)    - if a token was successfully lexed.
//	(Token{}, false, nil) - if more data is needed (call Add/Close).
//	(Token{}, false, error) - if a lexing error occurred.
func TextLexerStreamNext[TTokenType comparable](
	stream *TextLexerStream[TTokenType],
) (Token[TTokenType], bool, error) {

	// Check if we're at the end of the buffer
	if stream.cursor == len(stream.buffer) {
		if stream.closed {
			return Token[TTokenType]{}, false, nil // Clean EOF
		}

		return Token[TTokenType]{}, false, nil // Need more data
	}

	// --- Start the "munch" from the current cursor ---
	scanPos := stream.cursor
	currentState := uint64(0)
	lastValidToken := stream.invalidToken
	lastValidPos := -1 // -1 = no valid token found yet

	munchLoopBroken := false
	transCursor := autarch.DFACursorGet(stream.stateMachine)

	startOutcome := autarch.DFAStateOutcome(stream.stateMachine, currentState)
	if startOutcome != stream.invalidToken {
		lastValidToken = startOutcome
		lastValidPos = scanPos - 1
	}

	for scanPos < len(stream.buffer) {
		observation := stream.buffer[scanPos]

		nextState, err := autarch.DFAStep(
			stream.stateMachine,
			currentState,
			observation,
			transCursor,
		)

		if err != nil {
			munchLoopBroken = true
			break
		}

		outcome := autarch.DFAStateOutcome(stream.stateMachine, nextState)

		if outcome != stream.invalidToken {
			lastValidToken = outcome
			lastValidPos = scanPos
		} else {
			if nextState == currentState && currentState != 0 {
				munchLoopBroken = true
				break
			}
		}

		// We always advance the state to continue scanning
		currentState = nextState
		scanPos++
	}

	// --- After the munch loop, analyze the result ---

	if !munchLoopBroken && !stream.closed {
		return Token[TTokenType]{}, false, nil
	}

	if lastValidPos == -1 {
		start := stream.cursor
		end := start + 1

		if end > len(stream.buffer) {
			end = len(stream.buffer)
		}

		lexemeRunes := stream.buffer[start:end]
		token := Token[TTokenType]{
			Lexeme:    string(lexemeRunes),
			TokenType: stream.invalidToken,
		}

		stream.cursor = end

		if stream.cursor > 4096 {
			copy(stream.buffer, stream.buffer[stream.cursor:])
			stream.buffer = stream.buffer[:len(stream.buffer)-stream.cursor]
			stream.cursor = 0
		}

		return token, true, nil
	}

	// --- Success: Emit the token ---
	start := stream.cursor
	end := lastValidPos + 1
	lexemeRunes := stream.buffer[start:end]
	token := Token[TTokenType]{
		Lexeme:    string(lexemeRunes),
		TokenType: lastValidToken,
	}

	stream.cursor = end

	if stream.cursor > 4096 {
		copy(stream.buffer, stream.buffer[stream.cursor:])
		stream.buffer = stream.buffer[:len(stream.buffer)-stream.cursor]
		stream.cursor = 0
	}

	return token, true, nil
}

func TextLexerDebug[TTokenType comparable](lexer *TextLexer[TTokenType]) {
	fmt.Println(autarch.DFADebugPrint(lexer.stateMachine))
}
