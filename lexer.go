package lexarch

import (
	"autarch"
	"autarch/pattern"
	"cmp"
	"fmt"
	"foundation/domain"
	"memarch"
	"memcore"
	"memforge"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

// ------------------------------------------------------ ERRORS

/* ObservationFormatter turns an observation into a string. */
type ObservationFormatter[TObservation cmp.Ordered] struct {
	FormatOne  func(observation TObservation) string
	FormatMany func(observations []TObservation) string
}

type LexingError[TObservation cmp.Ordered, TToken comparable] struct {
	Position int

	StartLine   int
	StartColumn int

	DFAState    uint64
	HasDFAState bool

	// Furthest DFA progress
	Furthest int

	// What was seen (if any)
	Found    TObservation
	HasFound bool

	// What transitions were possible
	Expected []autarch.SymbolDefinition[TObservation]

	// Best partial matches (by priority / length)
	Candidates []TToken

	Reason LexingErrorReason

	Formatter ObservationFormatter[TObservation]
}

func (e *LexingError[TObservation, TToken]) formatExpected(
	fmtObs func(TObservation) string,
) string {
	if len(e.Expected) == 0 {
		return "<none>"
	}

	var sb strings.Builder
	sb.WriteString("[")

	for i, expectedSymbol := range e.Expected {
		if i > 0 {
			sb.WriteString(", ")
		}

		if expectedSymbol.Observation != nil {
			sb.WriteString(fmtObs(*expectedSymbol.Observation))
		} else {
			sb.WriteString(expectedSymbol.Name)
		}
	}

	sb.WriteString("]")
	return sb.String()
}

func (e *LexingError[TObs, TToken]) Error() string {
	if e == nil {
		return "<nil lexing error>"
	}

	switch e.Reason {

	case LexErrUnexpectedEOF:
		var stateStr string
		if e.HasDFAState {
			stateStr = fmt.Sprintf("%d", e.DFAState)
		} else {
			stateStr = "?"
		}

		return fmt.Sprintf(
			"unexpected EOF at line %d:%d (expected %s) (absolute position %d) [dfaState=%s]",
			e.StartLine,
			e.StartColumn,
			e.formatExpected(e.Formatter.FormatOne),
			e.Position,
			stateStr,
		)

	case LexErrNoTransition:
		var stateStr string
		if e.HasDFAState {
			stateStr = fmt.Sprintf("%d", e.DFAState)
		} else {
			stateStr = "?"
		}

		if e.HasFound {
			return fmt.Sprintf(
				"unexpected %s at line %d:%d (expected %s) [dfaState=%s]",
				e.Formatter.FormatOne(e.Found),
				e.StartLine,
				e.StartColumn,
				e.formatExpected(e.Formatter.FormatOne),
				stateStr,
			)
		}

		return fmt.Sprintf(
			"invalid input at line %d:%d (expected %s) [dfaState=%s]",
			e.StartLine,
			e.StartColumn,
			e.formatExpected(e.Formatter.FormatOne),
			stateStr,
		)

	case LexErrBufferLimit:
		return "lexer buffer limit exceeded"

	default:
		return "lexing error"
	}
}

type LexingErrorReason int

const (
	LexErrNoTransition LexingErrorReason = iota
	LexErrUnexpectedEOF
	LexErrBufferLimit
)

// ------------------------------------------------------ RULES

type lexingRule[TObservation cmp.Ordered, TToken, TTokenRole comparable] struct {
	pattern  pattern.RegulaAST[TObservation]
	token    TToken
	role     TTokenRole
	priority int
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
			offset := (col - 1) % tabWidth
			return col + (tabWidth - offset)
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

// ------------------------------------------------------ LEXER

/*
Lexeme represents a recognized token from the input stream, containing the raw observation sequence,
the token type, and position information.

Use cases:
- Returning tokenized results from lexer operations
- Providing input to parsers and syntax analyzers
- Debugging and error reporting with position information

Time complexity: N/A - data structure
Space complexity: O(n) where n is the length of Raw

Prerequisites:
- Created by lexer operations (Consume, Peek, etc.)

Edge cases:
- Raw is a slice into the original input, so lifetime depends on input lifetime
- Raw is nil for EOF tokens
- Start and End are byte/observation positions in the input
- For EOF tokens, Start and End both equal len(input)
- Token may be the error token if recognition failed
- Token may be the EOF token when end of input is reached
*/
type Lexeme[TObservation cmp.Ordered, TToken, TTokenRole comparable] struct {
	Raw   []TObservation
	Token TToken

	Start, End int

	// StartColumn inclusive, EndColumn exclusive (half-open span)

	// Position information (1-indexed)
	StartLine   int // Line number where token starts
	StartColumn int // Column number where token starts
	EndLine     int // Line number where token ends
	EndColumn   int // Column number where token ends
	TokenNumber int // Sequence number of this token

	Role TTokenRole

	formatter ObservationFormatter[TObservation]
}

func (l Lexeme[TObservation, TToken, TTokenRole]) DebugString(
	fmtToken func(TToken) string,
	fmtRole func(TTokenRole) string,
) string {

	tokenStr := ""
	if fmtToken != nil {
		tokenStr = fmtToken(l.Token)
	} else {
		tokenStr = fmt.Sprintf("%v", l.Token)
	}

	roleStr := ""
	if fmtRole != nil {
		roleStr = fmtRole(l.Role)
	} else {
		roleStr = fmt.Sprintf("%v", l.Role)
	}

	rawStr := l.FormatRawDiagnostic()

	return fmt.Sprintf(
		"Lexeme{token=%s, role=%s, raw=%q, span=[%d:%d], pos=(%d:%d → %d:%d), #=%d}",
		tokenStr,
		roleStr,
		rawStr,
		l.Start,
		l.End,
		l.StartLine,
		l.StartColumn,
		l.EndLine,
		l.EndColumn,
		l.TokenNumber,
	)
}

func (l *Lexeme[TObs, TToken, TTokenRole]) FormatRawDiagnostic() string {
	if len(l.Raw) == 0 {
		return ""
	}

	return l.formatter.FormatMany(l.Raw)
}

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
type LexerSession[TObservation cmp.Ordered, TState, TToken comparable] struct {
	currentState TState

	input    []TObservation
	position int

	// Position tracking state
	newlineDetector NewlineDetector[TObservation]
	columnAdvanceFn ColumnAdvanceFn[TObservation]

	currentLine   int // Current line number (1-indexed)
	currentColumn int // Current column number (1-indexed)
	tokenNumber   int // Next token sequence number (1-indexed)

	inUse atomic.Bool

	lastError *LexingError[TObservation, TToken]
}

func (s *LexerSession[TObservation, TState, TToken]) begin() {
	if !s.inUse.CompareAndSwap(false, true) {
		panic("LexerSession is already in use (concurrent or re-entrant use detected)")
	}
}

func (s *LexerSession[TObservation, TState, TToken]) end() {
	s.inUse.Store(false)
}

func (s *LexerSession[TObservation, TState, TToken]) Position() int {
	return s.position
}

type LexerSessionSnapshot[TState comparable] struct {
	State       TState
	Position    int
	Line        int
	Column      int
	TokenNumber int
}

func (s *LexerSession[TObservation, TState, TToken]) Snapshot() LexerSessionSnapshot[TState] {
	return LexerSessionSnapshot[TState]{
		State:       s.currentState,
		Position:    s.position,
		Line:        s.currentLine,
		Column:      s.currentColumn,
		TokenNumber: s.tokenNumber,
	}
}

func (s *LexerSession[TObservation, TState, TToken]) RestoreSnapshot(ss LexerSessionSnapshot[TState]) {
	s.currentState = ss.State
	s.position = ss.Position
	s.currentLine = ss.Line
	s.currentColumn = ss.Column
	s.tokenNumber = ss.TokenNumber
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
func LexerSessionSetState[TObservation cmp.Ordered, TState, TToken comparable](lexerSession *LexerSession[TObservation, TState, TToken], state TState) {
	lexerSession.currentState = state
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
func LexerSessionCreate[TObservation cmp.Ordered, TState, TToken comparable](
	initialState TState,
	input []TObservation,
	newlineDetector NewlineDetector[TObservation],
	columnAdvanceFn ColumnAdvanceFn[TObservation],
) *LexerSession[TObservation, TState, TToken] {
	return &LexerSession[TObservation, TState, TToken]{
		currentState:    initialState,
		input:           input,
		position:        0,
		newlineDetector: newlineDetector,
		columnAdvanceFn: columnAdvanceFn,
		currentLine:     1,
		currentColumn:   1,
		tokenNumber:     1,
		lastError:       nil,
	}
}

/* Reset allows the same lexer session to be re-used again. */
func (s *LexerSession[TObservation, TState, TToken]) Reset(
	input []TObservation,
	initialState TState,
) {
	if s.inUse.Load() {
		panic("cannot reset active lexer session")
	}

	s.input = input
	s.currentState = initialState
	s.position = 0
	s.currentLine = 1
	s.currentColumn = 1
	s.tokenNumber = 1
	s.lastError = nil
}

func (s *LexerSession[TObservation, TState, TToken]) GetLastError() *LexingError[TObservation, TToken] {
	return s.lastError
}

/*
ObservationProducerFn streams observations into dst and reports whether EOF was reached.

Contract:
  - The producer writes up to len(dst) observations into dst and returns n.
  - If eof is true, no more observations will ever be produced after this call.
  - If err is non-nil, n must be 0 and the session/lexing operation should fail.
  - It is valid to return (0, false, nil) to indicate "no data available yet";
    callers should treat this as a blocking/producer choice (the lexer will retry
    only as needed during scanning).

Use cases:
- Streaming lexing from io.Reader (bytes) or bufio.Reader (runes)
- Incremental lexing of network streams or pipes
- Tokenizing data that is too large to materialize as a single slice

Time complexity: O(1) per call from the lexer’s perspective (producer-defined)
Space complexity: O(1) for the function itself

Edge cases:
- Returning n<0 or n>len(dst) is invalid and will be treated as an error by the session.
- Returning (n>0, eof=true, nil) indicates the last chunk was delivered.
*/
type ObservationProducerFn[TObservation cmp.Ordered] func(dst []TObservation) (n int, eof bool, err error)

/*
StreamingLexerSession maintains lexing state for streaming input. It mirrors LexerSession,
but sources observations lazily via a producer callback and buffers unread observations.

Use cases:
- Tokenizing large inputs without holding the entire input in memory
- Online lexing while reading from a stream
- Protocol parsing where input arrives incrementally

Time complexity: N/A - data structure
Space complexity: O(b) where b is the current buffered unread observations

Prerequisites:
- Created via StreamingLexerSessionCreate
- producer must obey ObservationProducerFn contract
- newlineDetector must be appropriate for the observation type

Edge cases:
  - If maxBufferedObservations is too small for the longest token in the language,
    scanning can fail with an explicit buffer limit error.
*/
type StreamingLexerSession[TObservation cmp.Ordered, TState, TToken comparable] struct {
	currentState TState

	producer ObservationProducerFn[TObservation]
	eof      bool

	// unread buffered observations starting at logical position 0
	buffer []TObservation

	// absolute position (count of consumed observations since start)
	absPos int

	// Buffer policy
	readChunkSize           int
	maxBufferedObservations int

	// Position tracking state
	newlineDetector NewlineDetector[TObservation]
	columnAdvanceFn ColumnAdvanceFn[TObservation]

	currentLine   int // Current line number (1-indexed)
	currentColumn int // Current column number (1-indexed)
	tokenNumber   int // Next token sequence number (1-indexed)

	inUse atomic.Bool

	lastError *LexingError[TObservation, TToken]
}

func (s *StreamingLexerSession[TObservation, TState, TToken]) begin() {
	if !s.inUse.CompareAndSwap(false, true) {
		panic("LexerSession is already in use (concurrent or re-entrant use detected)")
	}
}

func (s *StreamingLexerSession[TObservation, TState, TToken]) end() {
	s.inUse.Store(false)
}

func (s *StreamingLexerSession[TObservation, TState, TToken]) AbsPosition() int {
	return s.absPos
}

type StreamingLexerSessionSnapshot[TObservation cmp.Ordered, TState comparable] struct {
	State       TState
	AbsPos      int
	Line        int
	Column      int
	TokenNumber int
	EOF         bool
	Buffer      []TObservation
}

func (s *StreamingLexerSession[TObservation, TState, TToken]) Snapshot() StreamingLexerSessionSnapshot[TObservation, TState] {
	bufCopy := make([]TObservation, len(s.buffer))
	copy(bufCopy, s.buffer)

	return StreamingLexerSessionSnapshot[TObservation, TState]{
		State:       s.currentState,
		AbsPos:      s.absPos,
		Line:        s.currentLine,
		Column:      s.currentColumn,
		TokenNumber: s.tokenNumber,
		EOF:         s.eof,
		Buffer:      bufCopy,
	}
}

func (s *StreamingLexerSession[TObservation, TState, TToken]) RestoreSnapshot(
	snap StreamingLexerSessionSnapshot[TObservation, TState],
) {
	s.currentState = snap.State
	s.absPos = snap.AbsPos
	s.currentLine = snap.Line
	s.currentColumn = snap.Column
	s.tokenNumber = snap.TokenNumber
	s.eof = snap.EOF

	s.buffer = make([]TObservation, len(snap.Buffer))
	copy(s.buffer, snap.Buffer)
}

/*
StreamingLexerSessionCreate creates a new streaming lexing session.

Parameters:
- initialState: initial lexer state
- producer: callback used to stream observations into the session
- newlineDetector: detects newlines for line/column tracking
- readChunkSize: how many observations to request per producer call (must be > 0)
- maxBufferedObservations: hard cap on buffered unread observations (must be > 0)

Use cases:
- Creating a session over an io.Reader-backed producer
- Enforcing bounded memory while lexing streams
- Controlling read granularity for performance

Time complexity: O(1)
Space complexity: O(1) (buffer allocates lazily)

Edge cases:
- Panics if producer is nil
- Panics if readChunkSize <= 0
- Panics if maxBufferedObservations <= 0
*/
func StreamingLexerSessionCreate[TObservation cmp.Ordered, TState, TToken comparable](
	initialState TState,
	producer ObservationProducerFn[TObservation],
	newlineDetector NewlineDetector[TObservation],
	columnAdvanceFn ColumnAdvanceFn[TObservation],
	readChunkSize int,
	maxBufferedObservations int,
) *StreamingLexerSession[TObservation, TState, TToken] {
	if producer == nil {
		panic("producer must not be nil")
	}
	if readChunkSize <= 0 {
		panic("readChunkSize must be > 0")
	}
	if maxBufferedObservations <= 0 {
		panic("maxBufferedObservations must be > 0")
	}

	return &StreamingLexerSession[TObservation, TState, TToken]{
		currentState:            initialState,
		producer:                producer,
		eof:                     false,
		buffer:                  make([]TObservation, 0, min(readChunkSize, maxBufferedObservations)),
		absPos:                  0,
		readChunkSize:           readChunkSize,
		maxBufferedObservations: maxBufferedObservations,
		newlineDetector:         newlineDetector,
		columnAdvanceFn:         columnAdvanceFn,
		currentLine:             1,
		currentColumn:           1,
		tokenNumber:             1,
		lastError:               nil,
	}
}

/* Reset allows the same lexer streaming-session to be re-used again. */
func (s *StreamingLexerSession[TObservation, TState, TToken]) Reset(
	producer ObservationProducerFn[TObservation],
	initialState TState,
) {
	if s.inUse.Load() {
		panic("cannot reset active lexer session")
	}

	s.producer = producer
	s.currentState = initialState
	s.absPos = 0
	s.currentLine = 1
	s.currentColumn = 1
	s.tokenNumber = 1
	s.eof = false
	s.buffer = s.buffer[:0]
	s.lastError = nil
}

/*
StreamingLexerSessionSetState changes the current lexer state for a streaming session.

Use cases:
- Context-sensitive lexing over streams (strings/comments/etc.)
- Switching recognition modes mid-stream

Time complexity: O(1)
Space complexity: O(1)
*/
func StreamingLexerSessionSetState[TObservation cmp.Ordered, TState, TToken comparable](
	session *StreamingLexerSession[TObservation, TState, TToken],
	state TState,
) {
	session.currentState = state
}

func (s *StreamingLexerSession[TObservation, TState, TToken]) GetLastError() *LexingError[TObservation, TToken] {
	return s.lastError
}

/*
Lexer is the main lexical analyzer that contains compiled DFAs for each state and provides
tokenization operations. All patterns are compiled to minimized DFAs at creation time for
optimal runtime performance.

Use cases:
- Tokenizing input streams into sequences of tokens
- Building parsers and language processors
- Implementing lexical analysis for programming languages, protocols, or data formats

Time complexity: N/A - data structure
Space complexity: O(s * a) where s is states and a is alphabet size

Prerequisites:
- Created via LexerCreate with valid rulesets
- Must be closed via LexerClose to free resources

Edge cases:
- Holds references to allocated DFAs, so must be closed to prevent leaks
- Error token is returned when no pattern matches
- EOF token is returned when end of input is reached
- Multiple sessions can use the same lexer concurrently
*/
type Lexer[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable] struct {
	ruleSets         map[TState]*autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]]
	tokenResolutions map[TState]TokenResolutionStepFn[TToken]

	nonTerminalOutcome TokenOutcome[TToken, TTokenRole]

	dfaAllocator memcore.MarkRaw
	eofToken     TToken

	formatter ObservationFormatter[TObservation]
}

/* ObservationCTX encapsulates the context for observation handling. */
type ObservationCTX[TObservation cmp.Ordered] struct {
	formatter         ObservationFormatter[TObservation]
	observationDomain *domain.DiscreteDomain[TObservation]
	toBytes           func(observations []TObservation) []byte
}

func ObservationCTXCreate[TObservation cmp.Ordered](
	formatter ObservationFormatter[TObservation],
	observationDomain *domain.DiscreteDomain[TObservation],
	toBytes func(observations []TObservation) []byte,
) ObservationCTX[TObservation] {
	return ObservationCTX[TObservation]{
		formatter:         formatter,
		observationDomain: observationDomain,
		toBytes:           toBytes,
	}
}

/* LexarchRuneDomain returns the canonical discrete domain for rune observations (Unicode code points). */
func LexarchRuneDomain() *domain.DiscreteDomain[rune] {
	return domain.DiscreteDomainRuneCreate()
}

/*
RunesToBytesDefault returns a UTF-8 encoder for rune observation streams.

This provides a canonical, lossless projection from abstract rune symbols
to concrete byte representation suitable for hashing, debugging, and
automaton compilation internals.

Properties:
  - Unicode-correct
  - Deterministic
  - Order-preserving
  - Minimal encoding (UTF-8)

Time complexity: O(n)
Space complexity: O(n)
*/
func RunesToBytesDefault() func(observations []rune) []byte {
	return func(observations []rune) []byte {
		if len(observations) == 0 {
			return nil
		}

		// Worst case: 4 bytes per rune (UTF-8 max width)
		buf := make([]byte, 0, len(observations)*utf8.UTFMax)

		var tmp [utf8.UTFMax]byte

		for _, r := range observations {
			n := utf8.EncodeRune(tmp[:], r)
			buf = append(buf, tmp[:n]...)
		}

		return buf
	}
}

//go:generate stringer -type CompilerMode
type CompilerMode int

const (
	Thompson CompilerMode = iota + 1
	Glushkov
)

/*
LexerCreate compiles a set of rulesets into a ready-to-use lexer. Each state's ruleset is
compiled to a minimized DFA for efficient token recognition. The compilation process:
1. Converts each pattern to an NFA
2. Merges NFAs for each ruleset using alternation
3. Converts to DFA via subset construction
4. Minimizes the DFA using Hopcroft's algorithm

Use cases:
- Building lexers from pattern definitions
- Creating reusable tokenization engines
- Initializing lexical analyzers for parsers

Time complexity: O(2^n * a) worst case for NFA-to-DFA conversion per ruleset, where n is NFA states and a is alphabet size
Space complexity: O(s * a) where s is DFA states and a is alphabet size

Prerequisites:
- inputRulesets must contain at least one state
- Each ruleset must contain at least one rule
- errorToken must be distinct from all valid token values
- eofToken must be distinct from all valid token values and errorToken
- scratchAllocationFn must be a valid allocation function
- maxDFAAllocatorMemory must be sufficient for DFA storage

Edge cases:
- Panics if DFA allocator exceeds maxDFAAllocatorMemory
- Empty rulesets create DFAs that never accept
- Rules are evaluated with longest match priority
- The lexer must be closed via LexerClose to free resources
- EOF token is returned when position reaches end of input
*/
func LexerCreate[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	inputRulesets map[TState]LexingRuleset[TObservation, TToken, TTokenRole],
	eofToken TToken,
	scratchAllocationFn memarch.AllocationFn,
	maxDFAAllocatorMemory memcore.MemoryUnitBytes,
	observationCtx ObservationCTX[TObservation],
	compilationMode CompilerMode,
) *Lexer[TObservation, TState, TToken, TTokenRole] {
	dfaAllocator := memforge.DynamicLinearAllocatorCreateFunction(uint64(memcore.KiloByte), func(currentCap, neededCap uint64) uint64 {
		newSize := max(currentCap*2, neededCap)

		if newSize > uint64(maxDFAAllocatorMemory) {
			panic("dfa allocator consumes too much memory")
		}

		return newSize
	})

	lexerRulesets := make(map[TState]*autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]])
	tokenResolutions := make(map[TState]TokenResolutionStepFn[TToken])

	for state, ruleset := range inputRulesets {
		var compiler pattern.RegulaToNFACompiler[TObservation, TokenOutcome[TToken, TTokenRole]]
		switch compilationMode {
		case Thompson:
			compiler = pattern.RegulaCompileToNFAThompson
		case Glushkov:
			compiler = pattern.RegulaCompileToNFAGlushkov
		default:
			panic("unknown compilation mode")
		}

		var nonTerminalOutcome TokenOutcome[TToken, TTokenRole]

		compiled := lexingRulesetCompile(ruleset, scratchAllocationFn, func(sizeBytes, alignment uint64) memcore.MarkRaw {
			return memforge.DynamicLinearAllocatorMallocUnsafe(dfaAllocator, sizeBytes, alignment)
		}, observationCtx.observationDomain, observationCtx.toBytes, compiler, nonTerminalOutcome)

		lexerRulesets[state] = compiled

		// Store resolution function (default to longest if not set)
		if ruleset.tokenResolutionStep != nil {
			tokenResolutions[state] = ruleset.tokenResolutionStep
		} else {
			tokenResolutions[state] = TokenResolutionStepLongest[TToken]
		}
	}

	return &Lexer[TObservation, TState, TToken, TTokenRole]{
		ruleSets:           lexerRulesets,
		tokenResolutions:   tokenResolutions,
		nonTerminalOutcome: TokenOutcome[TToken, TTokenRole]{},
		dfaAllocator:       dfaAllocator,
		eofToken:           eofToken,
		formatter:          observationCtx.formatter,
	}
}

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

