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

#### `LexingRulesetCreate[TObservation, TToken](tokenResolutionStep TokenResolutionStepFn[TToken]) *LexingRuleset`

Creates a new empty ruleset ready for pattern-to-token mappings. The `tokenResolutionStep` parameter
specifies how to resolve conflicts when multiple tokens match at the same position using inline resolution.
If `nil`, defaults to `TokenResolutionStepLongest` (longest match). The inline resolution API eliminates
allocations in the hot path by updating the best match incrementally during scanning.

#### `WithRule(pattern RegulaAST[TObservation], token TToken)`

Adds a pattern-to-token mapping to the ruleset. The pattern defines what input sequence matches this token.

#### `WithRulePriority(pattern RegulaAST[TObservation], token TToken, priority int)`

Adds a pattern-to-token mapping with an explicit priority. Higher priority values are preferred during
priority-based resolution. Priority is only used with priority-based resolution functions.

#### `WithTokenResolution(resolutionStep TokenResolutionStepFn[TToken]) *LexingRuleset`

Sets the token resolution step function for the ruleset. This determines which token is selected when
multiple tokens match at the same position using inline resolution (zero allocations).

**Example:**

```go
import "autarch/pattern"

// Default resolution (longest match)
ruleset := lexarch.LexingRulesetCreate[rune, TokenType](nil)
ruleset.WithRule(pattern.Literal('i', 'f'), TokenIf)
ruleset.WithRule(pattern.Literal('e', 'l', 's', 'e'), TokenElse)
ruleset.WithRule(pattern.Class(pattern.Range('a', 'z')).Plus(), TokenIdentifier)
ruleset.WithRule(pattern.Class(pattern.Range('0', '9')).Plus(), TokenNumber)

// Custom resolution (first match)
rulesetFirst := lexarch.LexingRulesetCreate[rune, TokenType](lexarch.TokenResolutionStepFirst[TokenType])
rulesetFirst.WithRule(pattern.Literal('i', 'f'), TokenIf)

// Shortest match resolution
rulesetShortest := lexarch.LexingRulesetCreate[rune, TokenType](lexarch.TokenResolutionStepShortest[TokenType])
rulesetShortest.WithRule(pattern.Class(pattern.Range('a', 'z')).Plus(), TokenIdentifier)

// Priority-based resolution
rulesetPriority := lexarch.LexingRulesetCreate[rune, TokenType](lexarch.TokenResolutionStepPriority[TokenType])
rulesetPriority.WithRulePriority(pattern.Literal('i', 'f'), TokenIf, 10) // Higher priority
rulesetPriority.WithRule(pattern.Class(pattern.Range('a', 'z')).Plus(), TokenIdentifier) // Default priority (0)
```

### Lexer Creation

#### `LexerCreate[TObservation, TState, TToken](inputRulesets map[TState]LexingRuleset, errorToken TToken, eofToken TToken, scratchAllocationFn AllocationFn, maxDFAAllocatorMemory MemoryUnitBytes) *Lexer`

Compiles a set of rulesets into a ready-to-use lexer. Each state's ruleset is compiled to a minimized DFA.
The `eofToken` is returned when the end of input is reached during tokenization.

**Example:**

```go
import (
    "memarch"
    "memcore"
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

normalRules := lexarch.LexingRulesetCreate[rune, TokenType](nil) // Default: longest match
normalRules.WithRule(pattern.Literal('i', 'f'), TokenIf)
normalRules.WithRule(pattern.Class(pattern.Range('a', 'z')).Plus(), TokenIdentifier)

stringRules := lexarch.LexingRulesetCreate[rune, TokenType](nil) // Default: longest match
stringRules.WithRule(pattern.Class(pattern.Range('a', 'z'), pattern.Range('A', 'Z')).Star(), TokenStringContent)

rulesets := map[LexerState]lexarch.LexingRuleset[rune, TokenType]{
    StateNormal: *normalRules,
    StateString: *stringRules,
}

scratchAlloc := memarch.StackAllocatorCreate(1 * memcore.MegaByte)
lexer := lexarch.LexerCreate(
    rulesets,
    TokenError,
    TokenEOF,
    scratchAlloc.Allocate,
    10 * memcore.MegaByte,
)
defer lexarch.LexerClose(lexer)
```

#### `LexerClose[TObservation, TState, TToken](lexer *Lexer)`

