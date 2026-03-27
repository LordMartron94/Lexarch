# lexarch

Generic, state-based lexical analysis library for tokenizing input streams using deterministic finite automata.

## Overview

`lexarch` provides efficient lexical analysis (tokenization) capabilities for processing input streams into sequences of tokens. The library uses deterministic finite automata (DFA) compiled from regular expression patterns to recognize tokens with optimal runtime performance. It supports state-based lexing where different rulesets can be active depending on the current lexer state, enabling context-sensitive tokenization.

Key features:

- **Generic Type Support**: Works with any ordered observation type (runes, bytes, tokens) and comparable token types
- **State-Based Lexing**: Different rulesets per state for context-sensitive tokenization
- **Inline Token Resolution**: Zero-allocation token resolution using incremental best-match tracking
- **Longest Match Scanning**: Automatically resolves ambiguous patterns by matching the longest possible token (configurable)
- **Zero Allocations in Hot Paths**: Tokenization operations use pre-allocated DFAs and cursors (slice-based API)
- **Pattern-Based Definitions**: Uses the `autarch/pattern` RegulaAST system for flexible pattern construction
- **Streaming Support**: Callback-based observation providers with ring buffer optimization for tokenizing large streams without dynamic allocations
- **Position Tracking**: Line numbers, column numbers, and token sequence numbers for error reporting and debugging

## Design Philosophy

- **Explicit Memory Management**: All DFAs are compiled and allocated at lexer creation time, enabling zero-allocation tokenization
- **Generic Type Safety**: Support for any observation type (runes, bytes, tokens) and comparable token outcomes
- **State-Based Architecture**: Enables context-sensitive lexing where token recognition depends on lexer state
- **Separation of Concerns**: Data structures (lexers, sessions) are separate from operations (functions), following C-style function-on-data patterns
- **Cache Efficiency**: DFAs use flat transition tables for optimal memory layout and cache performance

## Performance Characteristics

- **Zero Allocations in Hot Paths**: Tokenization uses pre-allocated DFAs and cursors
- **Cache Efficiency**: DFA transition tables stored as contiguous arrays (state * alphabetSize + symbolID)
- **Time Complexity**:
  - Lexer creation: O(2^n * a) worst case per ruleset for NFA-to-DFA conversion, where n is NFA states and a is alphabet size
  - Token recognition: O(m) where m is the length of the matched token
  - DFA minimization: O(s log s) where s is DFA states
- **Space Complexity**:
  - Lexer: O(s * a) where s is DFA states and a is alphabet size
  - Slice Session: O(1) - stores references and position only
  - Streaming Session: O(bufferCapacity) - maintains buffer for lookahead
  - Lexeme: O(m) where m is token length (slice into input for slice API, allocated copy for streaming API)

## Integration

```text
lexarch
├── autarch (finite automata, pattern compilation)
│   └── pattern (RegulaAST pattern system)
├── memarch (memory allocation)
│   └── memcore (memory units and types)
└── memforge (dynamic allocators)
```

The library integrates with:

- **autarch**: For NFA construction, NFA-to-DFA conversion, and DFA minimization
- **autarch/pattern**: For building token recognition patterns using RegulaAST
- **memarch**: For memory allocation during DFA compilation
- **memforge**: For dynamic linear allocators managing DFA memory

## API

### Ruleset Definition

#### `LexingRulesetCreate[TObservation, TToken, TTokenRole](tokenResolutionStep TokenResolutionStepFn[TToken]) *LexingRuleset`

Creates a new empty ruleset ready for pattern-to-token mappings. The `tokenResolutionStep` parameter
specifies how to resolve conflicts when multiple tokens match at the same position using inline resolution.
If `nil`, defaults to `TokenResolutionStepLongest` (longest match). The inline resolution API eliminates
allocations in the hot path by updating the best match incrementally during scanning.

#### `WithRule(pattern RegulaAST[TObservation], token TToken, role TTokenRole)`

Adds a pattern-to-token mapping to the ruleset. The pattern defines what input sequence matches this token; the role is stored in each produced Lexeme.

#### `WithRulePriority(pattern RegulaAST[TObservation], token TToken, role TTokenRole, priority int)`

Adds a pattern-to-token mapping with an explicit priority. Higher priority values are preferred during
priority-based resolution. Priority is only used with priority-based resolution functions.

#### `WithTokenResolution(resolutionStep TokenResolutionStepFn[TToken]) *LexingRuleset`

