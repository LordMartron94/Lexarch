# Lexarch Performance Analysis: Architecture and Real Bottlenecks

## Executive Summary

This document is an architectural deep-dive into how the Lexarch lexer actually operates
during parsing and tokenisation. It identifies the genuine CPU hot paths, the real
allocation sources, and the root causes behind the profiling numbers. It explicitly
corrects the misconception that `dfaStateSubsetCreate` is a parse-time cost.

Key findings:

| Observation | Root Cause |
|---|---|
| `lexerPeekRangeCoreInto` 41.34% self-time | Position tracking re-scans every raw token a second time |
| 77.07% cumulative time flows through `lexerPeekRangeCoreInto` | All multi-token peek/consume operations bottom out here |
| `scanCoreSlice` 58.18% cumulative | Inner DFA stepping loop — one `DFAStep` call per input observation |
| `runtime.duffcopy` 2.09s | `Lexeme` struct is 128–144 bytes and copied on every append/return |
| `dfaStateSubsetCreate` 938.55 MB | **One-time lexer compile cost only — never runs during parsing** |
| String token types ~10× slower than `uint32`/`uint8` | Larger struct footprint, slower equality comparison, more cache pressure |

---

## 1. Architecture Overview

### 1.1 The `Lexer` Type

```go
// lexarch_types.go
type Lexer[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable] struct {
    ruleSets         map[TState]*autarch.DFA[...]  // one compiled DFA per lexer state
    tokenResolutions map[TState]TokenResolutionStepFn[TToken]
    nonTerminalOutcome TokenOutcome[TToken, TTokenRole]
    dfaAllocator     memcore.MarkRaw  // custom linear allocator owning all DFA memory
    eofToken         TToken
    formatter        ObservationFormatter[TObservation]
    scanConfig       LexerScanConfig  // controls ScanModeAsIs / PreTokenizeAll / CircularBuffer
}
```

The `Lexer` is a **read-only compiled artifact**. After `LexerCreate` returns, nothing in
it changes during parsing. All tokenisation reads from the precomputed DFA transition
tables stored in `dfaAllocator`.

### 1.2 Scan Modes

Three modes govern how `Peek` and `Consume` are served:

| Mode | Behaviour | Best For |
|---|---|---|
| `ScanModeAsIs` | Every call re-runs DFA from current position | Single-token consume with no lookahead |
| `ScanModePreTokenizeAll` | Tokenises entire input once on first call, then indexes | Heavy lookahead / repeated peek at same position |
| `ScanModeCircularTokenBuffer` | Maintains a bounded ahead-window, refills on demand | Bounded lookahead (e.g. LL(k)) |

### 1.3 Session Types

`LexerSession` (slice-based) and `StreamingLexerSession` hold per-invocation mutable
state: current input position, line/column counters, token number, DFA cursor cache,
and scratch buffers. Multiple sessions can share one `Lexer` concurrently.

### 1.4 `scannerContext` — The Abstraction Layer

```go
// lexarch_scan_core.go
type scannerContext[TObservation cmp.Ordered] struct {
    next        func(int) (TObservation, bool, error)  // read observation at offset i
    slice       func(start, end int) []TObservation    // sub-slice of current window
    directInput func() ([]TObservation, int)           // fast path: raw slice + offset
    remaining   func() int
    atEOF       func() bool
    position    func() int
    advanceRaw  func(raw []TObservation)  // advance position past a matched token
    rawRequiresCopy bool
}
```

Slice-based sessions always populate `directInput`, which allows `scanOne` to bypass
all function pointers and call `scanCoreSlice` directly with a raw slice + offset.
Streaming sessions leave `directInput == nil` and instead use the `next` callback, so
they go through `scanCoreStreaming` with a per-observation function call.

---

## 2. The Real Hot Path: Call Chain During Parsing

### 2.1 Entry Points

For a single-token consume (`LexerConsume`):

```
LexerConsume
  └─ scanCoreSlice  (direct, no lexerPeekRangeCoreInto)
```

For every other operation — including the lookahead pattern that most parsers use:

