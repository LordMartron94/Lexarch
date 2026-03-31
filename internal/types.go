package internal

import (
	"autarch/pattern"
	"fmt"
	"foundation/extensions"
)

// ----------------------------------------------------------------- TOKEN

type TokenKind uint32
type TokenRole uint32

type Token struct {
	// We keep this explicitly small to fit in cache (~64 bytes)

	// Metadata = 12 bytes total
	Kind   TokenKind
	Role   TokenRole
	FileID uint16 // Support for ~65k files in a shared context.

	// 8 Bytes: location
	Span ByteSpan

	// We explicitly do NOT store line + column counters, see: docs/adr/0001-token-location.md
}

// ----------------------------------------------------------------- ERROR

type LexerRuntimeError struct {
	msg  string
	area ByteSpan
}

func (e *LexerRuntimeError) Error() string {
	return fmt.Sprintf("lexer runtime error: %s at %d", e.msg, e.area.Offset)
}

func (e *LexerRuntimeError) Span() ByteSpan {
	return e.area
}

type LexerValidationError struct {
	msg string
}

func (e *LexerValidationError) Error() string {
	return fmt.Sprintf("lexer validation error: %s", e.msg)
}

// ----------------------------------------------------------------- RULE

type LexingState struct {
	descriptor string
	rules      []*LexingRule
}

func LexingStateCreate(descriptor string, rules []*LexingRule) LexingState {
	return LexingState{
		descriptor: descriptor,
		rules:      rules,
	}
}

type StackOperationKind uint8

const (
	STACK_NONE StackOperationKind = iota + 1
	STACK_PUSH
	STACK_POP
	STACK_SET
)

type StackOperationPayload struct {
	targets []string
	pop     *int
}

func (s StackOperationPayload) equal(other StackOperationPayload) bool {
	if !extensions.Equal(s.targets, other.targets) {
		return false
	}

	if s.pop != nil && other.pop != nil {
		if *s.pop != *other.pop {
			return false
		}
	} else if s.pop != other.pop {
		return false
	}

	return true
}

type LexingRule struct {
	pattern      pattern.RegulaAST[rune]
	priority     int
	kind         TokenKind
	role         TokenRole
	stackOpKind  StackOperationKind
	stackPayload *StackOperationPayload
}

func LexingRuleCreate(pattern pattern.RegulaAST[rune], priority int, kind TokenKind, role TokenRole) *LexingRule {
	return &LexingRule{
		pattern:      pattern,
		priority:     priority,
		kind:         kind,
		role:         role,
		stackOpKind:  STACK_NONE,
		stackPayload: nil,
	}
}

func LexingRuleSetStackPush(rule *LexingRule, targets ...string) {
	if rule.stackPayload != nil {
		panic("rule already has stack op attached")
	}

	rule.stackOpKind = STACK_PUSH
	rule.stackPayload = &StackOperationPayload{
		targets: targets,
		pop:     nil,
	}
}

func LexingRuleSetStackPop(rule *LexingRule, amount int) {
	if rule.stackPayload != nil {
		panic("rule already has stack op attached")
	}

	rule.stackOpKind = STACK_POP
	rule.stackPayload = &StackOperationPayload{
		targets: nil,
		pop:     &amount,
	}
}

func LexingRuleSetStackSet(rule *LexingRule, targets ...string) {
	if rule.stackPayload != nil {
		panic("rule already has stack op attached")
	}

	rule.stackOpKind = STACK_SET
	rule.stackPayload = &StackOperationPayload{
		targets: targets,
		pop:     nil,
	}
}

// ----------------------------------------------------------------- TOKEN OUTCOME

type TokenOutcome struct {
	Kind     TokenKind
	Role     TokenRole
	Priority int

	StackOperationID *int
}

// ----------------------------------------------------------------- GENERIC

type PatternCompilerMode uint8

const (
	PATTERN_COMPILE_GLUSHKOV PatternCompilerMode = iota + 1
	PATTERN_COMPILE_THOMPSON
)

type ByteSpan struct {
	Offset uint32 // ~4.2billion bytes = ~4.2GiB -- should be enough for virtually any file
	Length uint32
}