Sets the token resolution step function for the ruleset. This determines which token is selected when
multiple tokens match at the same position using inline resolution (zero allocations).

**Example:**

```go
import "autarch/pattern"

// Default resolution (longest match); role type can be int or your role type
ruleset := lexarch.LexingRulesetCreate[rune, TokenType, TokenRole](nil)
ruleset.WithRule(pattern.Literal('i', 'f'), TokenIf, RoleKeyword)
ruleset.WithRule(pattern.Literal('e', 'l', 's', 'e'), TokenElse, RoleKeyword)
ruleset.WithRule(pattern.Class(pattern.Range('a', 'z')).Plus(), TokenIdentifier, RoleIdentifier)
ruleset.WithRule(pattern.Class(pattern.Range('0', '9')).Plus(), TokenNumber, RoleLiteral)

// Custom resolution (first match)
rulesetFirst := lexarch.LexingRulesetCreate[rune, TokenType, TokenRole](lexarch.TokenResolutionStepFirst[TokenType])
rulesetFirst.WithRule(pattern.Literal('i', 'f'), TokenIf, RoleKeyword)

// Priority-based resolution
rulesetPriority := lexarch.LexingRulesetCreate[rune, TokenType, TokenRole](lexarch.TokenResolutionStepPriority[TokenType])
rulesetPriority.WithRulePriority(pattern.Literal('i', 'f'), TokenIf, RoleKeyword, 10)
rulesetPriority.WithRule(pattern.Class(pattern.Range('a', 'z')).Plus(), TokenIdentifier, RoleIdentifier)
```

### Lexer Creation

#### `LexerCreate[TObservation, TState, TToken, TTokenRole](inputRulesets map[TState]LexingRuleset, eofToken TToken, scratchAllocationFn AllocationFn, maxDFAAllocatorMemory MemoryUnitBytes, nfaToDFAPipelineMinTemp MemoryUnitBytes, nfaToDFAPipelineMaxTemp MemoryUnitBytes, observationCtx ObservationCTX[TObservation], compilationMode CompilerMode) *Lexer`

Compiles a set of rulesets into a ready-to-use lexer. Each state's ruleset is compiled to a minimized DFA.
The `eofToken` is returned when the end of input is reached. `observationCtx` provides observation formatting and domain/toBytes for compilation; use `ObservationCTXCreate(formatter, domain, toBytes)`. `compilationMode` is `lexarch.Thompson` or `lexarch.Glushkov` for NFA construction. `nfaToDFAPipelineMinTemp` and `nfaToDFAPipelineMaxTemp` configure the temporary allocator used during NFA-to-DFA conversion and DFA minimization (e.g. 1*KiloByte, 1*GigaByte).

**Example:**

```go
import (
    "memarch"
    "memcore"
    "memforge"
    "lexarch"
    "autarch/pattern"
    "foundation/domain"
)

type LexerState int
const (
    StateNormal LexerState = iota
    StateString
    StateComment
)

type TokenType int
const (
    TokenError TokenType = iota
    TokenEOF
    TokenIf
    TokenElse
    TokenIdentifier
    TokenNumber
)

type TokenRole int
const (
    RoleKeyword TokenRole = iota
    RoleIdentifier
    RoleLiteral
)

normalRules := lexarch.LexingRulesetCreate[rune, TokenType, TokenRole](nil)
normalRules.WithRule(pattern.Literal('i', 'f'), TokenIf, RoleKeyword)
normalRules.WithRule(pattern.Class(pattern.Range('a', 'z')).Plus(), TokenIdentifier, RoleIdentifier)

stringRules := lexarch.LexingRulesetCreate[rune, TokenType, TokenRole](nil)
stringRules.WithRule(pattern.Class(pattern.Range('a', 'z'), pattern.Range('A', 'Z')).Star(), TokenStringContent, RoleLiteral)

rulesets := map[LexerState]*lexarch.LexingRuleset[rune, TokenType, TokenRole]{
    StateNormal: normalRules,
    StateString: stringRules,
}

obsCtx := lexarch.ObservationCTXCreate(
    lexarch.RuneFormatterDefault(),
    lexarch.LexarchRuneDomain(),
    lexarch.RunesToBytesDefault(),
)
scratchAlloc := memarch.StackAllocatorCreate(1 * memcore.MegaByte)
lexer := lexarch.LexerCreate(
    rulesets,
    TokenEOF,
    scratchAlloc.Allocate,
    10 * memcore.MegaByte,
    1 * memcore.KiloByte,
    1 * memcore.GigaByte,
    obsCtx,
    lexarch.Glushkov,
    lexarch.LexerScanConfigDefault(),
)
defer lexarch.LexerClose(lexer)
```