```
LexerPeek(n)
  └─ lexerPeekRangeWithContext(count = n+1)
       └─ lexerPeekRangeCoreInto(count = n+1)
            └─ for each token:
                 ├─ ctx.atEOF()                    [indirect call]
                 ├─ scanOne(ctx, dfa, ...)
                 │    └─ scanCoreSlice(dfa, cursor, input, offset, ...)
                 │         └─ for each observation:
                 │              ├─ autarch.DFAStep(dfa, state, obs, cursor)
                 │              ├─ autarch.DFAIsDeadState(dfa, nextState)
                 │              └─ autarch.DFAStateOutcome(dfa, state)   [if accepting]
                 │                   └─ resolutionStep(...)
                 ├─ computePositionFromSlice(raw, ...)  [re-scans raw token]
                 ├─ lexemeBuild(...)                    [returns Lexeme by value]
                 ├─ append(out, lex)                    [copies Lexeme ~128 bytes]
                 └─ ctx.advanceRaw(raw)                 [indirect call]

LexerConsumeRange(count)
  └─ lexerPeekRangeWithContext(count)
       └─ lexerPeekRangeCoreInto(count)
            └─ (same as above)

LexerPeekRange(count)
  └─ (same as above)
```

The key insight: **`LexerConsume` does NOT go through `lexerPeekRangeCoreInto`**. If a
parser alternates `LexerPeek(0)` for lookahead and `LexerConsume` for advancement,
each `LexerPeek` invokes `lexerPeekRangeCoreInto` and re-scans from the current
position. This is the dominant usage pattern that drives the 77.07% cumulative figure.

### 2.2 What `lexerPeekRangeCoreInto` Does Per Token

```go
// lexarch_scan_core.go, lines 537–669
for i := 0; i < count; i++ {
    // 1. Check EOF via closure
    if ctx.atEOF() { ... break }

    // 2. Run DFA scan — dispatches to scanCoreSlice for slice mode
    token, tokenRole, raw, found, currentDFAState, lexErr :=
        scanOne(ctx, dfa, cursor, resolutionStep, forceRawCopy, nonTerminalOutcome)

    // 3. Re-scan raw token for line/column tracking  ← major self-time contributor
    endLine, endCol := computePositionFromSlice(raw, positionTracking, line, col)

    // 4. Build Lexeme value (~128 bytes)
    lex := lexemeBuild(lexer.formatter, token, raw, ...)

    // 5. Append to output slice — copies the Lexeme struct (duffcopy)
    out = append(out, lex)

    // 6. Advance the simulated position via closure
    ctx.advanceRaw(raw)
}
```

Step 3 is where 41.34% self-time lives (see Section 3 for the full explanation).

---

## 3. Why `lexerPeekRangeCoreInto` Shows 41.34% Self-Time

The Go profiler attributes time to the innermost executing function. The 41.34% self-time
does **not** include time inside `scanCoreSlice` (which is a separate call frame). It
represents work done directly inside `lexerPeekRangeCoreInto` itself.

### 3.1 Position Tracking — The Double-Scan Problem

After `scanOne` returns `raw` (a slice of the matched token), `lexerPeekRangeCoreInto`
calls `computePositionFromSlice(raw, ...)`, which iterates over **every observation in
the matched token** a second time to count newlines and advance column counters:

```go
// lexarch_scan_core.go, lines 141–161
func computePositionFromSliceRuneFastRunes(observations []rune, tabWidth int, ...) ... {
    for _, r := range observations {    // ← O(m) loop over every character
        if r == '\n' { line++; column = 1; continue }
        if r == '\t' { column = column + ...; continue }
        column++
    }
}
```

For a typical token of length `m`, the total per-token work is O(m) in `scanCoreSlice`
plus another O(m) in `computePositionFromSlice`. This effectively **doubles the
character-level iteration cost** for every token.

When the Go compiler inlines `computePositionFromSlice` and its helpers
(`computePositionFromSliceRuneFast`, `unsafeSliceAsRune`) into
`lexerPeekRangeCoreInto`, the loop body and the branch-heavy character classification
show up as self-time on `lexerPeekRangeCoreInto` in the pprof flat profile.

### 3.2 Lexeme Construction and Copying

`lexemeBuild` returns a `Lexeme` by value. The caller (`lexerPeekRangeCoreInto`) then
calls `append(out, lex)`. Both operations trigger `runtime.duffcopy` because the struct
is wider than a register pair (Section 5 details the exact size). This contributes
additional self-time on the append line.