/*
LexerClose releases all resources associated with the lexer, including DFA memory and allocators.
After closing, the lexer must not be used.

Use cases:
- Cleaning up lexer resources
- Preventing memory leaks in long-running applications
- Proper resource management

Time complexity: O(1) - destroys allocator
Space complexity: O(1) - frees all allocated memory

Prerequisites:
- lexer must be a valid lexer created via LexerCreate
- No active sessions should be using the lexer

Edge cases:
- Safe to call multiple times (idempotent after first call)
- All DFAs become invalid after closing
- Sessions using this lexer will fail on subsequent operations
*/
func LexerClose[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](lexer *Lexer[TObservation, TState, TToken, TTokenRole]) {
	memforge.DynamicLinearAllocatorDestroy(lexer.dfaAllocator)
}

/*
LexerConsume recognizes and consumes the next token.

If an error occurs or the session already has an error, it returns an EOF lexeme
and sets session.lastError.

Time complexity: O(length of matched token)
*/
func LexerConsume[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
) Lexeme[TObservation, TToken, TTokenRole] {
	session.begin()
	defer session.end()

	if session.lastError != nil {
		return lexerBuildEOFSession(lexer, session)
	}

	if eofLexeme, atEOF := lexerCheckEOF(lexer, session); atEOF {
		return eofLexeme
	}

	dfa, resolutionStep, err := lexerGetDFAAndResolution(lexer, session.currentState)
	if err != nil {
		lexErr := &LexingError[TObservation, TToken]{
			Position:    session.position,
			StartLine:   session.currentLine,
			StartColumn: session.currentColumn,
			Reason:      LexErrNoTransition,
			Formatter:   lexer.formatter,
		}
		session.lastError = lexErr
		return lexerBuildEOFSession(lexer, session)
	}

	next := func(i int) (TObservation, bool, error) {
		pos := session.position + i
		if pos >= len(session.input) {
			var zero TObservation
			return zero, false, nil
		}
		return session.input[pos], true, nil
	}

	token, tokenRole, endRel, found, _, lexErr := scanCore(dfa, next, resolutionStep, true, lexer.nonTerminalOutcome)
	if lexErr != nil {
		lexErr.Position = session.position + lexErr.Position
		lexErr.Furthest = session.position + lexErr.Furthest
		lexErr.Formatter = lexer.formatter

		line, col := computePositionFromSlice(
			session.input[session.position:lexErr.Position],
			session.newlineDetector,
			session.columnAdvanceFn,
			session.currentLine,
			session.currentColumn,
		)
		lexErr.StartLine = line
		lexErr.StartColumn = col

		session.lastError = lexErr
		return lexerBuildEOFSession(lexer, session)
	}

	if !found {
		return lexerBuildEOFSession(lexer, session)
	}

	start := session.position
	end := session.position + endRel
	raw := copyRaw(session.input[start:end])

	endLine, endCol := computePositionFromSlice(
		raw,
		session.newlineDetector,
		session.columnAdvanceFn,
		session.currentLine,
		session.currentColumn,
	)

	lex := lexemeBuild(
		lexer.formatter,
		token,
		raw,
		start,
		end,
		session.currentLine,
		session.currentColumn,
		endLine,
		endCol,
		session.tokenNumber,
		tokenRole,
	)

	session.tokenNumber++
	session.position = end
	session.currentLine = lex.EndLine
	session.currentColumn = lex.EndColumn

	return lex
}