#### `LexerClose[TObservation, TState, TToken](lexer *Lexer)`

Releases all resources associated with the lexer. Must be called when done with the lexer.

### Session Management

#### `LexerSessionCreate[TObservation, TState, TToken](initialState TState, input []TObservation, newlineDetector NewlineDetector[TObservation], columnAdvanceFn ColumnAdvanceFn[TObservation]) *LexerSession`

Creates a new lexing session with the specified initial state and input stream. The `newlineDetector` callback is used to identify newline characters for position tracking. The `columnAdvanceFn` computes the next column from an observation and current column (e.g. `ColumnAdvanceRune(tabWidth)` for runes). Use `NewlineDetectorRune()` or `NewlineDetectorByte()` for common cases.

#### `LexerSessionCreateRuneFast[TState, TToken, TTokenRole](initialState TState, input []rune, tabWidth int) *LexerSession`

Creates a rune session with explicit fast-path position tracking. This inlines newline and tab handling in the position loop and avoids per-character callback dispatch for line/column updates.

#### `LexerSessionCreateByteFast[TState, TToken, TTokenRole](initialState TState, input []byte) *LexerSession`

Creates a byte session with explicit fast-path position tracking. This inlines newline detection and default column advancement in the position loop.

#### `LexerSessionSetState[TObservation, TState](session *LexerSession, state TState)`

Changes the current lexer state, switching to a different ruleset for subsequent token recognition.

**Example:**

```go
input := []rune("if x else y")
session := lexarch.LexerSessionCreate(
    StateNormal,
    input,
    lexarch.NewlineDetectorRune(),
    lexarch.ColumnAdvanceRune(4),
)

// Switch to string state when entering string literal
lexarch.LexerSessionSetState(session, StateString)
```

### Token Recognition

#### `LexerConsume[TObservation, TState, TToken, TTokenRole](lexer *Lexer, session *LexerSession) Lexeme`

Recognizes and consumes the next token from the input stream at the current position. Advances the session position past the recognized token. Returns the EOF lexeme when the end of input is reached or when the session has a lexing error (check `session.GetLastError()`).

#### `LexerPeek[TObservation, TState, TToken, TTokenRole](lexer *Lexer, session *LexerSession, n int) Lexeme`

Recognizes the n-th upcoming token without advancing the session position. Returns the EOF lexeme when past end of input or on error.

#### `LexerAssertConsume[TObservation, TState, TToken, TTokenRole](lexer *Lexer, session *LexerSession, expected TToken) Lexeme`

Consumes the next token and verifies it matches the expected token type. Sets `session.lastError` (via `GetLastError()`) if mismatch.

#### `LexerAssertPeek[TObservation, TState, TToken, TTokenRole](lexer *Lexer, session *LexerSession, expected TToken, n int) Lexeme`

Peeks at the n-th token and verifies it matches the expected token type without consuming it.

**Example:**

```go
for {
    lexeme := lexarch.LexerConsume(lexer, session)
    if session.GetLastError() != nil {
        fmt.Printf("Lexing error at position %d: %v\n", session.Position(), session.GetLastError())
        break
    }

    if lexeme.Token == TokenEOF {
        fmt.Println("Reached end of input")
        break
    }

    fmt.Printf("Token: %v, Raw: %s, Position: %d-%d, Line: %d, Column: %d\n",
        lexeme.Token, string(lexeme.Raw), lexeme.Start, lexeme.End,
        lexeme.StartLine, lexeme.StartColumn)

    if lexeme.Token == TokenStringStart {
        lexarch.LexerSessionSetState(session, StateString)
    } else if lexeme.Token == TokenStringEnd {
        lexarch.LexerSessionSetState(session, StateNormal)
    }
}
```

### Data Structures

#### `Lexeme[TObservation, TToken, TTokenRole]`

Represents a recognized token containing:

- `Raw []TObservation`: The raw observation sequence that matched
- `Token TToken`: The token type
- `Role TTokenRole`: The role assigned when the rule was added
- `Start, End int`: Byte/observation position in the input stream
- `StartLine, StartColumn int`: Line and column where token starts (1-indexed)
- `EndLine, EndColumn int`: Line and column where token ends (1-indexed)
- `TokenNumber int`: Sequence number of this token (1-indexed)

