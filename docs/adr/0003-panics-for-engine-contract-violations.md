# Panics For Engine Contract Violations

STATUS: accepted
DECISION: Lexarch uses runtime errors for user-input lexing failures, and panics for developer or engine contract violations.

## Rationale

Lexing failures do not all have the same meaning.
Some failures are expected outcomes of untrusted input.
Other failures indicate that engine invariants were violated or that grammar integration code is incorrect.
These two classes must be separated clearly in behavior and diagnostics.

Runtime errors are used for user-input failures.
Examples include:

- unexpected character at a specific byte offset
- invalid token syntax from the current DFA/state

These are normal operational outcomes.
They are reported with spans and can be handled by callers as part of regular control flow.

Panics are used for developer or engine failures.
Examples include:

- illegal cross-owner stack mutation attempts
- parser stack mutation APIs (`LexingSessionPushStates`, `LexingSessionPop`, `LexingSessionSet`) when the lexer was configured with `LexerConfigurationDisableClientStackMutations` before `LexerCreate`
- impossible internal state transitions
- misconfigured or contradictory setup that breaks invariants

These are not recoverable lexing outcomes from user content.
They represent programming errors in the grammar authoring layer, parser integration layer, or lexer engine itself.
Treating them as plain runtime errors would blur ownership boundaries and can hide serious bugs behind normal error handling paths.

The panic policy enforces fail-fast behavior for invariant violations:

- Bugs surface immediately during development and testing.
- Contract violations are not silently downgraded into ordinary parse failures.
- Runtime handling remains clean for genuine user-input problems.

There is also a performance reason to keep contract violations on the panic path.
Core parser-lexer coordination operations (for example stack mutations like pop/set) run on hot paths.
If these APIs returned errors for invariant violations, callers would need to branch and handle return values on every invocation.
That introduces repeated control-flow and boilerplate for conditions that should be impossible under a valid grammar and correct integration.

In other words:

- returning errors forces defensive checks in the hottest syntax resolution loops
- the failure branch has no meaningful local recovery strategy because session invariants are already broken
- the additional checks pay ongoing cost for a non-operational class of failure

Panics avoid this by preserving a fast-path API for valid execution while still surfacing contract violations immediately and loudly.

This distinction keeps the API semantics explicit:

- Runtime error means "input cannot be lexed under valid engine usage."
- Panic means "engine or caller violated a required contract."

## Optional Alternatives Considered

Return runtime errors for all failures.

This was rejected because it collapses operational failures and programmer faults into one channel, making it harder to detect real engine misuse and easier to accidentally ignore invariant violations.