/*
LexerConsumeRange consumes and returns up to `count` tokens.

If an error occurs, the returned slice contains tokens found up to the error point.
The session state is updated to reflect the last successfully consumed token.
*/
func LexerConsumeRange[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	session.begin()
	defer session.end()

	if count <= 0 || session.lastError != nil {
		return nil
	}

	ctx := scannerFromSlice(session)
	lexemes, err := lexerPeekRangeCore(
		lexer, ctx, session.currentState, session.newlineDetector, session.columnAdvanceFn,
		session.currentLine, session.currentColumn, session.tokenNumber, count,
	)

	if err != nil {
		err.Formatter = lexer.formatter
		session.lastError = err
	}

	if len(lexemes) > 0 {
		last := lexemes[len(lexemes)-1]
		session.currentLine = last.EndLine
		session.currentColumn = last.EndColumn
		session.tokenNumber += len(lexemes)
		session.position = last.End
	}

	return lexemes
}

/*
LexerPeek returns the n-th token ahead without consuming input.
*/
func LexerPeek[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
	n int,
) Lexeme[TObservation, TToken, TTokenRole] {
	session.begin()
	defer session.end()

	if n < 0 || session.lastError != nil {
		return lexerBuildEOFSession(lexer, session)
	}

	ctx := scannerFromSliceSimulated(session)
	lexemes, err := lexerPeekRangeCore(
		lexer, ctx, session.currentState, session.newlineDetector, session.columnAdvanceFn,
		session.currentLine, session.currentColumn, session.tokenNumber, n+1,
	)

	if err != nil {
		err.Formatter = lexer.formatter
		session.lastError = err
		return lexerBuildEOFSession(lexer, session)
	}

	if len(lexemes) <= n {
		return lexerBuildEOFSession(lexer, session)
	}

	return lexemes[n]
}