#### `LexerSession[TObservation, TState, TToken]`

Maintains lexing state:

- `currentState TState`: Current lexer state (use `LexerSessionSetState` to change)
- `input []TObservation`: Input stream (reference)
- `position int`: Current position in input (use `Position()` method; see snapshot/restore for rollback)

### Streaming API

The streaming API allows tokenization of input streams without requiring the full input in memory. It uses a callback **ObservationProducerFn** that writes observations into a buffer and reports EOF.

#### `ObservationProducerFn[TObservation]`

```go
type ObservationProducerFn[TObservation cmp.Ordered] func(dst []TObservation) (n int, eof bool, err error)
```

The producer writes up to `len(dst)` observations into `dst` and returns how many were written. If `eof` is true, no more data will follow. If `err` is non-nil, the lexing operation fails. Returning `(0, false, nil)` is allowed (no data yet; caller may retry).

#### `StreamingLexerSessionCreate[TObservation, TState, TToken](initialState TState, producer ObservationProducerFn[TObservation], newlineDetector NewlineDetector[TObservation], columnAdvanceFn ColumnAdvanceFn[TObservation], readChunkSize int, maxBufferedObservations int) *StreamingLexerSession`

Creates a new streaming lexing session. The producer is called to fill an internal buffer. `readChunkSize` is how many observations to request per producer call; `maxBufferedObservations` is the hard cap on buffer size (must be at least as large as the longest possible token).

#### `StreamingLexerSessionCreateRuneFast[TState, TToken, TTokenRole](initialState TState, producer ObservationProducerFn[rune], readChunkSize int, maxBufferedObservations int, tabWidth int) *StreamingLexerSession`

Creates a streaming rune session with explicit fast-path position tracking (inline newline/tab handling).

#### `StreamingLexerSessionCreateByteFast[TState, TToken, TTokenRole](initialState TState, producer ObservationProducerFn[byte], readChunkSize int, maxBufferedObservations int) *StreamingLexerSession`

Creates a streaming byte session with explicit fast-path position tracking (inline newline/default column handling).

**Example:**

```go
input := []rune("if x else y")
position := 0

producer := func(dst []rune) (n int, eof bool, err error) {
    if position >= len(input) {
        return 0, true, nil
    }
    end := position + len(dst)
    if end > len(input) {
        end = len(input)
    }
    n = copy(dst, input[position:end])
    position += n
    eof = position >= len(input)
    return n, eof, nil
}

session := lexarch.StreamingLexerSessionCreate(
    StateNormal,
    producer,
    lexarch.NewlineDetectorRune(),
    lexarch.ColumnAdvanceRune(4),
    256,
    4096,
)
```

#### `StreamingLexerSessionSetState[TObservation, TState, TToken](session *StreamingLexerSession, state TState)`

Changes the current lexer state for a streaming session.

#### `LexerConsumeStreaming`, `LexerPeekStreaming`, `LexerAssertConsumeStreaming`, `LexerAssertPeekStreaming`

Same as the non-streaming Consume/Peek/Assert variants but take `*StreamingLexerSession`. Check `session.GetLastError()` for errors.

**Example:**

```go
for {
    lexeme := lexarch.LexerConsumeStreaming(lexer, session)
    if session.GetLastError() != nil {
        fmt.Printf("Lexing error: %v\n", session.GetLastError())
        break
    }

    if lexeme.Token == TokenEOF {
        fmt.Println("Reached end of input")
        break
    }

    fmt.Printf("Token: %v, Raw: %s, Position: %d-%d, Line: %d, Column: %d\n",
        lexeme.Token, string(lexeme.Raw), lexeme.Start, lexeme.End,
        lexeme.StartLine, lexeme.StartColumn)

    if lexeme.Token == TokenStringStart {
        lexarch.StreamingLexerSessionSetState(session, StateString)
    } else if lexeme.Token == TokenStringEnd {
        lexarch.StreamingLexerSessionSetState(session, StateNormal)
    }
}
```

### Data Structures (streaming)

#### `StreamingLexerSession[TObservation, TState, TToken]`

Maintains streaming lexing state:

