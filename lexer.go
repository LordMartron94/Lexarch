package lexarch

import (
	"autarch"
	"autarch/pattern"
	"cmp"
	"fmt"
	"memarch"
	"memcore"
	"memforge"
)

// ------------------------------------------------------ RULES

type lexingRule[TObservation cmp.Ordered, TToken comparable] struct {
	pattern  pattern.RegulaAST[TObservation]
	token    TToken
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
type TokenOutcome[TToken comparable] struct {
	Token    TToken
	Priority int
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
type LexingRuleset[TObservation cmp.Ordered, TToken comparable] struct {
	precompiledRules    []lexingRule[TObservation, TToken]
	tokenResolutionStep TokenResolutionStepFn[TToken]
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
func (l *LexingRuleset[TObservation, TToken]) WithRule(pattern pattern.RegulaAST[TObservation], token TToken) {
	l.precompiledRules = append(l.precompiledRules, lexingRule[TObservation, TToken]{
		pattern:  pattern,
		token:    token,
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
func (l *LexingRuleset[TObservation, TToken]) WithRulePriority(
	pattern pattern.RegulaAST[TObservation],
	token TToken,
	priority int,
) {
	l.precompiledRules = append(l.precompiledRules, lexingRule[TObservation, TToken]{
		pattern:  pattern,
		token:    token,
		priority: priority,
	})
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
func LexingRulesetCreate[TObservation cmp.Ordered, TToken comparable](
	tokenResolutionStep TokenResolutionStepFn[TToken],
) *LexingRuleset[TObservation, TToken] {
	if tokenResolutionStep == nil {
		tokenResolutionStep = TokenResolutionStepLongest[TToken]
	}
	return &LexingRuleset[TObservation, TToken]{
		precompiledRules:    make([]lexingRule[TObservation, TToken], 0),
		tokenResolutionStep: tokenResolutionStep,
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
func (l *LexingRuleset[TObservation, TToken]) WithTokenResolution(
	resolutionStep TokenResolutionStepFn[TToken],
) *LexingRuleset[TObservation, TToken] {
	if resolutionStep == nil {
		resolutionStep = TokenResolutionStepLongest[TToken]
	}
	l.tokenResolutionStep = resolutionStep
	return l
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
type Lexeme[TObservation cmp.Ordered, TToken comparable] struct {
	Raw   []TObservation
	Token TToken

	Start, End int

	// Position information (1-indexed)
	StartLine   int // Line number where token starts
	StartColumn int // Column number where token starts
	EndLine     int // Line number where token ends
	EndColumn   int // Column number where token ends
	TokenNumber int // Sequence number of this token
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
type LexerSession[TObservation cmp.Ordered, TState comparable] struct {
	currentState TState

	input    []TObservation
	position int

	// Position tracking state
	newlineDetector NewlineDetector[TObservation]
	currentLine     int // Current line number (1-indexed)
	currentColumn   int // Current column number (1-indexed)
	tokenNumber     int // Next token sequence number (1-indexed)
}

func (s *LexerSession[TObservation, TState]) Position() int {
	return s.position
}

func (s *LexerSession[TObservation, TState]) SetPosition(position int) {
	s.position = position
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
func LexerSessionSetState[TObservation cmp.Ordered, TState comparable](lexerSession *LexerSession[TObservation, TState], state TState) {
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
func LexerSessionCreate[TObservation cmp.Ordered, TState comparable](
	initialState TState,
	input []TObservation,
	newlineDetector NewlineDetector[TObservation],
) *LexerSession[TObservation, TState] {
	return &LexerSession[TObservation, TState]{
		currentState:    initialState,
		input:           input,
		position:        0,
		newlineDetector: newlineDetector,
		currentLine:     1,
		currentColumn:   1,
		tokenNumber:     1,
	}
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

Important lifetime note:
  - Lexeme.Raw returned by LexerConsumeStreaming / LexerPeekStreaming is a view into the
    session’s internal buffer and is only guaranteed to remain stable until the next
    call that may compact the buffer. If you need to retain Raw long-term, copy it.

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
type StreamingLexerSession[TObservation cmp.Ordered, TState comparable] struct {
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
	currentLine     int // Current line number (1-indexed)
	currentColumn   int // Current column number (1-indexed)
	tokenNumber     int // Next token sequence number (1-indexed)
}

func (s *StreamingLexerSession[TObservation, TState]) AbsPosition() int {
	return s.absPos
}

func (s *StreamingLexerSession[TObservation, TState]) RestoreAbsolute(position int) {
	s.absPos = position
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
func StreamingLexerSessionCreate[TObservation cmp.Ordered, TState comparable](
	initialState TState,
	producer ObservationProducerFn[TObservation],
	newlineDetector NewlineDetector[TObservation],
	readChunkSize int,
	maxBufferedObservations int,
) *StreamingLexerSession[TObservation, TState] {
	if producer == nil {
		panic("producer must not be nil")
	}
	if readChunkSize <= 0 {
		panic("readChunkSize must be > 0")
	}
	if maxBufferedObservations <= 0 {
		panic("maxBufferedObservations must be > 0")
	}

	return &StreamingLexerSession[TObservation, TState]{
		currentState:            initialState,
		producer:                producer,
		eof:                     false,
		buffer:                  make([]TObservation, 0, min(readChunkSize, maxBufferedObservations)),
		absPos:                  0,
		readChunkSize:           readChunkSize,
		maxBufferedObservations: maxBufferedObservations,
		newlineDetector:         newlineDetector,
		currentLine:             1,
		currentColumn:           1,
		tokenNumber:             1,
	}
}

/*
StreamingLexerSessionSetState changes the current lexer state for a streaming session.

Use cases:
- Context-sensitive lexing over streams (strings/comments/etc.)
- Switching recognition modes mid-stream

Time complexity: O(1)
Space complexity: O(1)
*/
func StreamingLexerSessionSetState[TObservation cmp.Ordered, TState comparable](
	session *StreamingLexerSession[TObservation, TState],
	state TState,
) {
	session.currentState = state
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
type Lexer[TObservation cmp.Ordered, TState, TToken comparable] struct {
	ruleSets         map[TState]*autarch.DFA[TObservation, TokenOutcome[TToken]]
	tokenResolutions map[TState]TokenResolutionStepFn[TToken]

	dfaAllocator memcore.MarkRaw
	errorToken   TToken
	eofToken     TToken
}

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
func LexerCreate[TObservation cmp.Ordered, TState, TToken comparable](
	inputRulesets map[TState]LexingRuleset[TObservation, TToken],
	errorToken TToken,
	eofToken TToken,
	scratchAllocationFn memarch.AllocationFn,
	maxDFAAllocatorMemory memcore.MemoryUnitBytes,
) *Lexer[TObservation, TState, TToken] {
	dfaAllocator := memforge.DynamicLinearAllocatorCreateFunction(uint64(memcore.KiloByte), func(currentCap, neededCap uint64) uint64 {
		newSize := min(currentCap*2, neededCap)

		if newSize > uint64(maxDFAAllocatorMemory) {
			panic("dfa allocator consumes too much memory")
		}

		return newSize
	})

	lexerRulesets := make(map[TState]*autarch.DFA[TObservation, TokenOutcome[TToken]])
	tokenResolutions := make(map[TState]TokenResolutionStepFn[TToken])

	for state, ruleset := range inputRulesets {
		compiled := lexingRulesetCompile(ruleset, scratchAllocationFn, func(sizeBytes, alignment uint64) memcore.MarkRaw {
			return memforge.DynamicLinearAllocatorMallocUnsafe(dfaAllocator, sizeBytes, alignment)
		})

		lexerRulesets[state] = compiled

		// Store resolution function (default to longest if not set)
		if ruleset.tokenResolutionStep != nil {
			tokenResolutions[state] = ruleset.tokenResolutionStep
		} else {
			tokenResolutions[state] = TokenResolutionStepLongest[TToken]
		}
	}

	return &Lexer[TObservation, TState, TToken]{
		ruleSets:         lexerRulesets,
		tokenResolutions: tokenResolutions,
		dfaAllocator:     dfaAllocator,
		errorToken:       errorToken,
		eofToken:         eofToken,
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
func LexerDebugDFA[TObservation cmp.Ordered, TState, TToken comparable](
	lexer *Lexer[TObservation, TState, TToken],
	state TState,
	formatter *autarch.DFADebugFormatter[TObservation, TokenOutcome[TToken]],
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
func LexerDebugFormatterCreateRune[TState, TToken comparable]() *autarch.DFADebugFormatter[rune, TokenOutcome[TToken]] {
	return &autarch.DFADebugFormatter[rune, TokenOutcome[TToken]]{
		FormatSymbolName: func(symbolID uint64, name string, observation *rune) string {
			if observation != nil {
				r := *observation
				if r >= 32 && r < 127 {
					return fmt.Sprintf("%s ('%c')", name, r)
				} else if r == '\n' {
					return fmt.Sprintf("%s ('\\n')", name)
				} else if r == '\t' {
					return fmt.Sprintf("%s ('\\t')", name)
				} else if r == '\r' {
					return fmt.Sprintf("%s ('\\r')", name)
				} else {
					return fmt.Sprintf("%s (\\u%04x)", name, r)
				}
			}
			return name
		},
		FormatStateOutcome: func(outcome TokenOutcome[TToken]) string {
			return fmt.Sprintf("{Token: %v, Priority: %d}", outcome.Token, outcome.Priority)
		},
		FormatSymbolID: func(symbolID uint64) string {
			return fmt.Sprintf("%3d", symbolID)
		},
		FormatStateID: func(stateID uint64) string {
			return fmt.Sprintf("%3d", stateID)
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
func LexerClose[TObservation cmp.Ordered, TState, TToken comparable](lexer *Lexer[TObservation, TState, TToken]) {
	memforge.DynamicLinearAllocatorDestroy(lexer.dfaAllocator)
}

/*
LexerConsume recognizes and consumes the next token from the input stream at the current position.
The session position is advanced past the recognized token. Uses longest match scanning to resolve
ambiguous patterns. Returns the EOF token when the end of input is reached.

Use cases:
- Tokenizing input streams incrementally
- Building parsers that consume tokens sequentially
- Processing input with state-based token recognition

Time complexity: O(n) where n is the length of the matched token, O(1) for EOF detection
Space complexity: O(1) - returns slice into original input

Prerequisites:
- lexer must be a valid, non-closed lexer
- session must be a valid session with valid current state
- session position must be within input bounds or at end

Edge cases:
- Returns EOF token when position is at end of input (position >= len(input))
- Returns error if no pattern matches at current position (and not at EOF)
- Returns error if current state has no ruleset
- Advances position past recognized token (or stays at end for EOF)
- Raw slice is empty for EOF token
- Raw slice is a view into session input, so lifetime depends on input
- Longest match is used when multiple patterns match
*/
func LexerConsume[TObservation cmp.Ordered, TState, TToken comparable](
	lexer *Lexer[TObservation, TState, TToken],
	session *LexerSession[TObservation, TState],
) (Lexeme[TObservation, TToken], error) {

	if eofLexeme, atEOF := lexerCheckEOF(lexer, session); atEOF {
		return eofLexeme, nil
	}

	dfa, ok := lexer.ruleSets[session.currentState]
	if !ok {
		return Lexeme[TObservation, TToken]{},
			fmt.Errorf("no ruleset for state %v", session.currentState)
	}

	resolutionStep, ok := lexer.tokenResolutions[session.currentState]
	if !ok {
		resolutionStep = TokenResolutionStepLongest[TToken]
	}

	token, end, ok := scanLongestMatch(
		dfa,
		session.input,
		session.position,
		resolutionStep,
	)

	if !ok {
		return Lexeme[TObservation, TToken]{},
			fmt.Errorf("invalid token at %d", session.position)
	}

	start := session.position
	raw := session.input[start:end]

	// Compute position information
	startLine := session.currentLine
	startColumn := session.currentColumn
	endLine, endColumn := computePositionFromSlice(raw, session.newlineDetector, startLine, startColumn)

	tokenNumber := session.tokenNumber
	session.tokenNumber++

	// Update session position state
	session.currentLine = endLine
	session.currentColumn = endColumn
	session.position = end

	return Lexeme[TObservation, TToken]{
		Token:       token,
		Raw:         raw,
		Start:       start,
		End:         end,
		StartLine:   startLine,
		StartColumn: startColumn,
		EndLine:     endLine,
		EndColumn:   endColumn,
		TokenNumber: tokenNumber,
	}, nil
}

/*
LexerPeek recognizes the next token from the input stream without advancing the session position.
This allows lookahead without consuming tokens. Uses longest match scanning to resolve ambiguous patterns.
Returns the EOF token when the end of input is reached.

Use cases:
- Lookahead for parser decision-making
- Checking next token without consuming it
- Implementing parser backtracking or multi-token lookahead

Time complexity: O(n) where n is the length of the matched token, O(1) for EOF detection
Space complexity: O(1) - returns slice into original input

Prerequisites:
- lexer must be a valid, non-closed lexer
- session must be a valid session with valid current state
- session position must be within input bounds or at end

Edge cases:
- Returns EOF token when position is at end of input (position >= len(input))
- Returns error if no pattern matches at current position (and not at EOF)
- Does not modify session position
- Raw slice is empty for EOF token
- Raw slice is a view into session input, so lifetime depends on input
- Longest match is used when multiple patterns match
- Multiple peeks return the same token until position changes
*/
func LexerPeek[TObservation cmp.Ordered, TState, TToken comparable](
	lexer *Lexer[TObservation, TState, TToken],
	session *LexerSession[TObservation, TState],
	n int,
) (Lexeme[TObservation, TToken], error) {

	if n < 0 {
		return Lexeme[TObservation, TToken]{},
			fmt.Errorf("peek index must be >= 0")
	}

	// Snapshot session state
	pos := session.position
	line := session.currentLine
	col := session.currentColumn
	tokenNum := session.tokenNumber

	var lex Lexeme[TObservation, TToken]
	var err error

	for i := 0; i <= n; i++ {

		if pos >= len(session.input) {
			lex = Lexeme[TObservation, TToken]{
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
			break
		}

		dfa := lexer.ruleSets[session.currentState]

		resolutionStep, ok := lexer.tokenResolutions[session.currentState]
		if !ok {
			resolutionStep = TokenResolutionStepLongest[TToken]
		}

		token, end, ok := scanLongestMatch(
			dfa,
			session.input,
			pos,
			resolutionStep,
		)

		if !ok {
			return Lexeme[TObservation, TToken]{},
				fmt.Errorf("invalid token at %d", pos)
		}

		raw := session.input[pos:end]

		startLine := line
		startColumn := col
		endLine, endColumn := computePositionFromSlice(
			raw,
			session.newlineDetector,
			startLine,
			startColumn,
		)

		lex = Lexeme[TObservation, TToken]{
			Token:       token,
			Raw:         raw,
			Start:       pos,
			End:         end,
			StartLine:   startLine,
			StartColumn: startColumn,
			EndLine:     endLine,
			EndColumn:   endColumn,
			TokenNumber: tokenNum,
		}

		// Advance simulated state (not real session)
		pos = end
		line = endLine
		col = endColumn
		tokenNum++
	}

	return lex, err
}

/*
LexerAssertConsume consumes the next token and verifies it matches the expected token type.
If the token does not match, an error is returned describing the mismatch. This is useful for
parsers that require specific token sequences.

Use cases:
- Parsing structured formats with required tokens
- Validating token sequences
- Implementing grammar rules with mandatory tokens

Time complexity: O(n) where n is the length of the matched token
Space complexity: O(1) - returns slice into original input

Prerequisites:
- lexer must be a valid, non-closed lexer
- session must be a valid session with valid current state
- session position must be within input bounds

Edge cases:
- Returns error if token recognition fails (from LexerConsume)
- Returns error if recognized token does not match expected
- Advances position only if token matches
- Error message includes expected and actual token values with position
*/
func LexerAssertConsume[TObservation cmp.Ordered, TState, TToken comparable](
	lexer *Lexer[TObservation, TState, TToken],
	session *LexerSession[TObservation, TState],
	expected TToken,
) (Lexeme[TObservation, TToken], error) {

	lex, err := LexerConsume(lexer, session)
	if err != nil {
		return lex, err
	}

	if lex.Token != expected {
		return lex, fmt.Errorf(
			"expected %v, got %v at %d",
			expected, lex.Token, lex.Start,
		)
	}

	return lex, nil
}

/*
LexerAssertPeek peeks at the next token and verifies it matches the expected token type without
consuming it. If the token does not match, an error is returned describing the mismatch.

Use cases:
- Validating lookahead tokens before consuming
- Implementing conditional parsing based on token type
- Checking token sequences without consuming

Time complexity: O(n) where n is the length of the matched token
Space complexity: O(1) - returns slice into original input

Prerequisites:
- lexer must be a valid, non-closed lexer
- session must be a valid session with valid current state
- session position must be within input bounds

Edge cases:
- Returns error if token recognition fails (from LexerPeek)
- Returns error if recognized token does not match expected
- Does not modify session position
- Error message includes expected and actual token values with position
*/
func LexerAssertPeek[TObservation cmp.Ordered, TState, TToken comparable](
	lexer *Lexer[TObservation, TState, TToken],
	session *LexerSession[TObservation, TState],
	expected TToken,
	n int,
) (Lexeme[TObservation, TToken], error) {

	lex, err := LexerPeek(lexer, session, n)
	if err != nil {
		return lex, err
	}

	if lex.Token != expected {
		return lex, fmt.Errorf(
			"expected %v, got %v at %d",
			expected, lex.Token, lex.Start,
		)
	}

	return lex, nil
}

/*
LexerConsumeStreaming recognizes and consumes the next token from a streaming input session.
Behavior matches LexerConsume, but input is obtained lazily from the session’s producer.

Use cases:
- Online tokenization as bytes/runes arrive
- Streaming parsers that consume tokens sequentially

Time complexity: O(n) where n is matched token length (plus producer cost)
Space complexity: O(1) excluding the session buffer

Edge cases:
- Returns EOF token when producer reaches EOF and buffer is empty
- Returns error if no pattern matches at current position (and not at EOF)
- Returns error if buffering would exceed maxBufferedObservations
*/
func LexerConsumeStreaming[TObservation cmp.Ordered, TState, TToken comparable](
	lexer *Lexer[TObservation, TState, TToken],
	session *StreamingLexerSession[TObservation, TState],
) (Lexeme[TObservation, TToken], error) {

	if eofLexeme, atEOF := lexerCheckEOFStreaming(lexer, session); atEOF {
		return eofLexeme, nil
	}

	dfa, ok := lexer.ruleSets[session.currentState]
	if !ok {
		return Lexeme[TObservation, TToken]{},
			fmt.Errorf("no ruleset for state %v", session.currentState)
	}

	resolutionStep, ok := lexer.tokenResolutions[session.currentState]
	if !ok {
		resolutionStep = TokenResolutionStepLongest[TToken]
	}

	next := func(i int) (TObservation, bool, error) {
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

	token, endRel, found, err := scanCore(dfa, next, resolutionStep)
	if err != nil {
		return Lexeme[TObservation, TToken]{}, err
	}
	if !found {
		return Lexeme[TObservation, TToken]{},
			fmt.Errorf("invalid token at %d", session.absPos)
	}

	raw := session.buffer[:endRel]

	// Compute position information
	startLine := session.currentLine
	startColumn := session.currentColumn
	endLine, endColumn := computePositionFromSlice(raw, session.newlineDetector, startLine, startColumn)

	tokenNumber := session.tokenNumber
	session.tokenNumber++

	// Advance session (consume endRel observations)
	session.currentLine = endLine
	session.currentColumn = endColumn
	session.absPos += endRel
	session.buffer = session.buffer[endRel:]

	// Optional compaction to prevent retaining large backing arrays.
	streamingMaybeCompact(session)

	return Lexeme[TObservation, TToken]{
		Token:       token,
		Raw:         raw,
		Start:       session.absPos - endRel,
		End:         session.absPos,
		StartLine:   startLine,
		StartColumn: startColumn,
		EndLine:     endLine,
		EndColumn:   endColumn,
		TokenNumber: tokenNumber,
	}, nil
}

/*
LexerPeekStreaming recognizes the next token from a streaming input session without consuming it.
Behavior matches LexerPeek, but input is obtained lazily from the session’s producer.

Use cases:
- Lookahead in streaming parsers
- Conditional parsing decisions without consumption

Time complexity: O(n) where n is matched token length (plus producer cost)
Space complexity: O(1) excluding the session buffer

Edge cases:
- Returns EOF token when producer reaches EOF and buffer is empty
- Returns error if no pattern matches at current position (and not at EOF)
- Returns error if buffering would exceed maxBufferedObservations
*/
func LexerPeekStreaming[TObservation cmp.Ordered, TState, TToken comparable](
	lexer *Lexer[TObservation, TState, TToken],
	session *StreamingLexerSession[TObservation, TState],
	n int,
) (Lexeme[TObservation, TToken], error) {

	if n < 0 {
		return Lexeme[TObservation, TToken]{},
			fmt.Errorf("peek index must be >= 0")
	}

	// Snapshot simulated state
	absPos := session.absPos
	line := session.currentLine
	col := session.currentColumn
	tokenNum := session.tokenNumber
	buffer := session.buffer

	var lex Lexeme[TObservation, TToken]

	for i := 0; i <= n; i++ {

		if session.eof && len(buffer) == 0 {
			lex = Lexeme[TObservation, TToken]{
				Token:       lexer.eofToken,
				Raw:         nil,
				Start:       absPos,
				End:         absPos,
				StartLine:   line,
				StartColumn: col,
				EndLine:     line,
				EndColumn:   col,
				TokenNumber: tokenNum,
			}
			break
		}

		next := func(j int) (TObservation, bool, error) {
			if err := streamingEnsureAt(session, j); err != nil {
				var zero TObservation
				return zero, false, err
			}
			if j >= len(session.buffer) {
				var zero TObservation
				return zero, false, nil
			}
			return session.buffer[j], true, nil
		}

		dfa := lexer.ruleSets[session.currentState]

		resolutionStep, ok := lexer.tokenResolutions[session.currentState]
		if !ok {
			resolutionStep = TokenResolutionStepLongest[TToken]
		}

		token, endRel, found, err := scanCore(dfa, next, resolutionStep)
		if err != nil {
			return Lexeme[TObservation, TToken]{}, err
		}
		if !found {
			return Lexeme[TObservation, TToken]{},
				fmt.Errorf("invalid token at %d", absPos)
		}

		raw := session.buffer[:endRel]

		startLine := line
		startColumn := col
		endLine, endColumn := computePositionFromSlice(
			raw,
			session.newlineDetector,
			startLine,
			startColumn,
		)

		lex = Lexeme[TObservation, TToken]{
			Token:       token,
			Raw:         raw,
			Start:       absPos,
			End:         absPos + endRel,
			StartLine:   startLine,
			StartColumn: startColumn,
			EndLine:     endLine,
			EndColumn:   endColumn,
			TokenNumber: tokenNum,
		}

		// Advance simulated state
		absPos += endRel
		line = endLine
		col = endColumn
		tokenNum++

		buffer = buffer[endRel:]
		session.buffer = buffer
	}

	return lex, nil
}

/*
LexerAssertConsumeStreaming consumes the next token from a streaming session and verifies
it matches the expected token type.

This mirrors LexerAssertConsume but operates on streaming input sourced via a producer
callback.

Use cases:
- Enforcing grammar structure in streaming parsers
- Protocol parsing with required token sequences
- Incremental syntax validation over large inputs

Time complexity: O(n) where n is the matched token length (plus producer cost)
Space complexity: O(1) excluding session buffer

Prerequisites:
- lexer must be a valid, non-closed lexer
- session must be a valid StreamingLexerSession
- expected must be a valid token value

Edge cases:
- Returns error if token recognition fails
- Returns error if recognized token does not match expected
- Advances stream position only if token matches
- Error message includes expected and actual token values with absolute position
*/
func LexerAssertConsumeStreaming[TObservation cmp.Ordered, TState, TToken comparable](
	lexer *Lexer[TObservation, TState, TToken],
	session *StreamingLexerSession[TObservation, TState],
	expected TToken,
) (Lexeme[TObservation, TToken], error) {

	lex, err := LexerConsumeStreaming(lexer, session)
	if err != nil {
		return lex, err
	}

	if lex.Token != expected {
		return lex, fmt.Errorf(
			"expected %v, got %v at %d",
			expected,
			lex.Token,
			lex.Start,
		)
	}

	return lex, nil
}

/*
LexerAssertPeekStreaming peeks at the next token from a streaming session and verifies
it matches the expected token type without consuming it.

This mirrors LexerAssertPeek but operates on streaming input.

Use cases:
- Lookahead validation in streaming grammars
- Conditional parsing decisions
- Verifying upcoming structure without consuming tokens

Time complexity: O(n) where n is the matched token length (plus producer cost)
Space complexity: O(1) excluding session buffer

Prerequisites:
- lexer must be a valid, non-closed lexer
- session must be a valid StreamingLexerSession
- expected must be a valid token value

Edge cases:
- Returns error if token recognition fails
- Returns error if recognized token does not match expected
- Does not modify session position
- Error message includes expected and actual token values with absolute position
*/
func LexerAssertPeekStreaming[TObservation cmp.Ordered, TState, TToken comparable](
	lexer *Lexer[TObservation, TState, TToken],
	session *StreamingLexerSession[TObservation, TState],
	expected TToken,
	n int,
) (Lexeme[TObservation, TToken], error) {

	lex, err := LexerPeekStreaming(lexer, session, n)
	if err != nil {
		return lex, err
	}

	if lex.Token != expected {
		return lex, fmt.Errorf(
			"expected %v, got %v at %d",
			expected,
			lex.Token,
			lex.Start,
		)
	}

	return lex, nil
}

// ------------------------------------------------------ PRIVATE HELPERS

/*
computePositionFromSlice computes the final line and column after processing a slice of observations,
given the starting line and column. This is used to compute end positions for tokens.

Time complexity: O(n) where n is the length of the slice
Space complexity: O(1)

Prerequisites:
- newlineDetector must be a valid newline detector
- startLine and startColumn must be valid (1-indexed)

Edge cases:
- Empty slice returns startLine and startColumn unchanged
- Handles multiple newlines in the slice
- Column resets to 1 after each newline
*/
func computePositionFromSlice[TObservation cmp.Ordered](
	observations []TObservation,
	newlineDetector NewlineDetector[TObservation],
	startLine, startColumn int,
) (endLine, endColumn int) {
	line := startLine
	column := startColumn

	for _, obs := range observations {
		if newlineDetector(obs) {
			line++
			column = 1
		} else {
			column++
		}
	}

	return line, column
}

/*
updatePositionIncrementally updates position state as observations are processed incrementally.
This is used during scanning to track position as we process each observation.

Time complexity: O(1) per observation
Space complexity: O(1)

Prerequisites:
- newlineDetector must be a valid newline detector
- line and column must be valid (1-indexed)

Edge cases:
- Newline increments line and resets column to 1
- Non-newline increments column
*/
func updatePositionIncrementally[TObservation cmp.Ordered](
	obs TObservation,
	newlineDetector NewlineDetector[TObservation],
	line, column *int,
) {
	if newlineDetector(obs) {
		*line++
		*column = 1
	} else {
		*column++
	}
}

func lexerCheckEOF[TObservation cmp.Ordered, TState, TToken comparable](
	lexer *Lexer[TObservation, TState, TToken],
	session *LexerSession[TObservation, TState],
) (Lexeme[TObservation, TToken], bool) {
	if session.position >= len(session.input) {
		pos := len(session.input)
		return Lexeme[TObservation, TToken]{
			Token:       lexer.eofToken,
			Raw:         nil,
			Start:       pos,
			End:         pos,
			StartLine:   session.currentLine,
			StartColumn: session.currentColumn,
			EndLine:     session.currentLine,
			EndColumn:   session.currentColumn,
			TokenNumber: session.tokenNumber,
		}, true
	}
	var zero Lexeme[TObservation, TToken]
	return zero, false
}

func scanLongestMatch[TObservation cmp.Ordered, TToken comparable](
	dfa *autarch.DFA[TObservation, TokenOutcome[TToken]],
	input []TObservation,
	start int,
	resolutionStep TokenResolutionStepFn[TToken],
) (token TToken, end int, ok bool) {

	next := func(i int) (TObservation, bool, error) {
		pos := start + i
		if pos >= len(input) {
			var zero TObservation
			return zero, false, nil
		}
		return input[pos], true, nil
	}

	token, endRel, found, _ := scanCore(dfa, next, resolutionStep)

	if !found {
		var zero TToken
		return zero, start, false
	}

	return token, start + endRel, true
}

func scanCore[TObservation cmp.Ordered, TToken comparable](
	dfa *autarch.DFA[TObservation, TokenOutcome[TToken]],
	nextObs func(int) (obs TObservation, ok bool, err error),
	resolutionStep TokenResolutionStepFn[TToken],
) (bestToken TToken, bestEnd int, found bool, err error) {

	cursor := autarch.DFACursorGet(dfa)
	state := uint64(0)

	bestPriority := 0
	pos := 0

	for {
		obs, hasObs, err := nextObs(pos)
		if err != nil {
			var zero TToken
			return zero, 0, false, err
		}
		if !hasObs {
			break
		}

		next := autarch.DFATransition(dfa, obs, state, cursor)
		if next == 0 {
			break
		}

		state = next

		if outcome, ok := autarch.DFAStateOutcome(dfa, state); ok {
			newBest, newEnd, updated := resolutionStep(
				outcome.Token,
				pos+1,
				outcome.Priority,
				bestToken,
				bestEnd,
				bestPriority,
			)

			if updated {
				bestToken = newBest
				bestEnd = newEnd
				bestPriority = outcome.Priority
				found = true
			}
		}

		pos++
	}

	return bestToken, bestEnd, found, nil
}

func lexingRulesetCompile[TObservation cmp.Ordered, TToken comparable](
	ruleset LexingRuleset[TObservation, TToken],
	scratchAllocFn memarch.AllocationFn,
	dfaAllocFn memarch.AllocationFn,
) *autarch.DFA[TObservation, TokenOutcome[TToken]] {
	ctx := pattern.RegulaCreateSharedCompilationContext[TObservation]()

	for _, rule := range ruleset.precompiledRules {
		ctx.CollectPattern(&rule.pattern)
	}

	ctx.BuildAlphabet()

	initialRule := ruleset.precompiledRules[0]
	initialOutcome := TokenOutcome[TToken]{Token: initialRule.token, Priority: initialRule.priority}
	nfa := pattern.RegulaCompileToNFAWithBuilder(scratchAllocFn, initialRule.pattern, ctx, initialOutcome)

	for i, rule := range ruleset.precompiledRules {
		if i == 0 {
			continue
		}

		ruleOutcome := TokenOutcome[TToken]{Token: rule.token, Priority: rule.priority}
		ruleNFA := pattern.RegulaCompileToNFAWithBuilder(scratchAllocFn, rule.pattern, ctx, ruleOutcome)
		nfa = autarch.NFAMergeOr(nfa, ruleNFA, scratchAllocFn)
	}

	dfa := autarch.NFAToDFA(nfa, 1*memcore.KiloByte, 1*memcore.GigaByte, dfaAllocFn, nil)
	minimizedDFA := autarch.DFAMinimize(dfa, dfaAllocFn, 1*memcore.KiloByte, 1*memcore.GigaByte)
	return minimizedDFA
}

func lexerCheckEOFStreaming[TObservation cmp.Ordered, TState, TToken comparable](
	lexer *Lexer[TObservation, TState, TToken],
	session *StreamingLexerSession[TObservation, TState],
) (Lexeme[TObservation, TToken], bool) {
	if session.eof && len(session.buffer) == 0 {
		pos := session.absPos
		return Lexeme[TObservation, TToken]{
			Token:       lexer.eofToken,
			Raw:         nil,
			Start:       pos,
			End:         pos,
			StartLine:   session.currentLine,
			StartColumn: session.currentColumn,
			EndLine:     session.currentLine,
			EndColumn:   session.currentColumn,
			TokenNumber: session.tokenNumber,
		}, true
	}
	var zero Lexeme[TObservation, TToken]
	return zero, false
}

func streamingEnsureAt[TObservation cmp.Ordered, TState comparable](
	session *StreamingLexerSession[TObservation, TState],
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

func streamingMaybeCompact[TObservation cmp.Ordered, TState comparable](session *StreamingLexerSession[TObservation, TState]) {
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