/*
LexerPeekRange returns up to `count` upcoming tokens without consuming input.
*/
func LexerPeekRange[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	session.begin()
	defer session.end()

	if count <= 0 || session.lastError != nil {
		return nil
	}

	ctx := scannerFromSliceSimulated(session)
	lexemes, err := lexerPeekRangeCore(
		lexer, ctx, session.currentState, session.newlineDetector, session.columnAdvanceFn,
		session.currentLine, session.currentColumn, session.tokenNumber, count,
	)

	if err != nil {
		err.Formatter = lexer.formatter
		session.lastError = err
	}

	return lexemes
}

/*
LexerAssertConsume consumes the next token and verifies it matches the expected type.
*/
func LexerAssertConsume[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
	expected TToken,
) Lexeme[TObservation, TToken, TTokenRole] {
	lex := LexerConsume(lexer, session)

	if session.lastError == nil && lex.Token != expected {
		session.lastError = &LexingError[TObservation, TToken]{
			Position:    lex.Start,
			StartLine:   lex.StartLine,
			StartColumn: lex.StartColumn,
			Reason:      LexErrNoTransition,
			Formatter:   lexer.formatter,
		}
		return lexerBuildEOFSession(lexer, session)
	}

	return lex
}

/*
LexerAssertPeek peeks at the n-th token and verifies it matches the expected type.
*/
func LexerAssertPeek[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
	expected TToken,
	n int,
) Lexeme[TObservation, TToken, TTokenRole] {
	lex := LexerPeek(lexer, session, n)

	if session.lastError == nil && lex.Token != expected {
		session.lastError = &LexingError[TObservation, TToken]{
			Position:    lex.Start,
			StartLine:   lex.StartLine,
			StartColumn: lex.StartColumn,
			Reason:      LexErrNoTransition,
			Formatter:   lexer.formatter,
		}
		return lexerBuildEOFSession(lexer, session)
	}

	return lex
}

