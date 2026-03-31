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
		return runLexerSessionToEnd(statefulLexer, input, false)
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

		input := tokenSpecsToInput(tc.specs)

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
		ExpectPanic bool
	}

	snapshotRunner := func(tc SnapshotTestCase) LexerLexResult {
		session := lexarch.LexerLexingSessionCreate(statefulLexer, tc.Input, 0)
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
			if out.Token.Kind == lexarch.TokenKindEOF {
				return
			}
			res.Tokens = append(res.Tokens, *out.Token)
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
			caseDef: SnapshotTestCase{
				Input:       "A_token", // Just the raw token
				ExpectError: true,
				Execute: func(session *lexarch.LexingSession) LexerLexResult {
					res := LexerLexResult{}

					// 1. Parser pushes STATE_A (Parser-owned, legal to mutate)
					lexarch.LexingSessionPushStates(session, "STATE_A")

					// 2. Snapshot (Stack: [INITIAL, STATE_A])
					snap := lexarch.LexingSessionSnapshotCreate(session)

					// 3. Consume in STATE_A to poison cache at offset 0
					consumeNext(session, &res)

					// 4. Rewind time
					lexarch.LexingSessionSnapshotRestore(session, snap)

					// 5. Parser Sets STATE_B (Legal, because STATE_A was parser-owned)
					lexarch.LexingSessionSet(session, "STATE_B")

					// 6. Attempt to consume. Safe Cache = error. Blind Cache = TokA.
					consumeNext(session, &res)

					return res
				},
			},
			expected: nil,
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

	// ---------------------------------------------------------
	// MIXED STACK MUTATIONS (PARSER + LEXER)
	// ---------------------------------------------------------

	mixedMutationsRunner := func(tc SnapshotTestCase) LexerLexResult {
		session := lexarch.LexerLexingSessionCreate(statefulLexer, tc.Input, 0)
		return tc.Execute(session)
	}

	mixedMutationsAtom := shield.AtomCreate(2, "Mixed Stack Mutations", mixedMutationsRunner)

	mixedTests := []struct {
		name     string
		caseDef  SnapshotTestCase
		expected []TokenSpec
	}{
		{
			name: "deep_interleaved_stack",
			// Parser pushes A -> Lexer pushes B -> Lexer pops B -> Parser pops A
			caseDef: SnapshotTestCase{
				Input: "startB B_token pop A_token",
				Execute: func(session *lexarch.LexingSession) LexerLexResult {
					res := LexerLexResult{}

					// Stack: [INITIAL, STATE_A]
					lexarch.LexingSessionPushStates(session, "STATE_A")

					consumeNext(session, &res) // startB (Lexer pushes STATE_B)
					consumeNext(session, &res) // <space>

					// Stack: [INITIAL, STATE_A, STATE_B]
					consumeNext(session, &res) // B_token
					consumeNext(session, &res) // <space>

					// Lexer pops STATE_B. Stack should be back to [INITIAL, STATE_A]
					consumeNext(session, &res) // pop
					consumeNext(session, &res) // <space>

					// Must resolve using STATE_A's DFA
					consumeNext(session, &res) // A_token

					// Parser pops STATE_A. Stack should be back to [INITIAL]
					lexarch.LexingSessionPop(session, 1)

					// Signal EOF
					consumeNext(session, &res)

					return res
				},
			},
			expected: []TokenSpec{
				{kind: TokPushB, text: "startB"},
				{kind: TokWhitespace, text: " "},
				{kind: TokB, text: "B_token"},
				{kind: TokWhitespace, text: " "},
				{kind: TokPop, text: "pop"},
				{kind: TokWhitespace, text: " "},
				{kind: TokA, text: "A_token"},
			},
		},
	}

	for _, tc := range mixedTests {
		tc := tc

		testCase := shield.CaseCreate(tc.name, tc.caseDef, func(output LexerLexResult) shield.AtomResult {
			if output.LexingError != nil {
				return *shield.AtomResultFailureCreate(fmt.Sprintf("unexpected lexing error during mixed mutations: %v", output.LexingError))
			}
			return *assertTokenSpecs(output.Tokens, tc.expected)
		})

		shield.CaseSetDescription(testCase, "validates interplay between API state mutations and Rule state mutations")
		shield.AtomRegisterCase(mixedMutationsAtom, testCase)
	}

	shield.UnitRegisterAtom(statefulUnit, mixedMutationsAtom)

	// ---------------------------------------------------------
	// LEXICAL LOCK GUARDS (EXCLUSIVE OWNERSHIP)
	// ---------------------------------------------------------

	lockGuardsRunner := func(tc SnapshotTestCase) LexerLexResult {
		session := lexarch.LexerLexingSessionCreate(statefulLexer, tc.Input, 0)
		return tc.Execute(session)
	}

	lockGuardsAtom := shield.AtomCreate(3, "Lexical Lock Guards", lockGuardsRunner)

	lockTests := []struct {
		name     string
		caseDef  SnapshotTestCase
		expected []TokenSpec
	}{
		{
			name: "parser_cannot_pop_lexer_state",
			caseDef: SnapshotTestCase{
				Input:       "startA",
				ExpectError: true,
				ExpectPanic: true, // This scenario must panic.
				Execute: func(session *lexarch.LexingSession) LexerLexResult {
					res := LexerLexResult{}
					consumeNext(session, &res)

					panicked := expectPanic(func() {
						lexarch.LexingSessionPop(session, 1)
					})
					setPanicMarker(&res, panicked, "LexingSessionPop failed to panic")
					return res
				},
			},
			expected: nil, // We don't care about sequence matching here
		},
		{
			name: "parser_cannot_set_over_lexer_state",
			caseDef: SnapshotTestCase{
				Input:       "startA",
				ExpectError: true,
				ExpectPanic: true, // This scenario must panic.
				Execute: func(session *lexarch.LexingSession) LexerLexResult {
					res := LexerLexResult{}
					consumeNext(session, &res)

					panicked := expectPanic(func() {
						lexarch.LexingSessionSet(session, "STATE_B")
					})
					setPanicMarker(&res, panicked, "LexingSessionSet failed to panic")
					return res
				},
			},
			expected: nil,
		},
		{
			name: "lexer_cannot_pop_parser_state",
			caseDef: SnapshotTestCase{
				Input:       "startB pop2",
				ExpectError: true,
				ExpectPanic: true, // This scenario must panic.
				Execute: func(session *lexarch.LexingSession) LexerLexResult {
					res := LexerLexResult{}

					// Parser imposes STATE_A.
					lexarch.LexingSessionPushStates(session, "STATE_A")

					// Lexer pushes STATE_B through rule transition.
					consumeNext(session, &res) // startB
					consumeNext(session, &res) // <space>

					// pop2 in STATE_B asks lexer to pop 2 levels:
					// [INITIAL, parser:A, lexer:B] -> attempts to remove B and parser-owned A.
					// Exclusive ownership means this must panic.
					panicked := expectPanic(func() {
						consumeNext(session, &res) // pop2
					})
					setPanicMarker(&res, panicked, "lexer-driven pop crossed parser-owned frame without panic")

					return res
				},
			},
			expected: nil,
		},
		{
			name: "snapshot_restore_bypasses_lock",
			// Proves that while Pop and Set are blocked, Restore is allowed to
			// wipe lexer-owned states because it is a time-travel rewind operation.
			caseDef: SnapshotTestCase{
				Input: " startA", // Added a leading space to anchor the snapshot
				Execute: func(session *lexarch.LexingSession) LexerLexResult {
					res := LexerLexResult{}

					consumeNext(session, &res) // Consume leading space

					// Take snapshot BEFORE the lexer pushes its state (Stack: [INITIAL])
					snap := lexarch.LexingSessionSnapshotCreate(session)

					consumeNext(session, &res) // startA (Lexer pushes STATE_A)

					// Parser abandons the lookahead and restores.
					// This MUST NOT panic, even though it destroys STATE_A.
					panicked := expectPanic(func() {
						lexarch.LexingSessionSnapshotRestore(session, snap)
					})

					if panicked {
						res.LexingError = fmt.Errorf("LexingSessionSnapshotRestore panicked unexpectedly")
						return res
					}

					// We are safely back at offset 1, so the next read is startA again
					consumeNext(session, &res) // startA

					return res
				},
			},
			expected: []TokenSpec{
				{id: "anchor", kind: TokWhitespace, text: " "},
				{kind: TokPushA, text: "startA"},
				// --- RESTORE ---
				// Rewind the test runner's expected offset back to the anchor
				{restoreToID: "anchor", kind: TokPushA, text: "startA"},
			},
		},
	}

	for _, tc := range lockTests {
		tc := tc

		testCase := shield.CaseCreate(tc.name, tc.caseDef, func(output LexerLexResult) shield.AtomResult {
			if tc.caseDef.ExpectPanic {
				if output.LexingError == nil {
					return *shield.AtomResultFailureCreate("Expected panic, but execution succeeded")
				}
				if output.LexingError.Error() != expectedPanicMarker {
					return *shield.AtomResultFailureCreate(fmt.Sprintf("Expected panic marker, got: %v", output.LexingError))
				}
				return *shield.AtomResultSuccessCreate()
			}

			if tc.caseDef.ExpectError {
				if output.LexingError == nil {
					return *shield.AtomResultFailureCreate("Expected panic/error, but execution succeeded")
				}
				return *shield.AtomResultSuccessCreate()
			}
			if output.LexingError != nil {
				return *shield.AtomResultFailureCreate(output.LexingError.Error())
			}
			return *assertTokenSpecs(output.Tokens, tc.expected)
		})

		shield.CaseSetDescription(testCase, "validates API boundaries protecting lexer-owned stack frames")
		shield.AtomRegisterCase(lockGuardsAtom, testCase)
	}

	shield.UnitRegisterAtom(statefulUnit, lockGuardsAtom)

	return []shield.Unit{*statefulUnit}
}