Releases all resources associated with the lexer. Must be called when done with the lexer.

### Session Management

#### `LexerSessionCreate[TObservation, TState](initialState TState, input []TObservation, newlineDetector NewlineDetector[TObservation]) *LexerSession`

Creates a new lexing session with the specified initial state and input stream. The `newlineDetector` callback is used to identify newline characters for position tracking. Use `NewlineDetectorRune()` or `NewlineDetectorByte()` for common cases, or provide a custom detector for special newline conventions.

#### `LexerSessionSetState[TObservation, TState](session *LexerSession, state TState)`

Changes the current lexer state, switching to a different ruleset for subsequent token recognition.

**Example:**

```go
input := []rune("if x else y")
session := lexarch.LexerSessionCreate(StateNormal, input, lexarch.NewlineDetectorRune())

// Switch to string state when entering string literal
lexarch.LexerSessionSetState(session, StateString)
```

### Token Recognition

#### `LexerConsume[TObservation, TState, TToken](lexer *Lexer, session *LexerSession) (Lexeme, error)`

Recognizes and consumes the next token from the input stream at the current position. Advances the session position past the recognized token. Returns the EOF token when the end of input is reached.

#### `LexerPeek[TObservation, TState, TToken](lexer *Lexer, session *LexerSession) (Lexeme, error)`

Recognizes the next token without advancing the session position. Useful for lookahead. Returns the EOF token when the end of input is reached.

#### `LexerAssertConsume[TObservation, TState, TToken](lexer *Lexer, session *LexerSession, expected TToken) (Lexeme, error)`

Consumes the next token and verifies it matches the expected token type. Returns error if mismatch.

#### `LexerAssertPeek[TObservation, TState, TToken](lexer *Lexer, session *LexerSession, expected TToken) (Lexeme, error)`

Peeks at the next token and verifies it matches the expected token type without consuming it.

**Example:**

```go
for {
    lexeme, err := lexarch.LexerConsume(lexer, session)
    if err != nil {
        fmt.Printf("Lexing error at position %d: %v\n", session.position, err)
        break
    }
    
    // Check for end of input
    if lexeme.Token == TokenEOF {
        fmt.Println("Reached end of input")
        break
    }
    
    fmt.Printf("Token: %v, Raw: %s, Position: %d-%d, Line: %d, Column: %d\n", 
        lexeme.Token, string(lexeme.Raw), lexeme.Start, lexeme.End,
        lexeme.StartLine, lexeme.StartColumn)
    
    // State transitions based on token
    if lexeme.Token == TokenStringStart {
        lexarch.LexerSessionSetState(session, StateString)
    } else if lexeme.Token == TokenStringEnd {
        lexarch.LexerSessionSetState(session, StateNormal)
    }
}
```

### Data Structures

#### `Lexeme[TObservation, TToken]`

Represents a recognized token containing:

- `Raw []TObservation`: The raw observation sequence that matched
- `Token TToken`: The token type
- `Start, End int`: Byte/observation position in the input stream
- `StartLine, StartColumn int`: Line and column where token starts (1-indexed)
- `EndLine, EndColumn int`: Line and column where token ends (1-indexed)
- `TokenNumber int`: Sequence number of this token (1-indexed)

#### `LexerSession[TObservation, TState]`

Maintains lexing state:

- `currentState TState`: Current lexer state
- `input []TObservation`: Input stream
- `position int`: Current position in input

### Streaming API

The streaming API allows tokenization of input streams without requiring pre-allocated slices. It uses callback-based observation providers and internal buffering.

#### `ObservationProvider[TObservation]`

A callback function type that provides observations on-demand:

```go
type ObservationProvider[TObservation cmp.Ordered] func(count int) ([]TObservation, error)
```

The provider:
- Returns up to `count` observations (or fewer if EOF)
- Returns empty slice (not error) when EOF is reached
- Returns error only for actual read failures

#### `LexerStreamingSessionCreate[TObservation, TState](initialState TState, provider ObservationProvider, bufferCapacity int, newlineDetector NewlineDetector[TObservation]) *LexerStreamingSession`

Creates a new streaming lexing session with the specified initial state and observation provider. The `newlineDetector` callback is used to identify newline characters for position tracking. Use `NewlineDetectorRune()` or `NewlineDetectorByte()` for common cases, or provide a custom detector for special newline conventions.