/*
LexerConsumeStreaming consumes the next token from a streaming session.
*/
func LexerConsumeStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
) Lexeme[TObservation, TToken, TTokenRole] {
	session.begin()
	defer session.end()

	if session.lastError != nil {
		return lexerBuildEOFSessionStream(lexer, session)
	}

	if eofLexeme, atEOF := lexerCheckEOFStreaming(lexer, session); atEOF {
		return eofLexeme
	}

	dfa, resolutionStep, err := lexerGetDFAAndResolution(lexer, session.currentState)
	if err != nil {
		session.lastError = &LexingError[TObservation, TToken]{
			Position:    session.absPos,
			StartLine:   session.currentLine,
			StartColumn: session.currentColumn,
			Reason:      LexErrNoTransition,
			Formatter:   lexer.formatter,
		}
		return lexerBuildEOFSessionStream(lexer, session)
	}

	next := streamingNextFn(session)
	token, role, endRel, found, _, lexErr := scanCore(dfa, next, resolutionStep, true, lexer.nonTerminalOutcome)

	if lexErr != nil {
		lexErr.Formatter = lexer.formatter
		session.lastError = lexErr
		return lexerBuildEOFSessionStream(lexer, session)
	}

	if !found {
		return lexerBuildEOFSessionStream(lexer, session)
	}

	var raw []TObservation
	if endRel > 0 && endRel <= len(session.buffer) {
		raw = copyRaw(session.buffer[:endRel])
	}

	endLine, endCol := computePositionFromSlice(raw, session.newlineDetector, session.columnAdvanceFn, session.currentLine, session.currentColumn)

	lex := lexemeBuild(lexer.formatter, token, raw, session.absPos, session.absPos+endRel, session.currentLine, session.currentColumn, endLine, endCol, session.tokenNumber, role)

	session.tokenNumber++
	session.absPos += endRel
	session.currentLine = lex.EndLine
	session.currentColumn = lex.EndColumn
	session.buffer = session.buffer[endRel:]

	streamingMaybeCompact(session)
	return lex
}

/*
LexerConsumeRangeStreaming consumes and returns up to `count` tokens from a streaming session.
*/
func LexerConsumeRangeStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	session.begin()
	defer session.end()

	if count <= 0 || session.lastError != nil {
		return nil
	}

	ctx := scannerFromStreaming(session)
	lexemes, err := lexerPeekRangeCore(
		lexer, ctx, session.currentState, session.newlineDetector, session.columnAdvanceFn,
		session.currentLine, session.currentColumn, session.tokenNumber, count,
	)

	if err != nil {
		err.Formatter = lexer.formatter
		session.lastError = err
	}

	if len(lexemes) > 0 {
		last := lexemes[len(lexemes)-1]
		session.currentLine = last.EndLine
		session.currentColumn = last.EndColumn
		session.tokenNumber += len(lexemes)
	}

	return lexemes
}

/*
LexerPeekStreaming performs lookahead on a streaming session.
*/
func LexerPeekStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
	n int,
) Lexeme[TObservation, TToken, TTokenRole] {
	if n < 0 || session.lastError != nil {
		return lexerBuildEOFSessionStream(lexer, session)
	}

	lexemes := LexerPeekRangeStreaming(lexer, session, n+1)
	if n >= len(lexemes) {
		return lexerBuildEOFSessionStream(lexer, session)
	}

	return lexemes[n]
}

/*
LexerPeekRangeStreaming returns up to `count` upcoming tokens without consuming input.
*/
func LexerPeekRangeStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	session.begin()
	defer session.end()

	if count <= 0 || session.lastError != nil {
		return nil
	}

	ctx := scannerFromStreamingSimulated(session)
	lexemes, err := lexerPeekRangeCore(
		lexer, ctx, session.currentState, session.newlineDetector, session.columnAdvanceFn,
		session.currentLine, session.currentColumn, session.tokenNumber, count,
	)

	if err != nil {
		err.Formatter = lexer.formatter
		session.lastError = err
	}

	return lexemes
}

/*
LexerAssertConsumeStreaming consumes and verifies the next token from a streaming session.
*/
func LexerAssertConsumeStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
	expected TToken,
) Lexeme[TObservation, TToken, TTokenRole] {
	lex := LexerConsumeStreaming(lexer, session)

	if session.lastError == nil && lex.Token != expected {
		session.lastError = &LexingError[TObservation, TToken]{
			Position:    lex.Start,
			StartLine:   lex.StartLine,
			StartColumn: lex.StartColumn,
			Reason:      LexErrNoTransition,
			Formatter:   lexer.formatter,
		}
		return lexerBuildEOFSessionStream(lexer, session)
	}

	return lex
}

