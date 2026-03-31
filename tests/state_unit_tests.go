package tests

import (
	"autarch/pattern"
	"fmt"
	"foundation/domain"
	"lexarch"
	"shield"
)

const (
	TokInit = iota + 100 // Offset to avoid clashing with basic tokens
	TokA
	TokB
	TokPushA
	TokPushB
	TokSetB
	TokPop
	TokPop2
)

func setupStatefulTestLexer() *lexarch.Lexer {
	cfg := lexarch.LexerConfigurationCreate()

	obsDomain := domain.DiscreteDomainRuneCreate()
	factory := pattern.RegulaASTFactoryCreate(obsDomain)
	templates := pattern.RegulaTemplatesCreate(factory)

	// Shared whitespace rule so we can write readable test strings
	wsRule := lexarch.LexingRuleCreate(templates.Whitespace().Plus(), 1, TokWhitespace, 0)

	// -----------------------------------------
	// INITIAL STATE
	// -----------------------------------------
	ruleInitToken := lexarch.LexingRuleCreate(pattern.LiteralString(factory, "init_token"), 2, TokInit, 0)

	rulePushA := lexarch.LexingRuleCreate(pattern.LiteralString(factory, "startA"), 2, TokPushA, 0)
	lexarch.LexingRuleSetStackPush(rulePushA, "STATE_A")

	stateInitial := lexarch.LexingStateCreate("INITIAL", []*lexarch.LexingRule{wsRule, ruleInitToken, rulePushA})

	// -----------------------------------------
	// STATE A
	// -----------------------------------------
	ruleAToken := lexarch.LexingRuleCreate(pattern.LiteralString(factory, "A_token"), 2, TokA, 0)

	ruleSetB := lexarch.LexingRuleCreate(pattern.LiteralString(factory, "gotoB"), 2, TokSetB, 0)
	lexarch.LexingRuleSetStackSet(ruleSetB, "STATE_B")

	rulePushB := lexarch.LexingRuleCreate(pattern.LiteralString(factory, "startB"), 2, TokPushB, 0)
	lexarch.LexingRuleSetStackPush(rulePushB, "STATE_B")

	rulePopA := lexarch.LexingRuleCreate(pattern.LiteralString(factory, "pop"), 2, TokPop, 0)
	lexarch.LexingRuleSetStackPop(rulePopA, 1)

	stateA := lexarch.LexingStateCreate("STATE_A", []*lexarch.LexingRule{wsRule, ruleAToken, ruleSetB, rulePushB, rulePopA})

	// -----------------------------------------
	// STATE B
	// -----------------------------------------
	ruleBToken := lexarch.LexingRuleCreate(pattern.LiteralString(factory, "B_token"), 2, TokB, 0)

	rulePopB := lexarch.LexingRuleCreate(pattern.LiteralString(factory, "pop"), 2, TokPop, 0)
	lexarch.LexingRuleSetStackPop(rulePopB, 1)

	rulePop2 := lexarch.LexingRuleCreate(pattern.LiteralString(factory, "pop2"), 2, TokPop2, 0)
	lexarch.LexingRuleSetStackPop(rulePop2, 2) // Unwinds back to INITIAL

	stateB := lexarch.LexingStateCreate("STATE_B", []*lexarch.LexingRule{wsRule, ruleBToken, rulePopB, rulePop2})

	// -----------------------------------------
	// REGISTRATION
	// -----------------------------------------
	lexarch.LexerConfigurationRegisterState(cfg, stateInitial, true)
	lexarch.LexerConfigurationRegisterState(cfg, stateA, false)
	lexarch.LexerConfigurationRegisterState(cfg, stateB, false)

	return lexarch.LexerCreate(cfg)
}

