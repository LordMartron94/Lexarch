/*
Package lexarch provides a highly efficient lexing framework.
*/
package lexarch

import (
	"autarch/pattern"
	"lexarch/internal"
	"memcore"
)

type TokenKind = internal.TokenKind
type TokenRole = internal.TokenRole

type Lexer = internal.Lexer
type Token = internal.Token

type LexerRuntimeError = internal.LexerRuntimeError
type LexerValidationError = internal.LexerValidationError

type LexingNextResult = internal.NextResult

type LexingState = internal.LexingState
type LexingRule = internal.LexingRule

type LexingSession = internal.LexingSession

type LexerConfiguration = internal.LexerConfiguration

type PatternCompilerMode = internal.PatternCompilerMode

type ByteSpan = internal.ByteSpan

func LexerByteSpanToPosition(span ByteSpan, source string, tabWidth int) LexingPosition {
	return internal.SpanToPosition(span, source, tabWidth)
}

type LexingSnapshot = internal.LexingSessionSnapshot

type LexingPosition = internal.LexPosition

func LexerConfigurationCreate() *LexerConfiguration {
	return internal.LexerConfigurationCreate()
}

func LexerConfigurationSetPatternCompiler(cfg *LexerConfiguration, patternCompiler PatternCompilerMode) {
	internal.LexerConfigurationCompilerSetPatternCompiler(cfg, patternCompiler)
}

func LexerConfigurationRegisterState(cfg *LexerConfiguration, state LexingState, isStart bool) {
	internal.LexerConfigurationRegisterState(cfg, state, isStart)
}

func LexerConfigurationSetScratchMemory(cfg *LexerConfiguration, min, max memcore.MemoryUnitBytes) {
	internal.LexerConfigurationSetScratchMemory(cfg, min, max)
}

func LexerConfigurationSetMainMemory(cfg *LexerConfiguration, min, max memcore.MemoryUnitBytes) {
	internal.LexerConfigurationSetMainMemory(cfg, min, max)
}

func LexerConfigurationSetTokenKindFormatter(cfg *LexerConfiguration, formatter func(kind TokenKind) string) {
	internal.LexerConfigurationSetTokenKindFormatter(cfg, formatter)
}

func LexingStateCreate(descriptor string, rules []*LexingRule) LexingState {
	return internal.LexingStateCreate(descriptor, rules)
}

func LexingRuleCreate(pattern pattern.RegulaAST[rune], priority int, kind TokenKind, role TokenRole) *LexingRule {
	return internal.LexingRuleCreate(pattern, priority, kind, role)
}

func LexingRuleSetStackPush(rule *LexingRule, targets ...string) {
	internal.LexingRuleSetStackPush(rule, targets...)
}

func LexingRuleSetStackPop(rule *LexingRule, amount int) {
	internal.LexingRuleSetStackPop(rule, amount)
}

func LexingRuleSetStackSet(rule *LexingRule, targets ...string) {
	internal.LexingRuleSetStackSet(rule, targets...)
}

func LexerCreate(configuration *LexerConfiguration) *Lexer {
	return internal.LexerCreate(configuration)
}

func LexerDestroy(lexer *Lexer) {
	internal.LexerDestroy(lexer)
}

func LexerLexingSessionCreate(lexer *Lexer, content string) *LexingSession {
	return internal.LexerLexingSessionCreate(lexer, content)
}

func LexingSessionNextResultCreate() *LexingNextResult {
	return internal.LexingSessionNextResultCreate()
}

func LexingSessionPrefillCache(session *LexingSession) error {
	return internal.LexingSessionPrefillCache(session)
}

func LexingSessionConsume(session *LexingSession, out *LexingNextResult) {
	internal.LexingSessionConsume(session, out)
}

func LexingSessionConsumeUnsafe(session *LexingSession, out *LexingNextResult) {
	internal.LexingSessionConsumeUnsafe(session, out)
}

func LexingSessionPeek(session *LexingSession, out *LexingNextResult, n int) {
	internal.LexingSessionPeek(session, out, n)
}

func LexingSessionPeekUnsafe(session *LexingSession, out *LexingNextResult, n int) {
	internal.LexingSessionPeekUnsafe(session, out, n)
}

func LexingSessionPushStates(session *LexingSession, states ...string) {
	internal.LexingSessionPushStates(session, false, states...)
}

func LexingSessionPop(session *LexingSession, amount int) {
	internal.LexingSessionPop(session, false, amount)
}

func LexingSessionSet(session *LexingSession, targets ...string) {
	internal.LexingSessionSet(session, false, targets...)
}

func LexingSessionSnapshotCreate(session *LexingSession) LexingSnapshot {
	return internal.LexingSessionSnapshotCreate(session)
}

func LexingSessionSnapshotRestore(session *LexingSession, snapshot LexingSnapshot) {
	internal.LexingSessionSnapshotRestore(session, snapshot)
}
