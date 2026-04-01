# Dual Ownership Lexing States

STATUS: accepted
DECISION: Lexing state stack frames can be controlled by both lexer rules and parser API calls, with exclusive ownership guards.

## Rationale

The lexer and parser solve different but related concerns.
The lexer knows when lexical context should mutate as a direct result of token recognition (for example, rule-driven push/pop/set operations).
The parser, on the other hand, may need speculative or grammar-driven control over lexical context (for example, lookahead, recovery, or parser-owned mode forcing).

Using only lexer-controlled state transitions makes parser-driven workflows brittle.
Using only parser-controlled state transitions makes lexical rules less expressive and spreads lexical policy into higher layers.
We keep both, but make ownership explicit on each stack frame:

- Lexer-owned frames are created by lexer rule stack operations.
- Parser-owned frames are created by parser API operations.

Ownership enables strict safety boundaries:

- Parser mutation APIs must not pop or overwrite lexer-owned frames.
- Lexer rule operations must not pop or overwrite parser-owned frames.
- Illegal cross-owner mutation is a hard panic, signaling engine misuse.
- Snapshot/restore is an allowed exception because it is defined as full session time-travel, not incremental mutation.

This model preserves separation of concern:

- Lexical automata remain authoritative for token-driven mode transitions inside lexer-owned frames.
- Parsers remain free to orchestrate temporary context for speculative parsing inside parser-owned frames.
- The runtime composes both mutation sources with explicit ownership boundaries, preventing silent cross-owner stack corruption.

This decision favors explicitness, debuggability, and predictable failure behavior over permissive but ambiguous stack mutation semantics.
