// Package lexarch provides a generic, state-based lexical analysis library for tokenizing input held in slices.
//
// The library uses deterministic finite automata (DFA) compiled from regular expression patterns
// to efficiently recognize tokens. It supports state-based lexing where different rulesets can be
// active depending on the current lexer state, enabling context-sensitive tokenization.
//
// Key features:
// - Generic type support for any ordered observation type (runes, bytes, tokens) and comparable token types
// - State-based lexing with different rulesets per state
// - Pretokenized lexeme cache: Peek/Consume index a materialized token stream built from the session cursor
// - Pattern-based token definitions using the autarch/pattern RegulaAST system
// - Position tracking with line numbers, column numbers, and token sequence numbers
// - Priority-based token resolution with priority stored directly in DFA outcomes (no map lookups)
//
// The public API is slice-based: a LexerSession holds the full input; the lexer builds a token cache on demand.
//
// The library integrates with autarch for finite automaton construction and memarch for memory management.
// All DFAs are compiled and minimized at lexer creation time, ensuring optimal runtime performance.
package lexarch
