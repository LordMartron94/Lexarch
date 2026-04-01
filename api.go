/*
Package lexarch provides a deterministic, state-aware lexer with explicit session control.
*/
package lexarch

import (
	"autarch/pattern"
	"lexarch/internal"
	"memcore"
)

/* TokenKind classifies the concrete token variant produced by a rule. */
type TokenKind = internal.TokenKind

/* TokenRole provides an orthogonal token classification channel (for example category/grouping). */
type TokenRole = internal.TokenRole

/* Lexer is a compiled lexer instance containing per-state minimized DFAs and stack operations. */
type Lexer = internal.Lexer

/* Token is the runtime token payload (kind, role, file id, and byte span). */
type Token = internal.Token

/* LexerRuntimeError represents a user-input lexing failure at a concrete byte span. */
type LexerRuntimeError = internal.LexerRuntimeError

/* LexerValidationError represents misuse of the lexer API (for example consuming after destroy). */
type LexerValidationError = internal.LexerValidationError

/* LexingNextResult is the mutable output carrier reused by consume/peek calls. */
type LexingNextResult = internal.NextResult

/* LexingState is a named state descriptor with a set of lexing rules. */
type LexingState = internal.LexingState

/* LexingRule maps a pattern to a token outcome and optional stack operation. */
type LexingRule = internal.LexingRule

/* LexingSession is a mutable cursor over an input string for a specific lexer instance. */
type LexingSession = internal.LexingSession

/* LexerConfiguration describes the complete lexer construction input. */
type LexerConfiguration = internal.LexerConfiguration

/* PatternCompilerMode selects the NFA construction backend used during lexer compilation. */
type PatternCompilerMode = internal.PatternCompilerMode

/* TokenKindError is the reserved token kind emitted for invalid lexemes. */
const TokenKindError TokenKind = internal.ErrorToken

/* TokenKindEOF is the reserved token kind used by consumers as an EOF sentinel kind. */
const TokenKindEOF TokenKind = internal.EOFToken

/* TokenRoleSentinel is a reserved role used for non-user token outcomes and error tokens. */
const TokenRoleSentinel TokenRole = internal.SentinelTokenRole

/*
PATTERN_COMPILE_GLUSHKOV selects the Glushkov construction path.

This mode is the default in LexerConfigurationCreate.
*/
const PATTERN_COMPILE_GLUSHKOV = internal.PATTERN_COMPILE_GLUSHKOV

/* PATTERN_COMPILE_THOMPSON selects the Thompson construction path. */
const PATTERN_COMPILE_THOMPSON = internal.PATTERN_COMPILE_THOMPSON

/* ByteSpan identifies a byte-range in source content using offset and length. */
type ByteSpan = internal.ByteSpan

/* LexingSnapshot stores session state required for snapshot/restore operations. */
type LexingSnapshot = internal.LexingSessionSnapshot

/* LexingPosition stores 1-indexed start/end line and column coordinates for a span. */
type LexingPosition = internal.LexPosition

/*
LexerByteSpanToPosition converts a byte span into 1-indexed line/column positions.

The conversion walks the provided source from byte zero until span end.
tabWidth controls how tab characters advance the column counter.

When span.Length is zero, start and end coordinates point at the same cursor location.
*/
func LexerByteSpanToPosition(span ByteSpan, source string, tabWidth int) LexingPosition {
	return internal.SpanToPosition(span, source, tabWidth)
}

/* LexerConfigurationCreate creates a lexer configuration with sane defaults. */
func LexerConfigurationCreate() *LexerConfiguration {
	return internal.LexerConfigurationCreate()
}

/* LexerConfigurationSetPatternCompiler sets the compilation backend for rule patterns. */
func LexerConfigurationSetPatternCompiler(cfg *LexerConfiguration, patternCompiler PatternCompilerMode) {
	internal.LexerConfigurationCompilerSetPatternCompiler(cfg, patternCompiler)
}

/*
LexerConfigurationRegisterState registers a named state and optionally marks it as the start state.

At least one state must be registered as the start state before calling LexerCreate.
Panics if the descriptor is duplicated or if more than one start state is registered.
*/
func LexerConfigurationRegisterState(cfg *LexerConfiguration, state LexingState, isStart bool) {
	internal.LexerConfigurationRegisterState(cfg, state, isStart)
}