### 3.3 Indirect Function Calls on the Context

`ctx.atEOF()` and `ctx.advanceRaw(raw)` are Go function values stored in the
`scannerContext` struct. Calling them requires loading the function pointer and its
context from memory before branching. While each individual call is cheap, across
millions of tokens these indirect calls add measurable overhead because the CPU cannot
speculate the branch target reliably.

---

## 4. `scanCoreSlice` — The Inner DFA Loop

```go
// lexarch_scan_core.go, lines 333–420
func scanCoreSlice(...) ... {
    state := uint64(0)
    pos := 0
    for {
        inputPos := offset + pos
        if inputPos >= len(input) { break }
        obs := input[inputPos]                          // direct array access — fast

        nextState, err := autarch.DFAStep(dfa, state, obs, cursor)  // external call
        if autarch.DFAIsDeadState(dfa, nextState) { ... break }

        state = nextState

        if outcome, ok := autarch.DFAStateOutcome(dfa, state); ok && ... {
            newBest, newEnd, updated := resolutionStep(...)           // function value call
            if updated { bestToken = ...; found = true }
        }
        pos++
    }
}
```

The fast path for slice mode (`directInput != nil`) means `scanOne` calls
`scanCoreSlice` directly with the raw input slice and the current offset. There is no
`ctx.next` indirection inside the DFA loop — `input[inputPos]` is a direct array load.

The 58.18% cumulative figure means 58.18% of all profiling samples pass through this
function's call frame (including its callees in `autarch`). The bottleneck inside this
loop is the pair of external `autarch` calls:

- `autarch.DFAStep` — looks up the transition table: `table[state * alphabetSize + symbolID]`
- `autarch.DFAStateOutcome` — checks if the current state is accepting and retrieves the annotation

Both are external package calls that the Go compiler cannot inline. Each
`autarch.DFAStep` is an indexed array access plus symbol-to-ID mapping via a sorted
lookup structure; the exact cost depends on the DFA alphabet size and memory layout of
the `autarch` library.

---

## 5. `runtime.duffcopy` at 2.09s — Root Cause

Go's compiler emits `runtime.duffcopy` (Duff's device loop unrolling) for struct copies
of medium size (typically 32–4096 bytes). The `Lexeme` struct is the primary driver.

### 5.1 Lexeme Struct Size

```go
// lexarch_lexeme.go
type Lexeme[TObservation cmp.Ordered, TToken, TTokenRole comparable] struct {
    Raw         []TObservation   // 24 bytes  (slice header: ptr + len + cap)
    Token       TToken           // 4 bytes (uint32)  OR  16 bytes (string)
    Start       int              // 8 bytes
    End         int              // 8 bytes
    StartLine   int              // 8 bytes
    EndLine     int              // 8 bytes
    StartColumn int              // 8 bytes
    EndColumn   int              // 8 bytes
    TokenNumber int              // 8 bytes
    Role        TTokenRole       // 1 byte (uint8)  OR  16 bytes (string)
    formatter   ObservationFormatter[TObservation]  // 32 bytes (two func values)
}
```

`ObservationFormatter[TObservation]` is a plain struct containing two Go function
values (each 16 bytes: code pointer + closed-over data pointer):

```go
// lexarch_error.go
type ObservationFormatter[TObservation cmp.Ordered] struct {
    FormatOne  func(TObservation) string    // 16 bytes
    FormatMany func([]TObservation) string  // 16 bytes
}
```

Size totals (on 64-bit):

| Type params | Raw | Token | Fields (7×int) | Role | formatter | Padding | **Total** |
|---|---|---|---|---|---|---|---|
| `rune, uint32, uint8` | 24 | 4 | 56 | 1 | 32 | 11 | **~128 bytes** |
| `rune, string, string` | 24 | 16 | 56 | 16 | 32 | 0 | **~144 bytes** |

A cache line is 64 bytes. Every `Lexeme` value spans at least **2 full cache lines**.

### 5.2 Where Copies Happen

Every one of these operations copies the entire Lexeme struct:

```
append(out, lex)                          ← in lexerPeekRangeCoreInto (hot)
return Lexeme{...}  from lexemeBuild      ← struct returned by value
return lexemes[n]   from LexerPeek        ← indexed return by value
copy(out, toks[start:end])                ← in pre-tokenise mode
lex := toks[idx]                          ← in consume-from-pre-tokenised
for _, lex := range segment { ... }       ← in ConsumeRange
```

