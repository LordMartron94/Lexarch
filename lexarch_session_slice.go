package lexarch

import (
	"autarch"
	"autarch/pattern"
	"cmp"
	"memstruct"
)

/*
LexerSession maintains the state of a lexing operation, including the current lexer state,
input stream, and position. Multiple sessions can use the same lexer concurrently.

Use cases:
- Tracking position and state during tokenization
- Supporting multiple concurrent lexing operations
- Enabling state transitions during lexing

Time complexity: N/A - data structure
Space complexity: O(1) - stores references and position

Prerequisites:
- Created via LexerSessionCreate
- Input slice must remain valid for session lifetime

Edge cases:
- Position can be modified by Consume operations
- State can be changed via LexerSessionSetState
- Input is stored by reference, so modifications affect lexing
*/
type LexerSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable] struct {
	currentState TState

	input    []TObservation
	position int

	// Position tracking state
	newlineDetector  NewlineDetector[TObservation]
	columnAdvanceFn  ColumnAdvanceFn[TObservation]
	positionTracking positionTrackingStrategy[TObservation]

	currentLine   int // Current line number (1-indexed)
	currentColumn int // Current column number (1-indexed)
	tokenNumber   int // Next token sequence number (1-indexed)

	inUse bool

	lastError *LexingError[TObservation, TToken]

	scanCache  lexerSessionScanCache[TObservation, TToken, TTokenRole]
	dfaCursors map[*autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]]]memstruct.ArrayCursor[uint64]

	liveScanner      sliceScannerLiveContext[TObservation, TState, TToken, TTokenRole]
	simulatedScanner sliceScannerSimulatedContext[TObservation, TState, TToken, TTokenRole]
}

func (s *LexerSession[TObservation, TState, TToken, TTokenRole]) Position() int {
	return s.position
}

type LexerSessionSnapshot[TState comparable] struct {
	State       TState
	Position    int
	Line        int
	Column      int
	TokenNumber int
}

func (s *LexerSession[TObservation, TState, TToken, TTokenRole]) Snapshot() LexerSessionSnapshot[TState] {
	return LexerSessionSnapshot[TState]{
		State:       s.currentState,
		Position:    s.position,
		Line:        s.currentLine,
		Column:      s.currentColumn,
		TokenNumber: s.tokenNumber,
	}
}

func (s *LexerSession[TObservation, TState, TToken, TTokenRole]) RestoreSnapshot(ss LexerSessionSnapshot[TState]) {
	s.currentState = ss.State
	s.position = ss.Position
	s.currentLine = ss.Line
	s.currentColumn = ss.Column
	s.tokenNumber = ss.TokenNumber
	lexerSessionScanCacheResetSoft(&s.scanCache)
}

/*
LexerSessionSetState changes the current lexer state, switching to a different ruleset for
subsequent token recognition. This enables context-sensitive lexing where different tokens
are recognized in different contexts.

Use cases:
- Implementing state-based lexers (e.g., string literals, comments)
- Switching between different token recognition modes
- Handling context-dependent tokenization

Time complexity: O(1)
Space complexity: O(1)

Prerequisites:
- lexerSession must be a valid session
- state must have a corresponding ruleset in the lexer

Edge cases:
- Invalid states will cause errors on next Consume/Peek operation
- State changes take effect immediately for subsequent operations
*/
func LexerSessionSetState[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexerSession *LexerSession[TObservation, TState, TToken, TTokenRole],
	state TState,
) {
	lexerSession.currentState = state
	lexerSessionScanCacheResetSoft(&lexerSession.scanCache)
}

/*
LexerSessionCreate creates a new lexing session with the specified initial state and input stream.
The session tracks position and state for tokenization operations.

Use cases:
- Initializing a new tokenization operation
- Creating multiple concurrent lexing sessions
- Setting up lexer state for input processing

Time complexity: O(1)
Space complexity: O(1) - stores references

Prerequisites:
- initialState must have a corresponding ruleset in the lexer
- input must remain valid for session lifetime

Edge cases:
- Input is stored by reference, so modifications affect lexing
- Position starts at 0
- Session can be reused by resetting position and state
*/
func LexerSessionCreate[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	initialState TState,
	input []TObservation,
	newlineDetector NewlineDetector[TObservation],
	columnAdvanceFn ColumnAdvanceFn[TObservation],
) *LexerSession[TObservation, TState, TToken, TTokenRole] {
	session := &LexerSession[TObservation, TState, TToken, TTokenRole]{
		currentState:    initialState,
		input:           input,
		position:        0,
		newlineDetector: newlineDetector,
		columnAdvanceFn: columnAdvanceFn,
		positionTracking: positionTrackingStrategyGeneric(
			newlineDetector,
			columnAdvanceFn,
		),
		currentLine:   1,
		currentColumn: 1,
		tokenNumber:   1,
		lastError:     nil,
		dfaCursors:    make(map[*autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]]]memstruct.ArrayCursor[uint64]),
	}
	lexerSessionScannerContextsInit(session)
	return session
}

/* Reset allows the same lexer session to be re-used again. */
func (s *LexerSession[TObservation, TState, TToken, TTokenRole]) Reset(
	input []TObservation,
	initialState TState,
) {
	if s.inUse {
		panic("cannot reset active lexer session")
	}

	s.input = input
	s.currentState = initialState
	s.position = 0
	s.currentLine = 1
	s.currentColumn = 1
	s.tokenNumber = 1
	s.lastError = nil
	clear(s.dfaCursors)
	lexerSessionScanCacheResetSoft(&s.scanCache)
	lexerSessionScannerContextsInit(s)
}

func (s *LexerSession[TObservation, TState, TToken, TTokenRole]) GetLastError() *LexingError[TObservation, TToken] {
	return s.lastError
}