/*
LexerConfigurationSetScratchMemory sets temporary allocator bounds used while compiling automata.

These bounds affect NFA construction, subset conversion, and minimization workspace.
The constraints are validated by the internal configuration layer.
*/
func LexerConfigurationSetScratchMemory(cfg *LexerConfiguration, min, max memcore.MemoryUnitBytes) {
	internal.LexerConfigurationSetScratchMemory(cfg, min, max)
}

/*
LexerConfigurationSetMainMemory sets allocator bounds used for long-lived lexer structures.

This controls memory for compiled/minimized DFA artifacts stored in the final lexer.
*/
func LexerConfigurationSetMainMemory(cfg *LexerConfiguration, min, max memcore.MemoryUnitBytes) {
	internal.LexerConfigurationSetMainMemory(cfg, min, max)
}

/*
LexerConfigurationSetTokenKindFormatter configures how token kinds are rendered in diagnostics.

This is used in validation panics such as duplicate/equivalent rule detection.
*/
func LexerConfigurationSetTokenKindFormatter(cfg *LexerConfiguration, formatter func(kind TokenKind) string) {
	internal.LexerConfigurationSetTokenKindFormatter(cfg, formatter)
}

/*
LexingStateCreate creates a state descriptor with a fixed set of rules.

descriptor is the externally referenced state name used in stack transitions.
*/
func LexingStateCreate(descriptor string, rules []*LexingRule) LexingState {
	return internal.LexingStateCreate(descriptor, rules)
}

/*
LexingRuleCreate creates a tokenization rule from a pattern, priority, kind, and role.

Resolution strategy is: longest match first, then highest priority for equal match length.
*/
func LexingRuleCreate(pattern pattern.RegulaAST[rune], priority int, kind TokenKind, role TokenRole) *LexingRule {
	return internal.LexingRuleCreate(pattern, priority, kind, role)
}

/*
LexingRuleSetStackPush configures a rule to push one or more states when matched.

Panics if another stack operation was already attached to this rule.
*/
func LexingRuleSetStackPush(rule *LexingRule, targets ...string) {
	internal.LexingRuleSetStackPush(rule, targets...)
}

/*
LexingRuleSetStackPop configures a rule to pop the specified number of stack frames when matched.

Panics if another stack operation was already attached to this rule.
*/
func LexingRuleSetStackPop(rule *LexingRule, amount int) {
	internal.LexingRuleSetStackPop(rule, amount)
}

/*
LexingRuleSetStackSet configures a rule to replace the top frame with the provided targets.

Panics if another stack operation was already attached to this rule.
*/
func LexingRuleSetStackSet(rule *LexingRule, targets ...string) {
	internal.LexingRuleSetStackSet(rule, targets...)
}

/*
LexerCreate compiles all configured states into a ready-to-use lexer.

Panics on invalid configuration, unresolved state references, duplicate-equivalent rule
patterns inside a state, or unresolved internal compilation invariants.

Runtime lexing throughput depends on the configured rule set and the input being processed
(for example rule complexity, active-state transitions, and token distribution).
*/
func LexerCreate(configuration *LexerConfiguration) *Lexer {
	return internal.LexerCreate(configuration)
}

/*
LexerDestroy releases memory owned by a lexer and marks it as unusable.

After destruction, guarded APIs return LexerValidationError where applicable.
*/
func LexerDestroy(lexer *Lexer) {
	internal.LexerDestroy(lexer)
}

/*
LexerLexingSessionCreate creates a session for lexing content with a specific file id.

fileID is copied into emitted tokens, enabling multi-file diagnostics in higher layers.
Sessions are independent and can be used concurrently when not shared across goroutines.
*/
func LexerLexingSessionCreate(lexer *Lexer, content string, fileID uint16) *LexingSession {
	return internal.LexerLexingSessionCreate(lexer, content, fileID)
}

/*
LexerLexingSessionReset reuses an existing session with new content and file id.

Reset clears cursor/cache/state-stack back to start-state baseline while preserving the
session allocation itself, enabling allocation-free session reuse across runs.
*/
func LexerLexingSessionReset(lexer *Lexer, session *LexingSession, content string, fileID uint16) {
	internal.LexerLexingSessionReset(lexer, session, content, fileID)
}