The 2.09s attributed to `runtime.duffcopy` is the aggregate of all these copies across
potentially millions of tokens in a file parse.

---

## 6. `dfaStateSubsetCreate` — Confirmed Non-Issue at Parse Time

`dfaStateSubsetCreate` is part of the `autarch` dependency and implements the
**NFA-to-DFA powerset (subset) construction** algorithm. It runs **once**, inside
`LexerCreate()`, as part of compiling each ruleset's NFA into a minimised DFA.

```go
// lexarch_compile.go — called once per state per LexerCreate invocation
compiled := lexingRulesetCompile(ruleset, scratchAllocationFn, dfaAllocFn,
    nfaToDFAPipelineMinTemp, nfaToDFAPipelineMaxTemp,
    observationDomain, toBytes, compiler, nonTerminalOutcome)
```

The pipeline inside `autarch` is:
1. Compile each token pattern (RegulaAST) to an NFA — Thompson or Glushkov construction
2. Merge all NFAs for the ruleset via alternation
3. Run `dfaStateSubsetCreate` (NFA → DFA) — the 938.55 MB of temporary allocations
4. Minimise the DFA with Hopcroft's algorithm — `O(s log s)`
5. Store the result in `dfaAllocator` (a custom linear allocator)

After `LexerCreate` returns, the compiled DFA is immutable and `dfaStateSubsetCreate`
is never called again. The 938.55 MB is **temporary working memory** used during
compilation; it is freed (or recycled by the scratch allocator) once compilation
completes. None of these allocations occur during tokenisation.

**The profiler attribute is correct but was misread.** If `dfaStateSubsetCreate` appears
in a benchmark profile, it is because the benchmark includes `LexerCreate` in its
measured region, not because parsing triggers subset construction.

---

## 7. Genuine Allocation Sources During Parsing

With `ScanModeAsIs` on the slice-based API, the allocations that actually occur during
tokenisation are:

### 7.1 `LexingError` Structs (Cold Path Only)

```go
// Only on lex error:
return bestToken, ..., &LexingError[TObservation, TToken]{
    Position: pos, Reason: LexErrNoTransition, ...
}
```

`LexingError` is heap-allocated with `&LexingError{...}` only when a real lex error
occurs. On a well-formed input this never fires. It is not a hot-path allocation.

### 7.2 `autarch.DFAAvailableSymbols` (Cold Path Only)

```go
expected := autarch.DFAAvailableSymbols(dfa, state)
```

This call allocates a `[]autarch.SymbolDefinition` slice. It is called only when
building error diagnostics — again, off the happy path entirely.

### 7.3 Scan Cache Scratch Buffers (First Call Only)

```go
func lexemeScratchCoreRangeReset(cache, required int) []Lexeme {
    if cap(cache.coreRangeScratch) < required {
        cache.coreRangeScratch = make([]Lexeme[...], 0, required)  // ← allocates once
    }
    return cache.coreRangeScratch[:0]
}
```

The first call to `LexerPeek(n)` with a given `n` allocates a scratch buffer of
capacity `n+1`. Subsequent calls reuse it (`:= cache.coreRangeScratch[:0]`). After
warm-up there are zero per-call allocations from scratch buffers.

### 7.4 Streaming Mode: `copyRaw` Per Token

```go
// lexarch_scan_core.go
if forceRawCopy || ctx.rawRequiresCopy {
    raw = copyRaw(ctx.slice(0, endRel))   // ← make([]T, len(src))
}
```

The streaming API always sets `rawRequiresCopy = true` because the internal buffer can
be compacted or extended. Each token produced by the streaming path incurs one
heap allocation of size `endRel × sizeof(TObservation)`. For the slice API,
`rawRequiresCopy = false` and `raw` is a zero-copy sub-slice of the input — no
allocation.

### 7.5 DFA Cursor Cache (First Access Per DFA)

```go
func lexerSessionCursorGet(session, dfa) memstruct.ArrayCursor[uint64] {
    if cursor, ok := session.dfaCursors[dfa]; ok {
        return cursor  // cached — O(1)
    }
    cursor := autarch.DFACursorGet(dfa)
    session.dfaCursors[dfa] = cursor  // stores in map, may trigger map growth
    return cursor
}
```