/*
LexerAssertPeekStreaming peeks and verifies the next token from a streaming session.
*/
func LexerAssertPeekStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
	expected TToken,
	n int,
) Lexeme[TObservation, TToken, TTokenRole] {
	lex := LexerPeekStreaming(lexer, session, n)

	if session.lastError == nil && lex.Token != expected {
		session.lastError = &LexingError[TObservation, TToken]{
			Position:    lex.Start,
			StartLine:   lex.StartLine,
			StartColumn: lex.StartColumn,
			Reason:      LexErrNoTransition,
			Formatter:   lexer.formatter,
		}
		return lexerBuildEOFSessionStream(lexer, session)
	}

	return lex
}

// ------------------------------------------------------ PRIVATE HELPERS

func lexerGetDFAAndResolution[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	state TState,
) (*autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]], TokenResolutionStepFn[TToken], error) {

	dfa, ok := lexer.ruleSets[state]
	if !ok {
		return nil, nil, fmt.Errorf("no ruleset for state %v", state)
	}

	resolutionStep, ok := lexer.tokenResolutions[state]
	if !ok || resolutionStep == nil {
		resolutionStep = TokenResolutionStepLongest[TToken]
	}

	return dfa, resolutionStep, nil
}

func lexemeBuild[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	observationFormatter ObservationFormatter[TObservation],
	token TToken,
	raw []TObservation,
	start, end int,
	startLine, startColumn int,
	endLine, endColumn int,
	tokenNumber int,
	role TTokenRole,
) Lexeme[TObservation, TToken, TTokenRole] {
	return Lexeme[TObservation, TToken, TTokenRole]{
		formatter:   observationFormatter,
		Token:       token,
		Raw:         raw,
		Start:       start,
		End:         end,
		StartLine:   startLine,
		StartColumn: startColumn,
		EndLine:     endLine,
		EndColumn:   endColumn,
		TokenNumber: tokenNumber,
		Role:        role,
	}

}

func lexemeEOF[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	eofToken TToken,
	pos int,
	line int,
	col int,
	tokenNum int,
) Lexeme[TObservation, TToken, TTokenRole] {
	return Lexeme[TObservation, TToken, TTokenRole]{
		Token:       eofToken,
		Raw:         nil,
		Start:       pos,
		End:         pos,
		StartLine:   line,
		StartColumn: col,
		EndLine:     line,
		EndColumn:   col,
		TokenNumber: tokenNum,
	}
}

func streamingNextFn[TObservation cmp.Ordered, TState, TToken comparable](
	session *StreamingLexerSession[TObservation, TState, TToken],
) func(int) (TObservation, bool, error) {

	return func(i int) (TObservation, bool, error) {
		if err := streamingEnsureAt(session, i); err != nil {
			var zero TObservation
			return zero, false, err
		}
		if i >= len(session.buffer) {
			var zero TObservation
			return zero, false, nil
		}
		return session.buffer[i], true, nil
	}
}

func computePositionFromSlice[TObservation cmp.Ordered](
	observations []TObservation,
	newlineDetector NewlineDetector[TObservation],
	advanceColumn ColumnAdvanceFn[TObservation],
	startLine, startColumn int,
) (endLine, endColumn int) {

	line := startLine
	column := startColumn

	for _, obs := range observations {
		if newlineDetector(obs) {
			line++
			column = 1
		} else {
			column = advanceColumn(obs, column)
		}
	}

	return line, column
}

func lexerCheckEOF[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
) (Lexeme[TObservation, TToken, TTokenRole], bool) {
	// fmt.Printf("Checking: pos=%05d, max=%05d; EOF? %v\n", session.position, len(session.input), session.position >= len(session.input))

	if session.position >= len(session.input) {
		return lexerBuildEOF(
			lexer,
			len(session.input),
			session.currentLine,
			session.currentColumn,
			session.tokenNumber,
		), true
	}
	var zero Lexeme[TObservation, TToken, TTokenRole]
	return zero, false
}

func lexerBuildEOFSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
) Lexeme[TObservation, TToken, TTokenRole] {
	return lexerBuildEOF(lexer, session.position, session.currentLine, session.currentColumn, session.tokenNumber)
}

func lexerBuildEOFSessionStream[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
) Lexeme[TObservation, TToken, TTokenRole] {
	return lexerBuildEOF(lexer, session.absPos, session.currentLine, session.currentColumn, session.tokenNumber)
}

func lexerBuildEOF[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	pos int,
	line int,
	col int,
	tokenNum int,
) Lexeme[TObservation, TToken, TTokenRole] {
	return Lexeme[TObservation, TToken, TTokenRole]{
		Token:       lexer.eofToken,
		Raw:         nil,
		Start:       pos,
		End:         pos,
		StartLine:   line,
		StartColumn: col,
		EndLine:     line,
		EndColumn:   col,
		TokenNumber: tokenNum,
	}
}

func scanCore[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	dfa *autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]],
	nextObservation func(int) (obs TObservation, ok bool, err error),
	resolutionStep TokenResolutionStepFn[TToken],
	strictEOF bool,
	nonTerminalOutcome TokenOutcome[TToken, TTokenRole],
) (bestToken TToken, role TTokenRole, bestEnd int, found bool, dfaState uint64, lexErr *LexingError[TObservation, TToken]) {

	cursor := autarch.DFACursorGet(dfa)

	state := uint64(0)
	pos := 0

	bestPriority := 0
	var bestRole TTokenRole

	// Diagnostic tracking
	furthestPos := 0

	for {
		obs, hasObs, err := nextObservation(pos)
		if err != nil {
			return bestToken, bestRole, bestEnd, found, state, &LexingError[TObservation, TToken]{
				Position:    pos,
				Reason:      LexErrNoTransition,
				DFAState:    state,
				HasDFAState: true,
			}
		}

		if !hasObs {
			if strictEOF && !found {
				expected := autarch.DFAAvailableSymbols(dfa, state)
				if len(expected) > 0 {
					return bestToken, bestRole, bestEnd, found, state, &LexingError[TObservation, TToken]{
						Position:    pos,
						Furthest:    furthestPos,
						Expected:    expected,
						Reason:      LexErrUnexpectedEOF,
						DFAState:    state,
						HasDFAState: true,
					}
				}
			}
			break
		}

		nextState, err := autarch.DFAStep(dfa, state, obs, cursor)

		if err != nil || autarch.DFAIsDeadState(dfa, nextState) {
			if found {
				break
			}

			expected := autarch.DFAAvailableSymbols(dfa, state)

			return bestToken, bestRole, bestEnd, found, state, &LexingError[TObservation, TToken]{
				Position:    pos,
				Furthest:    furthestPos,
				Found:       obs,
				HasFound:    true,
				Expected:    expected,
				Reason:      LexErrNoTransition,
				DFAState:    state,
				HasDFAState: true,
			}
		}

		state = nextState

		// update furthest progress
		furthestPos = pos + 1

		if outcome, ok := autarch.DFAStateOutcome(dfa, state); ok && outcome.Value != nonTerminalOutcome {
			newBest, newEnd, updated := resolutionStep(
				outcome.Value.Token,
				pos+1,
				outcome.Value.Priority,
				bestToken,
				bestEnd,
				bestPriority,
			)

			if updated {
				bestToken = newBest
				bestEnd = newEnd
				bestPriority = outcome.Value.Priority
				bestRole = outcome.Value.TokenRole
				found = true
			}
		}

		pos++
	}

	// If we matched something successfully, return it
	if found {
		return bestToken, bestRole, bestEnd, true, state, nil
	}

	return bestToken, bestRole, bestEnd, false, state, nil
}

