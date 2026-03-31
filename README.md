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
