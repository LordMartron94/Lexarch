# No Lexer-Level Token Filtering

STATUS: accepted
DECISION: The lexer must not implement skip-role/ignore-role filtering; all recognized tokens are emitted and filtering is a client concern.

## Rationale

Lexer-level filtering is destructive.
If the lexer hardcodes skipping of whitespace, comments, or doc tokens, that information is permanently lost before higher layers can make context-aware decisions.

Modern language tooling needs lossless token streams:

- language servers need comments/doc tokens for hover, signature help, and semantic context
- formatters need whitespace and comment structure to preserve intent and stable output
- documentation tooling needs doc comments and surrounding trivia
- refactoring tools need full lexical fidelity to avoid accidental content loss

If comments are discarded at lexing time, parsers cannot attach them to syntax nodes.
That prevents building a lossless syntax tree and blocks downstream tooling use cases.

This means lexarch keeps lexing generic and non-destructive:

- lexarch emits what it recognizes
- clients decide which tokens are relevant for a specific pipeline stage
- clients may filter at parse entry, AST construction, or post-parse phases

This preserves separation of concerns:

- lexer responsibility: correct recognition and location
- client responsibility: policy decisions about consumption, filtering, and retention

## Optional Alternatives Considered

Add configurable skip-role/ignore-role behavior directly to the lexer.

This was rejected because it couples domain policy into a generic lexing layer and introduces an easy path to irreversible data loss in toolchains that require full-fidelity source representation.