- `currentState TState`: Current lexer state
- `producer ObservationProducerFn[TObservation]`: Callback for observations
- `buffer []TObservation`: Internal buffer for lookahead
- `absPos int`: Absolute position in stream (consumed observations)
- `eof` / buffer length: Whether EOF reached and how much is buffered

### Position Tracking and Newline Detection

The library tracks position information for each token, including line numbers, column numbers, and token sequence numbers. Position tracking requires a `NewlineDetector` callback to identify newline characters.

#### `NewlineDetector[TObservation]`

A callback function type that determines if an observation represents a newline character:

```go
type NewlineDetector[TObservation cmp.Ordered] func(obs TObservation) bool
```

#### Built-in Newline Detectors

- `NewlineDetectorRune()`: Detects `'\n'` for rune observations (Unix/Linux line endings)
- `NewlineDetectorByte()`: Detects `'\n'` for byte observations (Unix/Linux line endings)

**Example:**

```go
// For rune-based lexing
session := lexarch.LexerSessionCreate(StateNormal, input, lexarch.NewlineDetectorRune(), lexarch.ColumnAdvanceRune(4))

// For byte-based lexing (column advance: one column per byte, or provide custom)
byteAdvance := func(b byte, col int) int { return col + 1 }
byteSession := lexarch.LexerSessionCreate(StateNormal, byteInput, lexarch.NewlineDetectorByte(), byteAdvance)

// Custom newline detector (e.g., for Windows \r\n)
customDetector := func(obs rune) bool {
    return obs == '\n' || obs == '\r'
}
customSession := lexarch.LexerSessionCreate(StateNormal, input, customDetector)
```

**Position Information in Lexemes:**

All lexemes include position information:
- `StartLine`, `StartColumn`: Where the token starts (1-indexed)
- `EndLine`, `EndColumn`: Where the token ends (1-indexed)
- `TokenNumber`: Sequence number of the token (1-indexed, increments on each consume)

**Example Usage:**

```go
lexeme, err := lexarch.LexerConsume(lexer, session)
if err != nil {
    fmt.Printf("Error at line %d, column %d: %v\n", 
        session.currentLine, session.currentColumn, err)
    return
}

fmt.Printf("Token %d at line %d, column %d-%d: %s\n",
    lexeme.TokenNumber,
    lexeme.StartLine, lexeme.StartColumn,
    lexeme.EndLine, lexeme.EndColumn,
    string(lexeme.Raw))
```

## Use Cases

- **Programming Language Lexers**: Tokenizing source code for compilers and interpreters
- **Protocol Parsers**: Recognizing structured data formats (JSON, XML, custom protocols)
- **Text Processing**: Extracting tokens from structured text (log files, configuration files)
- **Language Recognition**: Building tokenizers for domain-specific languages
- **Syntax Highlighting**: Tokenizing code for editor syntax highlighting engines
- **Data Validation**: Recognizing and validating structured input formats
- **Streaming Processing**: Tokenizing large files or network streams without loading into memory (streaming API)
- **Real-time Lexing**: Processing observations as they arrive from sensors or network sources (streaming API)

## Safety Guidelines

⚠️ **Important:**

1. **Memory Lifetime**: Lexers hold references to allocated DFAs. Ensure allocators remain valid for the lexer's lifetime. Always call `LexerClose` when done.

2. **Input Lifetime**: `Lexeme.Raw` is a slice into the original input for slice-based sessions. The input must remain valid for as long as lexemes are used. For streaming sessions, `Raw` is a copy.

3. **Peek Slice Lifetime**: In cached scan modes (`ScanModePreTokenizeAll`, `ScanModeCircularTokenBuffer`), `LexerPeekRange*` results are returned from session-owned reusable scratch storage. Treat returned slices as ephemeral views valid only until the next lexer call on the same session.

4. **State Validity**: The session's current state must have a corresponding ruleset in the lexer. Invalid states cause errors on token recognition.

5. **Allocator Sizing**: Ensure `maxDFAAllocatorMemory` is sufficient for DFA storage. The lexer panics if exceeded during compilation.

6. **Error handling**: When no pattern matches or on EOF, the lexer returns an EOF lexeme and may set `session.GetLastError()`. Check `GetLastError()` after Consume/Peek to detect lexing errors.

7. **EOF Token**: The EOF token is returned when the end of input is reached (position >= len(input) for slice, or producer returned eof and buffer is empty for streaming). The EOF lexeme has `Raw == nil` and `Start == End`.