The cursor lookup is a map access keyed on a pointer. The cursor itself (likely a
small struct) is allocated once per DFA per session. Map insertion may cause a
rehash on the first few DFAs. This is a one-time setup cost per session, not a
per-token cost.

### Summary: Zero Steady-State Allocations (Slice API, Happy Path)

Once the session has been warmed up (scratch buffers allocated, DFA cursors cached),
a correctly-formed input tokenised via the slice API produces **zero heap allocations
per token** in `ScanModeAsIs`. The `runtime.duffcopy` cost is stack-to-stack or
stack-to-heap struct copying, not heap allocation.

---

## 8. String vs. `uint32`/`uint8` — The 10× Performance Gap

The benchmark measurement ("string variant ~14.84% vs uint32/uint8 ~1.45% of total
time") reflects a real structural difference in how the two parameterisations affect
memory layout and equality semantics.

### 8.1 DFA Annotation Size

Every DFA accepting state stores an annotation of type
`pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]`.

For `TToken = uint32, TTokenRole = uint8`:
```
TokenOutcome { Token: uint32(4), Role: uint8(1), Priority: int(8) } = ~16 bytes
```

For `TToken = string, TTokenRole = string`:
```
TokenOutcome { Token: string(16), Role: string(16), Priority: int(8) } = ~40 bytes
```

The DFA transition table and state annotation array are stored contiguously. With string
token types, each annotation is 2.5× larger (40 bytes vs 16 bytes): a 64-byte cache
line holds 4 integer annotations but only 1 complete string annotation (with the second
spanning to the next cache line). In practice this means 2–4× more cache misses per
DFA accepting-state check, which compounds across the entire DFA traversal for every
token.

### 8.2 `TokenOutcome` Equality Check (Hot Path)

In `scanCoreSlice`, every accepting state triggers:

```go
if outcome, ok := autarch.DFAStateOutcome(dfa, state); ok && outcome.Value != nonTerminalOutcome {
```

`outcome.Value != nonTerminalOutcome` compares two `TokenOutcome[TToken, TTokenRole]`
structs. For integer types this is two integer comparisons (fast, register operations).
For string types each field comparison is:
1. Length check (integer compare)
2. Pointer comparison (fast if interned, otherwise…)
3. `runtime.memequal` for content (O(n) on the string length)

Since token type names in a real language grammar are short strings (e.g. `"IDENT"`,
`"LBRACE"`), the memequal call is fast in practice, but it still cannot be reduced to a
single CPU instruction and has branch overhead. More importantly, it inhibits Go's
ability to keep `nonTerminalOutcome` in a register across loop iterations.

### 8.3 Lexeme Struct Size and Cache Impact

A `Lexeme[rune, string, string]` is ~144 bytes vs ~128 bytes for
`Lexeme[rune, uint32, uint8]`. When a parser processes a file with tens of thousands of
tokens, the output slice of Lexemes is:

- 128-byte Lexeme × 50,000 tokens = ~6.1 MB → fits in L3 on most CPUs
- 144-byte Lexeme × 50,000 tokens = ~6.9 MB → 13% more cache pressure

Each `append(out, lex)` copies 16 more bytes with the string variant, and each
`copy(out, toks[start:end])` bulk-copies proportionally more data.

### 8.4 Go Generics Monomorphisation

Go 1.18+ generics uses a combination of monomorphisation (for GC shape-compatible
types) and dictionary-based dispatch (for types with different GC shapes). `string` and
`uint32`/`uint8` have different GC shapes — `string` is a pointer-bearing type while
integers are not. This means the two parameterisations may get different code-generation
paths, with the string variant potentially using dictionary calls for some operations.
The practical effect on modern Go versions is typically small but non-zero.

---

## 9. Token Streaming and Lookahead Patterns

### 9.1 The Re-Scan Cost of `ScanModeAsIs`

With the default `ScanModeAsIs`, every call to `LexerPeek(n)` scans from the current
position to produce `n+1` tokens and then discards all but token `n`. If a parser's
lookahead pattern is:

```go
t0 := LexerPeek(lexer, session, 0)  // scans tokens 0..0 from position
t1 := LexerPeek(lexer, session, 1)  // scans tokens 0..1 from position (redundant)
t2 := LexerPeek(lexer, session, 2)  // scans tokens 0..2 from position (redundant)
LexerConsume(lexer, session)         // scans token 0 again from position
```

Tokens 0 and 1 are scanned three and two times respectively before being consumed. In
general, for a parser that peeks to depth D (calls `LexerPeek(0)` through `LexerPeek(D)`)
before each consume, the total number of token-scans via `lexerPeekRangeCoreInto` per
consumed token is:

```
sum_{d=0}^{D} (d+1) = (D+1)(D+2)/2
```

Here D is the lookahead depth (the maximum `n` passed to `LexerPeek`). Each token-scan
is O(m) observations, giving O(D² × m) total observation-level work per consumed token,
vs O(m) with a cached approach.

### 9.2 `ScanModePreTokenizeAll` — The Correct Fix

`ScanModePreTokenizeAll` calls `lexerCollectAllFromContext` once on the first peek,
tokenises the entire remaining input, stores all tokens in `cache.preTokens`, and then
serves every subsequent `Peek`/`Consume` as an O(1) index into the pre-computed slice:

```go
func lexerConsumeFromPreTokenizedSession(...) {
    toks, ok := lexerEnsurePreTokenizedSessionCache(lexer, session)
    idx := session.tokenNumber - session.scanCache.baseTokenNumber
    lex := toks[idx]               // O(1) indexed access
    lexerApplyConsumedLexemeToSession(session, lex)
    return lex
}
```

This eliminates re-scanning entirely. The tradeoff is a one-time O(N) tokenisation
pass at first access, plus holding all tokens in memory simultaneously.

### 9.3 `ScanModeCircularTokenBuffer` — Bounded Lookahead

`ScanModeCircularTokenBuffer` pre-scans a window of `CircularBufferSize` tokens
(default 256) on demand. It eliminates re-scanning within the window while keeping
memory bounded. When the parser advances past the window boundary, the next window is
scanned. This is optimal for LL(k) parsers with small, known k.

---

## 10. Data-Backed Optimization Recommendations

### 10.1 Switch Scan Mode (Highest Impact, Zero Code Change)

For parsers that use any lookahead:

```go
// Before (default):
config := lexarch.LexerScanConfigDefault()  // ScanModeAsIs

// After — for LL(k) parsers with bounded lookahead:
config := lexarch.LexerScanConfig{
    Mode:               lexarch.ScanModeCircularTokenBuffer,
    CircularBufferSize: 8,  // tune to actual max lookahead depth
}

// After — for parsers that scan the full file:
config := lexarch.LexerScanConfig{
    Mode: lexarch.ScanModePreTokenizeAll,
}
```

Expected impact: eliminates the O(D²) re-scan penalty entirely. Using the formula
`(D+1)(D+2)/2` from Section 9.1, the theoretical reduction in `lexerPeekRangeCoreInto`
work per consumed token is:

| Lookahead depth D | Token-scans before (ScanModeAsIs) | After (cached) | Reduction |
|---|---|---|---|
| D=1 | 3 | 1 | **3×** |
| D=2 | 6 | 1 | **6×** |
| D=3 | 10 | 1 | **10×** |

Actual wall-clock speedup will be lower because `scanCoreSlice` (which is not affected
by re-scanning elimination) accounts for a large portion of total time.

### 10.2 Use Integer Token Types, Not Strings (High Impact)

Replace `string`-typed `TToken` and `TTokenRole` with `uint32`, `uint16`, or `uint8`:

```go
// Before:
type MyToken = string       // "IDENT", "KEYWORD_IF", ...
type MyRole  = string       // "identifier", "keyword", ...

// After:
type MyToken  uint32
type MyRole   uint8
const (
    TokenIdent    MyToken = iota + 1
    TokenKeywordIf
    // ...
)
```

Expected impact: ~10× reduction in token-type-related overhead from smaller structs,
single-instruction comparisons, and better DFA annotation cache locality.

### 10.3 Use `RuneFast` or `ByteFast` Sessions (Medium Impact)

When the observation type is `rune` or `byte`, use the fast-path session constructors:

```go
// Rune input:
session := lexarch.LexerSessionCreateRuneFast[TState, TToken, TTokenRole](
    initialState, input, tabWidth,
)

// Byte input:
session := lexarch.LexerSessionCreateByteFast[TState, TToken, TTokenRole](
    initialState, input,
)
```

These set `positionTracking.mode` to `positionTrackingModeRuneFast` /
`positionTrackingModeByteFast`, which selects the tight `computePositionFromSliceRuneFast`
/ `computePositionFromSliceByteFast` path (an `unsafe` reinterpret-cast + tight loop)
instead of the generic callback-based path. This is already the most optimised position
tracking available in the library for these types.

### 10.4 Omit Position Tracking When Not Needed (Targeted Impact)

Position tracking (`computePositionFromSlice`) accounts for a significant fraction of
`lexerPeekRangeCoreInto`'s 41.34% self-time because it re-iterates every matched token.
If downstream consumers only need token offsets (`Start`/`End`) and not line/column
information, consider a session variant that skips position tracking entirely.

This is a library-level change: adding a `positionTrackingModeNone` mode that makes
`computePositionFromSlice` a no-op. The `EndLine`/`EndColumn` fields in the produced
Lexemes would remain zero.

**Estimated impact:** The position-tracking loop is O(m) per token, the same complexity
as the DFA scan. Eliminating it could halve the work inside `lexerPeekRangeCoreInto`.

### 10.5 Reduce `Lexeme` Struct Size (Structural Change)

The `ObservationFormatter` stored in each `Lexeme` is used only for the
`DebugString()` / `FormatRawDiagnostic()` methods. Moving the formatter out of the
struct (e.g. making `DebugString` a package function that takes a separate formatter
argument) would remove 32 bytes from every `Lexeme`:

```go
// Current: formatter is inside Lexeme (32 bytes per Lexeme)
func (l Lexeme[...]) DebugString(fmtToken, fmtRole func) string { ... }

// Alternative: formatter passed in by caller (zero bytes per Lexeme)
func LexemeDebugString[...](l Lexeme[...], obs ObservationFormatter[...], ...) string { ... }
```

Expected impact: reduces each Lexeme from ~128 to ~96 bytes for integer types. Every
`Lexeme` copy in `append`, `return`, `copy(out, toks...)` becomes ~25% cheaper in
bytes transferred.

### 10.6 Avoid Repeated Single-Token Peek (Usage Pattern)

Replace the common anti-pattern:

```go
for {
    tok := LexerPeek(lexer, session, 0)   // re-scans each iteration
    if tok.Token == EOFToken { break }
    consume := LexerConsume(lexer, session) // re-scans again
}
```

With the more efficient equivalent:

```go
for {
    tok := LexerConsume(lexer, session)   // single scan, no peek overhead
    if tok.Token == EOFToken { break }
    // use tok directly
}
```

`LexerConsume` calls `scanCoreSlice` directly (bypassing `lexerPeekRangeCoreInto`)
and avoids the double-scan overhead entirely when lookahead is not needed.

---

## 11. Conclusion

The Lexarch profiling data reflects three distinct, separable performance phenomena:

1. **`dfaStateSubsetCreate` / 938.55 MB** — pure compile-time cost. Not relevant to
   parse throughput. The benchmark must be restructured to exclude `LexerCreate` from
   the timed region.

2. **`lexerPeekRangeCoreInto` 41.34% self-time** — position tracking
   (`computePositionFromSlice`) re-iterates the matched token character-by-character
   after `scanCoreSlice` already scanned it. Combined with the Lexeme struct copy
   overhead, this is the dominant addressable bottleneck for the lexer itself.

3. **`scanCoreSlice` 58.18% cumulative** — the DFA stepping loop is working correctly.
   Each token requires O(m) DFA steps where m is the token length. The only way to
   reduce this is to reduce how often the same input positions are scanned (fix the
   re-scan problem with an appropriate `ScanMode`).

The ~29ms parse time vs ~1ms target is achievable through a combination of:
- Switching to `ScanModePreTokenizeAll` or `ScanModeCircularTokenBuffer` to eliminate
  the O(k²) re-scan penalty from lookahead
- Using integer token types to cut struct sizes and comparison costs by ~2×
- Potentially eliminating position tracking when line/column information is not needed
- Using `LexerSessionCreateRuneFast` / `LexerSessionCreateByteFast` for the tightest
  position tracking path
