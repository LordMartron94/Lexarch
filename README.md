# lexarch

State-aware lexer with explicit session APIs, DFA-backed matching, and byte-span based token locations.

## What This Library Provides

`lexarch` compiles per-state token rules into DFAs and exposes a function-oriented API to:

- configure a lexer (`LexerConfiguration*`)
- define states and rules (`LexingStateCreate`, `LexingRuleCreate`, stack op setters)
- run lexing sessions (`LexingSession*`)
- convert byte spans to line/column (`LexerByteSpanToPosition`)

The public API is intentionally explicit and mutable-by-function, not method-oriented.

## Core Concepts

- **Lexer**: compiled automata and stack operation tables.
- **Lexing Session**: mutable cursor over one source string with a state stack.
- **State Stack**: active lexical context; can be changed by parser APIs and by rule-driven stack ops.
- **Token**: `Kind`, `Role`, `FileID`, and `Span` (`Offset`, `Length`).
- **Errors**:
  - runtime errors for user input that cannot be lexed
  - panics for contract/invariant violations (developer/engine faults)

## Minimal Flow

1. Create configuration with `LexerConfigurationCreate`.
2. Create rules with `LexingRuleCreate`.
3. Build states with `LexingStateCreate`.
4. Register states via `LexerConfigurationRegisterState`.
5. Compile lexer with `LexerCreate`.
6. Start session with `LexerLexingSessionCreate`.
7. Reuse `LexingSessionNextResultCreate` and call `LexingSessionConsume` until EOF or error.

## Example

```go
package main

import (
    "autarch/pattern"
    "foundation/domain"
    "lexarch"
)

const (
    TokIdentifier lexarch.TokenKind = iota + 1
    TokWhitespace
)

func buildLexer() *lexarch.Lexer {
    cfg := lexarch.LexerConfigurationCreate()

    obsDomain := domain.DiscreteDomainRuneCreate()
    factory := pattern.RegulaASTFactoryCreate(obsDomain)
    templates := pattern.RegulaTemplatesCreate(factory)

    rules := []*lexarch.LexingRule{
        lexarch.LexingRuleCreate(templates.Whitespace().Plus(), 1, TokWhitespace, 0),
        lexarch.LexingRuleCreate(templates.Identifier(), 2, TokIdentifier, 0),
    }

    initial := lexarch.LexingStateCreate("INITIAL", rules)
    lexarch.LexerConfigurationRegisterState(cfg, initial, true)

    return lexarch.LexerCreate(cfg)
}

func main() {
    lexer := buildLexer()
    defer lexarch.LexerDestroy(lexer)

    session := lexarch.LexerLexingSessionCreate(lexer, "hello world", 0)
    out := lexarch.LexingSessionNextResultCreate()

    for {
        lexarch.LexingSessionConsume(session, out)
        if out.LexingError != nil {
            break
        }
        if out.EOF {
            break
        }
        _ = out.Token
    }
}
```

## Position Conversion

Tokens store byte spans only. Convert spans to human-readable line/column with:

- `LexerByteSpanToPosition(span, source, tabWidth)`

Coordinates are 1-indexed.

## Session Operations

- `LexingSessionConsume` / `LexingSessionPeek`: normal guarded operations
- `LexingSessionConsumeUnsafe` / `LexingSessionPeekUnsafe`: skip destroyed-lexer validation
- `LexingSessionPushStates`, `LexingSessionPop`, `LexingSessionSet`: parser-driven state stack mutation
- `LexingSessionSnapshotCreate`, `LexingSessionSnapshotRestore`: speculative parse support
- `LexingSessionPrefillCache`: pre-lex optimization path (currently only meaningful for single-state lexers)

## Safety Notes

- Always call `LexerDestroy` when done with a lexer.
- Reuse a `LexingNextResult` object in loops to avoid unnecessary allocations.
- If a parser API attempts illegal cross-owner stack mutation, the engine panics by design.
- Use snapshots for speculative flows; restore resets session position and stack state.

## Benchmark Scaling Criteria

The lexer benchmark suite publishes corpus-size variants (`small`, `medium`, `large`) with
the same metric keys so Anvil can compare trends directly.

Primary metrics:

- `chars/op`
- `tokens/op`
- `throughput.chars_per_sec`
- `throughput.tokens_per_sec`

Interpretation guidance:

- `tokens/op` must remain stable for a fixed corpus (deterministic output check).
- `chars/sec` and `tokens/sec` may decline with larger corpora, but should not collapse
  disproportionately between adjacent sizes under the same mode/hardware.
- Regression triage should compare the same suite mode and machine class first, then inspect
  callgrind profile output for hot-path shifts.

## Current Benchmark Snapshot

Baseline is `Corpus=small` with median `2.03 us/op`.

### Time/Op Summary

- `Corpus=small`: median `2.03 us`, mean `7.76 us`, `tokens/op=63`, `chars/op=87`
- `Corpus=medium`: median `25.08 us`, mean `34.68 us`, `tokens/op=875`, `chars/op=1.38k`
- `Corpus=large`: median `48.08 us`, mean `52.70 us`, `tokens/op=1.75k`, `chars/op=2.90k`

Absolute `time/op` rises strongly with larger files, which is expected because each operation
processes far more input and emits far more tokens.

### Throughput Summary

- `throughput.chars_per_sec`
  - small: `42.8M`
  - medium: `55.1M`
  - large: `60.4M`
- `throughput.tokens_per_sec`
  - small: `31.0M`
  - medium: `34.9M`
  - large: `36.4M`

Throughput does not collapse as corpus size grows; it improves in this run. That indicates
the lexer hot path remains stable under larger workloads and that fixed per-iteration overheads
are amortized better on medium/large corpora.

### Allocation/GC Summary

- allocs/op stays around `1.00-1.02`
- `gc.count` remains `0` and `gc/op` remains `0.000`

This indicates the benchmark is largely allocation-stable and not dominated by GC in these runs.

### Variability Notes

- `small` shows high CV (`156%`) and long-tail outliers (`p95/p99`) due to very short operation time.
- `medium` and `large` have lower relative variance (`59%`, `22%` CV respectively), so scaling comparisons are more reliable there.

Interpretation rule for regressions: prioritize changes in `throughput.chars_per_sec` and
`throughput.tokens_per_sec`, then use `time/op` as a secondary metric after normalizing for
`chars/op` and `tokens/op`.