func GetStatefulLexerUnits(order int) []shield.Unit {
	statefulUnit := shield.UnitCreate(order, "Lexer State Operations")

	var statefulLexer *lexarch.Lexer
	shield.UnitSetSetupAndTeardown(statefulUnit,
		func() { statefulLexer = setupStatefulTestLexer() },
		func() {
			if statefulLexer != nil {
				lexarch.LexerDestroy(statefulLexer)
				statefulLexer = nil
			}
		},
	)

	stateRunner := func(input string) LexerLexResult {
		session := lexarch.LexerLexingSessionCreate(statefulLexer, input)
		out := lexarch.LexingSessionNextResultCreate()
		var result LexerLexResult

		for {
			lexarch.LexingSessionConsume(session, out)
			if out.LexingError != nil {
				result.LexingError = out.LexingError
				break
			}
			if out.EOF {
				result.EOF = true
				break
			}
			if out.Token != nil {
				result.Tokens = append(result.Tokens, *out.Token)
			}
		}
		return result
	}

	stateAtom := shield.AtomCreate(0, "State Transitions", stateRunner)

	stateTests := []struct {
		name  string
		specs []TokenSpec
	}{
		{
			name: "basic_push_and_pop",
			// INITIAL -> Push A -> INITIAL
			specs: []TokenSpec{
				{kind: TokInit, text: "init_token"},
				{kind: TokWhitespace, text: " "},
				{kind: TokPushA, text: "startA"},
				{kind: TokWhitespace, text: " "},
				{kind: TokA, text: "A_token"}, // Only valid in STATE_A
				{kind: TokWhitespace, text: " "},
				{kind: TokPop, text: "pop"},
				{kind: TokWhitespace, text: " "},
				{kind: TokInit, text: "init_token"}, // Only valid in INITIAL
			},
		},
		{
			name: "lateral_set_transition",
			// INITIAL -> Push A -> Set B -> INITIAL
			specs: []TokenSpec{
				{kind: TokPushA, text: "startA"},
				{kind: TokWhitespace, text: " "},
				{kind: TokA, text: "A_token"},
				{kind: TokWhitespace, text: " "},
				{kind: TokSetB, text: "gotoB"}, // Replaces A with B
				{kind: TokWhitespace, text: " "},
				{kind: TokB, text: "B_token"}, // Only valid in STATE_B
				{kind: TokWhitespace, text: " "},
				{kind: TokPop, text: "pop"}, // Pops B, returns to INITIAL
				{kind: TokWhitespace, text: " "},
				{kind: TokInit, text: "init_token"},
			},
		},
		{
			name: "deep_stack_multi_pop",
			// INITIAL -> Push A -> Push B -> Pop 2 -> INITIAL
			specs: []TokenSpec{
				{kind: TokPushA, text: "startA"},
				{kind: TokWhitespace, text: " "},
				{kind: TokPushB, text: "startB"}, // Stack is now [INITIAL, A, B]
				{kind: TokWhitespace, text: " "},
				{kind: TokB, text: "B_token"},
				{kind: TokWhitespace, text: " "},
				{kind: TokPop2, text: "pop2"}, // Unwinds 2 levels directly to INITIAL
				{kind: TokWhitespace, text: " "},
				{kind: TokInit, text: "init_token"},
			},
		},
	}

	for _, tc := range stateTests {
		tc := tc

		input := ""
		for _, s := range tc.specs {
			input += s.text
		}

		testCase := shield.CaseCreate(tc.name, input, func(output LexerLexResult) shield.AtomResult {
			if output.LexingError != nil {
				return *shield.AtomResultFailureCreate(fmt.Sprintf("unexpected lexing error: %v", output.LexingError))
			}
			return *assertTokenSpecs(output.Tokens, tc.specs)
		})

		shield.CaseSetDescription(testCase, fmt.Sprintf("validates stack operations for %q", tc.name))
		shield.AtomRegisterCase(stateAtom, testCase)
	}

	shield.UnitRegisterAtom(statefulUnit, stateAtom)

	return []shield.Unit{*statefulUnit}
}
