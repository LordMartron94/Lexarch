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

	// ---------------------------------------------------------
	// SNAPSHOT & RESTORE OPERATIONS
	// ---------------------------------------------------------

	type SnapshotTestCase struct {
		Input       string
		Execute     func(session *lexarch.LexingSession) LexerLexResult
		ExpectError bool
	}

	snapshotRunner := func(tc SnapshotTestCase) LexerLexResult {
		session := lexarch.LexerLexingSessionCreate(statefulLexer, tc.Input)
		return tc.Execute(session)
	}

	snapshotAtom := shield.AtomCreate(1, "Snapshot and Restore", snapshotRunner)

	// Helper to consume cleanly in the imperative tests
	consumeNext := func(session *lexarch.LexingSession, res *LexerLexResult) {
		out := lexarch.LexingSessionNextResultCreate()
		lexarch.LexingSessionConsume(session, out)
		if out.LexingError != nil {
			res.LexingError = out.LexingError
		} else if out.Token != nil {
			res.Tokens = append(res.Tokens, *out.Token)
		} else if out.EOF {
			res.EOF = true
		}
	}

	snapshotTests := []struct {
		name     string
		caseDef  SnapshotTestCase
		expected []TokenSpec
	}{
		{
			name: "restore_rewinds_offset_and_state",
			// Proves that if the lexer transitions from STATE_A to STATE_B,
			// a restore violently rips the lexer back to STATE_A and the old offset.
			caseDef: SnapshotTestCase{
				Input: "startA A_token gotoB B_token",
				Execute: func(session *lexarch.LexingSession) LexerLexResult {
					res := LexerLexResult{}

					// 1. Enter STATE_A
					consumeNext(session, &res) // startA
					consumeNext(session, &res) // <space>

					// 2. CREATE SNAPSHOT (We are at offset 7, Stack: [INITIAL, STATE_A])
					snap := lexarch.LexingSessionSnapshotCreate(session)

					// 3. Move forward and transition to STATE_B
					consumeNext(session, &res) // A_token
					consumeNext(session, &res) // <space>
					consumeNext(session, &res) // gotoB (Lexer pushes STATE_B)
					consumeNext(session, &res) // <space>
					consumeNext(session, &res) // B_token

					// 4. RESTORE SNAPSHOT
					lexarch.LexingSessionSnapshotRestore(session, snap)

					// 5. Consume again. If the snapshot worked, we should re-read A_token
					// and it should NOT throw a LexingError (meaning we are back in STATE_A).
					consumeNext(session, &res) // A_token (second time)

					return res
				},
			},
			expected: []TokenSpec{
				{kind: TokPushA, text: "startA"},

				{id: "snapshot_1", kind: TokWhitespace, text: " "},

				{kind: TokA, text: "A_token"},
				{kind: TokWhitespace, text: " "},
				{kind: TokSetB, text: "gotoB"},
				{kind: TokWhitespace, text: " "},
				{kind: TokB, text: "B_token"},

				{restoreToID: "snapshot_1", kind: TokA, text: "A_token"},
			},
		},
		{
			name: "state_aware_cache_isolation",
			// Proves that reading a token in STATE_A does not poison the cache
			// if we backtrack and attempt to read the same offset in STATE_B.
			caseDef: SnapshotTestCase{
				Input:       "startA A_token",
				ExpectError: true, // "A_token" is invalid syntax in STATE_B. We EXPECT a crash.
				Execute: func(session *lexarch.LexingSession) LexerLexResult {
					res := LexerLexResult{}

					consumeNext(session, &res) // startA
					consumeNext(session, &res) // <space>

					// Offset is now 7. Stack is [INITIAL, STATE_A].
					snap := lexarch.LexingSessionSnapshotCreate(session)

					// Consume in STATE_A to populate the cache at offset 7
					consumeNext(session, &res) // reads A_token

					// Rewind time
					lexarch.LexingSessionSnapshotRestore(session, snap)

					// Force a state change from the parser side
					lexarch.LexingSessionSet(session, "STATE_B")

					// Attempt to consume again.
					// Buggy cache: Returns TokA.
					// Fixed cache: Runs STATE_B DFA, fails on 'A', returns error.
					consumeNext(session, &res)

					return res
				},
			},
			expected: nil, // We don't assert token sequences when expecting an error
		},
	}

	for _, tc := range snapshotTests {
		tc := tc

		testCase := shield.CaseCreate(tc.name, tc.caseDef, func(output LexerLexResult) shield.AtomResult {
			if tc.caseDef.ExpectError {
				if output.LexingError == nil {
					return *shield.AtomResultFailureCreate("expected a lexing error due to state change, but got none (is your cache state-blind?)")
				}
				return *shield.AtomResultSuccessCreate()
			}

			if output.LexingError != nil {
				return *shield.AtomResultFailureCreate(fmt.Sprintf("unexpected lexing error during snapshot test: %v", output.LexingError))
			}
			return *assertTokenSpecs(output.Tokens, tc.expected)
		})

		shield.CaseSetDescription(testCase, "validates deep mutability reversal of LexingSession")
		shield.AtomRegisterCase(snapshotAtom, testCase)
	}

	shield.UnitRegisterAtom(statefulUnit, snapshotAtom)

	return []shield.Unit{*statefulUnit}
}
