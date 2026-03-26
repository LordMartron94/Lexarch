package lexarch

import (
	"cmp"
	"sync/atomic"
)

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
	// reusable temporary storage for producer reads
	refillScratch []TObservation

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

	scanCache lexerSessionScanCache
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
	lexerSessionScanCacheReset(&s.scanCache)
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
		refillScratch:           make([]TObservation, 0, min(readChunkSize, maxBufferedObservations)),
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
	s.refillScratch = s.refillScratch[:0]
	s.lastError = nil
	lexerSessionScanCacheReset(&s.scanCache)
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
	lexerSessionScanCacheReset(&session.scanCache)
}

func (s *StreamingLexerSession[TObservation, TState, TToken]) GetLastError() *LexingError[TObservation, TToken] {
	return s.lastError
}