**Example:**

```go
input := []rune("if x else y")
position := 0

provider := func(count int) ([]rune, error) {
    if position >= len(input) {
        return nil, nil // EOF
    }
    end := position + count
    if end > len(input) {
        end = len(input)
    }
    result := input[position:end]
    position = end
    return result, nil
}

session := lexarch.LexerStreamingSessionCreate(StateNormal, provider, 1024, lexarch.NewlineDetectorRune())
```

#### `LexerStreamingSessionSetState[TObservation, TState](session *LexerStreamingSession, state TState)`

Changes the current lexer state for a streaming session.

#### `LexerStreamingConsume[TObservation, TState, TToken](lexer *Lexer, session *LexerStreamingSession) (Lexeme, error)`

Consumes the next token from the streaming session. Advances position past the recognized token.

#### `LexerStreamingPeek[TObservation, TState, TToken](lexer *Lexer, session *LexerStreamingSession) (Lexeme, error)`

Peeks at the next token without consuming it.

#### `LexerStreamingAssertConsume[TObservation, TState, TToken](lexer *Lexer, session *LexerStreamingSession, expected TToken) (Lexeme, error)`

Consumes and verifies the token matches the expected type.

#### `LexerStreamingAssertPeek[TObservation, TState, TToken](lexer *Lexer, session *LexerStreamingSession, expected TToken) (Lexeme, error)`

Peeks and verifies the token matches the expected type.

**Example:**

```go
for {
    lexeme, err := lexarch.LexerStreamingConsume(lexer, session)
    if err != nil {
        fmt.Printf("Lexing error: %v\n", err)
        break
    }
    
    if lexeme.Token == TokenEOF {
        fmt.Println("Reached end of input")
        break
    }
    
    fmt.Printf("Token: %v, Raw: %s, Position: %d-%d, Line: %d, Column: %d\n", 
        lexeme.Token, string(lexeme.Raw), lexeme.Start, lexeme.End,
        lexeme.StartLine, lexeme.StartColumn)
    
    // State transitions based on token
    if lexeme.Token == TokenStringStart {
        lexarch.LexerStreamingSessionSetState(session, StateString)
    } else if lexeme.Token == TokenStringEnd {
        lexarch.LexerStreamingSessionSetState(session, StateNormal)
    }
}
```

### Data Structures

#### `LexerStreamingSession[TObservation, TState]`

Maintains streaming lexing state:

- `currentState TState`: Current lexer state
- `provider ObservationProvider[TObservation]`: Callback for observations
- `buffer []TObservation`: Internal buffer for lookahead
- `absolutePosition int64`: Absolute position in stream
- `eofReached bool`: Whether EOF has been reached

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
session := lexarch.LexerSessionCreate(StateNormal, input, lexarch.NewlineDetectorRune())

// For byte-based lexing
byteSession := lexarch.LexerSessionCreate(StateNormal, byteInput, lexarch.NewlineDetectorByte())

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

2. **Input Lifetime**: `Lexeme.Raw` is a slice into the original input. The input must remain valid for as long as lexemes are used.

3. **State Validity**: The session's current state must have a corresponding ruleset in the lexer. Invalid states cause errors on token recognition.

4. **Allocator Sizing**: Ensure `maxDFAAllocatorMemory` is sufficient for DFA storage. The lexer panics if exceeded during compilation.

5. **Error Token**: The error token must be distinct from all valid token values. It is returned when no pattern matches.

6. **EOF Token**: The EOF token must be distinct from all valid token values and the error token. It is returned when the end of input is reached (position >= len(input)). The EOF lexeme has an empty Raw slice and Start/End both equal to len(input).

7. **Position Bounds**: Session position must be within input bounds or at end. EOF is returned when position reaches end of input.

8. **Concurrent Access**: Multiple sessions can use the same lexer concurrently, but each session should be used by a single goroutine.

9. **Streaming Buffer Capacity**: For streaming sessions, `bufferCapacity` must be >= longest possible token. Tokens exceeding capacity will return errors.

10. **Streaming Raw Data**: For streaming sessions, `Lexeme.Raw` contains allocated copies (not slices into input). Caller is responsible for lifetime.

11. **Provider Thread Safety**: Observation providers should be thread-safe if used concurrently with multiple sessions.

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