/*
LexingSessionNextResultCreate allocates an empty reusable result container.

Callers should reuse this object in hot loops to avoid repeated allocations.
*/
func LexingSessionNextResultCreate() *LexingNextResult {
	return internal.LexingSessionNextResultCreate()
}

/*
LexingSessionPrefillCache lexes the remaining input ahead of time and stores token cache entries.

This operation is currently only meaningful when the lexer has a single state.
Returns the first encountered runtime error while pre-filling.
*/
func LexingSessionPrefillCache(session *LexingSession) error {
	return internal.LexingSessionPrefillCache(session)
}

/*
LexingSessionConsume consumes the next token or EOF into out and advances the session cursor.

If the lexer was destroyed, out.LexingError is set to LexerValidationError.
Throughput for repeated consume calls depends on both lexer rules and input characteristics.
*/
func LexingSessionConsume(session *LexingSession, out *LexingNextResult) {
	internal.LexingSessionConsume(session, out)
}

/*
LexingSessionConsumeUnsafe is consume without destroyed-lexer validation checks.

Use only where external code already enforces lifecycle correctness.
*/
func LexingSessionConsumeUnsafe(session *LexingSession, out *LexingNextResult) {
	internal.LexingSessionConsumeUnsafe(session, out)
}

/*
LexingSessionCurrent reads the token at the current session cursor without advancing.

Semantically this is equivalent to one-token lookahead at the cursor position.
If the lexer was destroyed, out.LexingError is set to LexerValidationError.
*/
func LexingSessionCurrent(session *LexingSession, out *LexingNextResult) {
	internal.LexingSessionCurrent(session, out)
}

/*
LexingSessionCurrentUnsafe is current-token read without destroyed-lexer validation checks.

Use only where external code already enforces lifecycle correctness.
*/
func LexingSessionCurrentUnsafe(session *LexingSession, out *LexingNextResult) {
	internal.LexingSessionCurrentUnsafe(session, out)
}

/*
LexingSessionPeek evaluates the n-th next token while restoring session state afterwards.

Peek is implemented via snapshot/restore, so it does not mutate final session position.
Lookahead is 1-based: n=1 returns the immediate next token, n=2 the one after that.
Contract: n must be >= 1. Passing n <= 0 is invalid and leaves out unchanged.
*/
func LexingSessionPeek(session *LexingSession, out *LexingNextResult, n int) {
	internal.LexingSessionPeek(session, out, n)
}

/*
LexingSessionPeekUnsafe is peek without destroyed-lexer validation checks.

Semantics match LexingSessionPeek exactly, including 1-based lookahead and n>=1 contract.
*/
func LexingSessionPeekUnsafe(session *LexingSession, out *LexingNextResult, n int) {
	internal.LexingSessionPeekUnsafe(session, out, n)
}

/*
LexingSessionPushStates pushes parser-owned states onto the stack.

Parser-owned frames may later be mutated by parser APIs.
Targets must reference registered state descriptors.
*/
func LexingSessionPushStates(session *LexingSession, states ...string) {
	internal.LexingSessionPushStates(session, false, states...)
}

/*
LexingSessionPop pops parser-owned stack frames.

Panics if a parser call attempts to pop across lexer-owned frames.
Pop requests that exceed available mutable frames clamp at the stack bottom marker.
*/
func LexingSessionPop(session *LexingSession, amount int) {
	internal.LexingSessionPop(session, false, amount)
}

/*
LexingSessionSet replaces the parser-visible top frame with parser-owned targets.

This is implemented as pop(1) + push(targets...).
Panics if replacing would cross lexer-owned ownership boundaries.
*/
func LexingSessionSet(session *LexingSession, targets ...string) {
	internal.LexingSessionSet(session, false, targets...)
}

/*
LexingSessionSnapshotCreate captures current offset and stack state for later restoration.

Snapshots are value objects and may be stored by callers for speculative workflows.
*/
func LexingSessionSnapshotCreate(session *LexingSession) LexingSnapshot {
	return internal.LexingSessionSnapshotCreate(session)
}

/*
LexingSessionSnapshotRestore restores offset/stack state from a previously captured snapshot.

Restore is ownership-agnostic by design and may rewind through lexer-owned mutations.
*/
func LexingSessionSnapshotRestore(session *LexingSession, snapshot LexingSnapshot) {
	internal.LexingSessionSnapshotRestore(session, snapshot)
}