func lexingRulesetCompile[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	ruleset LexingRuleset[TObservation, TToken, TTokenRole],
	scratchAllocFn memarch.AllocationFn,
	dfaAllocFn memarch.AllocationFn,
	observationDomain *domain.DiscreteDomain[TObservation],
	toBytes func(observations []TObservation) []byte,
	compiler pattern.RegulaToNFACompiler[TObservation, TokenOutcome[TToken, TTokenRole]],
	nonTerminalOutcome TokenOutcome[TToken, TTokenRole],
) *autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]] {
	obsEqual := func(a, b TObservation) bool {
		return observationDomain.OrderingCmp(a, b) == 0
	}
	rules := ruleset.precompiledRules
	for i := 0; i < len(rules); i++ {
		for j := i + 1; j < len(rules); j++ {
			if pattern.PatternEquivalent(&rules[i].pattern, &rules[j].pattern, obsEqual) {
				panic(fmt.Sprintf(
					"lexing ruleset: duplicate pattern: tokens %v and %v have equivalent patterns",
					rules[i].token,
					rules[j].token,
				))
			}
		}
	}

	ctx := pattern.CreateSharedCompilationContext[TObservation, pattern.RegulaAST[TObservation]](
		observationDomain,
		pattern.ObservationFormatter[TObservation]{
			ToBytes: toBytes,
		},
	)

	instructions := make([]pattern.PatternCompilationInstruction[TObservation, TokenOutcome[TToken, TTokenRole], pattern.RegulaAST[TObservation]], len(ruleset.precompiledRules))
	for i, rule := range ruleset.precompiledRules {
		ruleOutcome := TokenOutcome[TToken, TTokenRole]{Token: rule.token, Priority: rule.priority, TokenRole: rule.role}
		instructions[i] = pattern.PatternCompilationInstruction[TObservation, TokenOutcome[TToken, TTokenRole], pattern.RegulaAST[TObservation]]{
			Pattern: &rule.pattern,
			Outcome: ruleOutcome,
		}
	}

	nfas, err := compiler(scratchAllocFn, instructions, ctx, nonTerminalOutcome)
	if err != nil {
		panic(fmt.Errorf("lexing ruleset error: %w", err))
	}

	outNFA := nfas[0]

	for i, generatedNFA := range nfas {
		if i == 0 {
			continue
		}

		outNFA = autarch.NFAMergeOr(outNFA, generatedNFA, scratchAllocFn)
	}

	dfa := autarch.NFAToDFA(outNFA, 1*memcore.KiloByte, 1*memcore.GigaByte, dfaAllocFn, nil)
	minimizedDFA := autarch.DFAMinimize(
		dfa,
		dfaAllocFn,
		1*memcore.KiloByte, 1*memcore.GigaByte,
		func(out pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]) pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]] {
			return out
		})
	return minimizedDFA
}

func lexerCheckEOFStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
) (Lexeme[TObservation, TToken, TTokenRole], bool) {
	if session.eof && len(session.buffer) == 0 {
		return lexerBuildEOF(
			lexer,
			session.absPos,
			session.currentLine,
			session.currentColumn,
			session.tokenNumber,
		), true
	}
	var zero Lexeme[TObservation, TToken, TTokenRole]
	return zero, false
}

func streamingEnsureAt[TObservation cmp.Ordered, TState, TToken comparable](
	session *StreamingLexerSession[TObservation, TState, TToken],
	i int,
) error {
	if i < 0 {
		return fmt.Errorf("invalid lookahead index %d", i)
	}

	// Need buffer length at least i+1, unless EOF occurs.
	for len(session.buffer) <= i && !session.eof {
		remaining := session.maxBufferedObservations - len(session.buffer)
		if remaining <= 0 {
			return fmt.Errorf(
				"streaming lexer buffer limit exceeded (max=%d); token may be longer than configured maxBufferedObservations",
				session.maxBufferedObservations,
			)
		}

		chunk := session.readChunkSize
		if chunk > remaining {
			chunk = remaining
		}

		tmp := make([]TObservation, chunk)
		n, eof, err := session.producer(tmp)
		if err != nil {
			return err
		}
		if n < 0 || n > len(tmp) {
			return fmt.Errorf("producer returned invalid n=%d (dst len=%d)", n, len(tmp))
		}

		if n > 0 {
			session.buffer = append(session.buffer, tmp[:n]...)
		}

		if eof {
			session.eof = true
			break
		}

		// If n==0 and not eof, we loop; this is allowed by contract.
		// Caller controls whether producer blocks or yields no data temporarily.
	}

	return nil
}

func streamingMaybeCompact[TObservation cmp.Ordered, TState, TToken comparable](session *StreamingLexerSession[TObservation, TState, TToken]) {
	// If buffer is empty, drop backing array.
	if len(session.buffer) == 0 {
		session.buffer = session.buffer[:0:0]
		return
	}

	// If the backing capacity is much larger than current length, compact.
	// Heuristic: cap >= 4*len and cap is meaningfully large.
	if cap(session.buffer) >= 4*len(session.buffer) && cap(session.buffer) >= 1024 {
		buf := make([]TObservation, len(session.buffer))
		copy(buf, session.buffer)
		session.buffer = buf
	}
}

