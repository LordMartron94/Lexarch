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

type LexerLexResult = internal.LexerLexResult
type LexerError = internal.LexerError

type LexingState = internal.LexingState
type LexingRule = internal.LexingRule

type LexerConfiguration = internal.LexerConfiguration

type PatternCompilerMode = internal.PatternCompilerMode

type ByteSpan = internal.ByteSpan

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

func LexingStateCreate(descriptor string, rules []LexingRule) LexingState {
	return internal.LexingStateCreate(descriptor, rules)
}

func LexingRuleCreate(pattern pattern.RegulaAST[rune], priority int, kind TokenKind, role TokenRole) LexingRule {
	return internal.LexingRuleCreate(pattern, priority, kind, role)
}

func LexerCreate(configuration *LexerConfiguration) *Lexer {
	return internal.LexerCreate(configuration)
}

func LexerDestroy(lexer *Lexer) {
	internal.LexerDestroy(lexer)
}

func LexerLexContentFull(lexer *Lexer, content string) LexerLexResult {
	return internal.LexerLexContentFull(lexer, content)
}