8. **Concurrent Access**: Multiple sessions can use the same lexer concurrently, but each session should be used by a single goroutine.

9. **Streaming Buffer**: For streaming sessions, `maxBufferedObservations` must be >= longest possible token. Tokens exceeding the buffer will report a buffer limit error.

10. **ObservationCTX**: Use `ObservationCTXCreate(formatter, observationDomain, toBytes)`. For runes: `LexarchRuneDomain()`, `RunesToBytesDefault()`, and `RuneFormatterDefault()` or `RuneFormatterCreate(cfg)`.

11. **Debug Re-entrancy Guard**: Session misuse detection (`begin/end` re-entrancy checks) is enabled only in debug builds. Build with `-tags=debug` to enable guard panics; default builds remove this check for zero-overhead API entry paths.

12. **Position Tracking Fast Path**: Fast position tracking is explicit and opt-in. Use `*CreateRuneFast` or `*CreateByteFast` constructors for inlined newline/column updates. Custom callback constructors preserve exact callback semantics through the generic path. The fast kernel rebinds observation slices as `[]rune` or `[]byte` via `unsafe` (no per-element type assertions); only use the rune fast path when `TObservation` is `rune`, and the byte fast path when it is `byte`.

## Implementation Notes

### Inline Token Resolution

The lexer uses inline token resolution to eliminate allocations in the hot path. Instead of collecting all candidate tokens in a slice, the lexer maintains only the current best match and updates it incrementally as it scans. This provides the same flexibility as candidate collection but with zero allocations, making it suitable for high-performance lexing scenarios.

### Priority Storage in DFA Outcomes

Token priorities are stored directly in DFA state outcomes as part of a `TokenOutcome` struct, eliminating hash map lookups during token scanning. When a rule is compiled, its priority is embedded in the DFA outcome, allowing priority-based resolution to access priority values with zero lookups. This optimization is particularly important for large grammars with many accepting states during long token matches.

### Streaming Buffer Optimization

The streaming lexer uses the existing ring buffer directly for token extraction, avoiding dynamic buffer growth. During scanning, only the `bestEnd` position is tracked. After scanning completes, observations are copied directly from the ring buffer in a bounded loop (by token length). This eliminates:
- Dynamic slice growth and reallocation
- Two allocations per token (buffer growth + final copy)
- GC pressure in large streaming inputs

The ring buffer approach provides predictable memory usage and cache-friendly access patterns, making it suitable for high-throughput streaming scenarios.

### Longest Match Scanning

The lexer uses longest match scanning (or other configurable strategies) to resolve ambiguous patterns. When multiple patterns match at the same position, the resolution strategy determines which token is selected. The default strategy is longest match, which ensures that keywords are recognized over identifiers (e.g., "if" as a keyword rather than an identifier).

### DFA Compilation Process

1. Each pattern in a ruleset is compiled to an NFA using Thompson's construction, with token and priority stored as `TokenOutcome` in accepting states
2. NFAs are merged using alternation (OR) to create a single NFA for the ruleset
3. The NFA is converted to a DFA via subset construction using default outcome resolution (first match)
4. The DFA is minimized using Hopcroft's algorithm
5. The minimized DFA is stored for runtime token recognition

**Note**: DFA construction uses default outcome resolution (first match) since token resolution is handled during scanning. The DFA outcome is just an arbitrary valid outcome when NFA states merge; the real resolution happens during scan-time using the configured `TokenResolutionStepFn`.

### State-Based Lexing

State-based lexing enables context-sensitive tokenization. For example:

- Normal state: Recognizes keywords, identifiers, operators
- String state: Recognizes string content, escape sequences, string terminators
- Comment state: Recognizes comment content, comment terminators

State transitions are managed explicitly via `LexerSessionSetState`, allowing parsers to control lexer behavior based on recognized tokens.

## Accuracy and Limitations

- **Pattern Complexity**: Very complex patterns may create large NFAs that explode during DFA conversion. Monitor memory usage during lexer creation.
- **Alphabet Size**: Large alphabets (e.g., Unicode) may create large transition tables. Consider using character classes to reduce alphabet size.
- **Position Information**: Position tracking includes byte/observation positions, line numbers, column numbers, and token sequence numbers. Line and column numbers are 1-indexed.
- **Backtracking**: The lexer does not support backtracking. Once a token is consumed, the position cannot be rolled back. Use `Peek` for lookahead instead.