func lexerPeekRangeCore[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	ctx scannerContext[TObservation],
	lexerState TState,
	newlineDetector NewlineDetector[TObservation],
	columnAdvanceFn ColumnAdvanceFn[TObservation],
	startLine, startCol, startToken int,
	count int,
) ([]Lexeme[TObservation, TToken, TTokenRole], *LexingError[TObservation, TToken]) {

	dfa, resolutionStep, err := lexerGetDFAAndResolution(lexer, lexerState)
	if err != nil {
		return nil, &LexingError[TObservation, TToken]{
			Position:    ctx.position(),
			StartLine:   startLine,
			StartColumn: startCol,
			Reason:      LexErrNoTransition,
			Formatter:   lexer.formatter,
		}
	}

	line := startLine
	col := startCol
	tokenNum := startToken

	out := make([]Lexeme[TObservation, TToken, TTokenRole], 0, count)

	for i := 0; i < count; i++ {
		if ctx.atEOF() {
			pos := ctx.position()
			out = append(out, lexemeEOF[TObservation, TToken, TTokenRole](
				lexer.eofToken,
				pos,
				line,
				col,
				tokenNum,
			))
			break
		}

		token, tokenRole, raw, found, currentDFAState, lexErr := scanOne(ctx, dfa, resolutionStep, lexer.nonTerminalOutcome)

		// =====================================================
		// scanOne produced structured error
		// =====================================================
		if lexErr != nil {
			base := ctx.position()
			absoluteErrPos := base + lexErr.Position

			lexErr.Position = absoluteErrPos
			lexErr.Furthest = base + lexErr.Furthest
			lexErr.Formatter = lexer.formatter

			errLine, errCol := computePositionFromSlice(
				ctx.slice(0, lexErr.Position-base),
				newlineDetector,
				columnAdvanceFn,
				line,
				col,
			)

			lexErr.StartLine = errLine
			lexErr.StartColumn = errCol

			relativeOffset := absoluteErrPos - base
			if obs, ok, _ := ctx.next(relativeOffset); ok {
				lexErr.Found = obs
				lexErr.HasFound = true
			}

			return nil, lexErr
		}

		// =====================================================
		// No transition at all (Immediate failure)
		// =====================================================
		if !found {
			if ctx.atEOF() {
				return out, nil
			}

			var foundObs TObservation
			if obs, ok, _ := ctx.next(0); ok {
				foundObs = obs
			}

			return nil, &LexingError[TObservation, TToken]{
				Position:    ctx.position(),
				StartLine:   line,
				StartColumn: col,
				Found:       foundObs,
				HasFound:    true,
				Expected:    autarch.DFAAvailableSymbols(dfa, currentDFAState),
				Reason:      LexErrNoTransition,
				DFAState:    currentDFAState,
				HasDFAState: true,
				Formatter:   lexer.formatter,
			}
		}

		// =====================================================
		// Normal token production
		// =====================================================
		start := ctx.position()

		endLine, endCol := computePositionFromSlice(
			raw,
			newlineDetector,
			columnAdvanceFn,
			line,
			col,
		)

		lex := lexemeBuild(
			lexer.formatter,
			token,
			raw,
			start,
			start+len(raw),
			line,
			col,
			endLine,
			endCol,
			tokenNum,
			tokenRole,
		)

		out = append(out, lex)

		ctx.advanceRaw(raw)

		line = endLine
		col = endCol
		tokenNum++
	}

	return out, nil
}

func scanOne[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	ctx scannerContext[TObservation],
	dfa *autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]],
	resolutionStep TokenResolutionStepFn[TToken],
	nonTerminalOutcome TokenOutcome[TToken, TTokenRole],
) (token TToken, tokenRole TTokenRole, raw []TObservation, found bool, state uint64, err *LexingError[TObservation, TToken]) {

	token, role, endRel, found, state, lexErr := scanCore(dfa, ctx.next, resolutionStep, false, nonTerminalOutcome)

	if lexErr != nil {
		return token, role, nil, found, state, lexErr
	}

	if !found {
		return token, role, nil, false, state, nil
	}

	raw = copyRaw(ctx.slice(0, endRel))
	return token, role, raw, true, state, nil
}

type scannerContext[TObservation cmp.Ordered] struct {
	next       func(int) (TObservation, bool, error)
	slice      func(start, end int) []TObservation
	atEOF      func() bool
	position   func() int
	advanceRaw func(raw []TObservation)
}

func scannerFromSlice[TObservation cmp.Ordered, TState, TToken comparable](
	session *LexerSession[TObservation, TState, TToken],
) scannerContext[TObservation] {

	return scannerContext[TObservation]{
		next: func(i int) (TObservation, bool, error) {
			pos := session.position + i
			if pos >= len(session.input) {
				var zero TObservation
				return zero, false, nil
			}
			return session.input[pos], true, nil
		},

		slice: func(start, end int) []TObservation {
			return session.input[start:end]
		},

		atEOF: func() bool {
			return session.position >= len(session.input)
		},

		position: func() int {
			return session.position
		},

		advanceRaw: func(raw []TObservation) {
			session.position += len(raw)
		},
	}
}

func scannerFromStreaming[TObservation cmp.Ordered, TState, TToken comparable](
	session *StreamingLexerSession[TObservation, TState, TToken],
) scannerContext[TObservation] {

	next := streamingNextFn(session)

	return scannerContext[TObservation]{
		next: next,

		slice: func(start, end int) []TObservation {
			return session.buffer[start:end]
		},

		atEOF: func() bool {
			return session.eof && len(session.buffer) == 0
		},

		position: func() int {
			return session.absPos
		},

		advanceRaw: func(raw []TObservation) {
			n := len(raw)
			session.absPos += n
			session.buffer = session.buffer[n:]
			streamingMaybeCompact(session)
		},
	}
}

func scannerFromSliceSimulated[TObservation cmp.Ordered, TState, TToken comparable](
	session *LexerSession[TObservation, TState, TToken],
) scannerContext[TObservation] {

	pos := session.position

	return scannerContext[TObservation]{
		next: func(i int) (TObservation, bool, error) {
			p := pos + i
			if p >= len(session.input) {
				var zero TObservation
				return zero, false, nil
			}
			return session.input[p], true, nil
		},

		slice: func(start, end int) []TObservation {
			return session.input[pos+start : pos+end]
		},

		atEOF: func() bool {
			return pos >= len(session.input)
		},

		position: func() int {
			return pos
		},

		advanceRaw: func(raw []TObservation) {
			pos += len(raw)
		},
	}
}

func scannerFromStreamingSimulated[TObservation cmp.Ordered, TState, TToken comparable](
	session *StreamingLexerSession[TObservation, TState, TToken],
) scannerContext[TObservation] {

	buffer := session.buffer
	absPos := session.absPos
	atEOF := func() bool {
		return session.eof && len(buffer) == 0
	}

	next := func(i int) (TObservation, bool, error) {
		if i >= len(buffer) {
			if atEOF() {
				var zero TObservation
				return zero, false, nil
			}
			if err := streamingEnsureAt(session, i); err != nil {
				var zero TObservation
				return zero, false, err
			}
			buffer = session.buffer
		}
		if i >= len(buffer) {
			var zero TObservation
			return zero, false, nil
		}
		return buffer[i], true, nil
	}

	return scannerContext[TObservation]{
		next: next,

		slice: func(start, end int) []TObservation {
			return buffer[start:end]
		},

		atEOF: func() bool {
			return atEOF() && len(buffer) == 0
		},

		position: func() int {
			return absPos
		},

		advanceRaw: func(raw []TObservation) {
			n := len(raw)
			absPos += n
			buffer = buffer[n:]
		},
	}
}

func copyRaw[T any](src []T) []T {
	dst := make([]T, len(src))
	copy(dst, src)
	return dst
}
