package lexarch

import (
	"autarch"
	"autarch/pattern"
	"cmp"
	"fmt"
	"foundation/domain"
	"memarch"
	"memcore"
	"memforge"
)

func LexerCreate[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	inputRulesets map[TState]LexingRuleset[TObservation, TToken, TTokenRole],
	eofToken TToken,
	scratchAllocationFn memarch.AllocationFn,
	maxDFAAllocatorMemory memcore.MemoryUnitBytes,
	nfaToDFAPipelineMinTemp, nfaToDFAPipelineMaxTemp memcore.MemoryUnitBytes,
	observationCtx ObservationCTX[TObservation],
	compilationMode CompilerMode,
	scanConfig LexerScanConfig,
) *Lexer[TObservation, TState, TToken, TTokenRole] {
	dfaAllocator := memforge.DynamicLinearAllocatorCreateFunction(uint64(memcore.KiloByte), func(currentCap, neededCap uint64) uint64 {
		newSize := max(currentCap*2, neededCap)

		if newSize > uint64(maxDFAAllocatorMemory) {
			panic("dfa allocator consumes too much memory")
		}

		return newSize
	})

	lexerRulesets := make(map[TState]*autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]])
	tokenResolutions := make(map[TState]TokenResolutionStepFn[TToken])

	for state, ruleset := range inputRulesets {
		var compiler pattern.RegulaToNFACompiler[TObservation, TokenOutcome[TToken, TTokenRole]]
		switch compilationMode {
		case Thompson:
			compiler = pattern.RegulaCompileToNFAThompson
		case Glushkov:
			compiler = pattern.RegulaCompileToNFAGlushkov
		default:
			panic("unknown compilation mode")
		}

		var nonTerminalOutcome TokenOutcome[TToken, TTokenRole]

		compiled := lexingRulesetCompile(ruleset, scratchAllocationFn, func(sizeBytes, alignment uint64) memcore.MarkRaw {
			return memforge.DynamicLinearAllocatorMallocUnsafe(dfaAllocator, sizeBytes, alignment)
		}, nfaToDFAPipelineMinTemp, nfaToDFAPipelineMaxTemp, observationCtx.observationDomain, observationCtx.toBytes, compiler, nonTerminalOutcome)

		lexerRulesets[state] = compiled

		// Store resolution function (default to longest if not set)
		if ruleset.tokenResolutionStep != nil {
			tokenResolutions[state] = ruleset.tokenResolutionStep
		} else {
			tokenResolutions[state] = TokenResolutionStepLongest[TToken]
		}
	}

	return &Lexer[TObservation, TState, TToken, TTokenRole]{
		ruleSets:           lexerRulesets,
		tokenResolutions:   tokenResolutions,
		nonTerminalOutcome: TokenOutcome[TToken, TTokenRole]{},
		dfaAllocator:       dfaAllocator,
		eofToken:           eofToken,
		formatter:          observationCtx.formatter,
		scanConfig:         scanConfig,
	}
}
func LexerClose[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](lexer *Lexer[TObservation, TState, TToken, TTokenRole]) {
	memforge.DynamicLinearAllocatorDestroy(lexer.dfaAllocator)
}
func lexingRulesetCompile[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	ruleset LexingRuleset[TObservation, TToken, TTokenRole],
	scratchAllocFn memarch.AllocationFn,
	dfaAllocFn memarch.AllocationFn,
	nfaToDFAPipelineMinTemp, nfaToDFAPipelineMaxTemp memcore.MemoryUnitBytes,
	observationDomain *domain.DiscreteDomain[TObservation],
	toBytes func(observations []TObservation) []byte,
	compiler pattern.RegulaToNFACompiler[TObservation, TokenOutcome[TToken, TTokenRole]],
	nonTerminalOutcome TokenOutcome[TToken, TTokenRole],
) *autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]] {
	obsEqual := func(a, b TObservation) bool {
		return observationDomain.OrderingCmp(a, b) == 0
	}
	rules := ruleset.precompiledRules
	for i := 0; i < len(rules); i++ {
		for j := i + 1; j < len(rules); j++ {
			if pattern.PatternEquivalent(&rules[i].pattern, &rules[j].pattern, obsEqual) {
				panic(fmt.Sprintf(
					"lexing ruleset: duplicate pattern: tokens %v and %v have equivalent patterns",
					rules[i].token,
					rules[j].token,
				))
			}
		}
	}

	ctx := pattern.CreateSharedCompilationContext[TObservation, pattern.RegulaAST[TObservation]](
		observationDomain,
		pattern.ObservationFormatter[TObservation]{
			ToBytes: toBytes,
		},
	)

	instructions := make([]pattern.PatternCompilationInstruction[TObservation, TokenOutcome[TToken, TTokenRole], pattern.RegulaAST[TObservation]], len(ruleset.precompiledRules))
	for i, rule := range ruleset.precompiledRules {
		ruleOutcome := TokenOutcome[TToken, TTokenRole]{Token: rule.token, Priority: rule.priority, TokenRole: rule.role}
		instructions[i] = pattern.PatternCompilationInstruction[TObservation, TokenOutcome[TToken, TTokenRole], pattern.RegulaAST[TObservation]]{
			Pattern: &rule.pattern,
			Outcome: ruleOutcome,
		}
	}

	nfas, err := compiler(scratchAllocFn, instructions, ctx, nonTerminalOutcome)
	if err != nil {
		panic(fmt.Errorf("lexing ruleset error: %w", err))
	}

	outNFA := nfas[0]

	for i, generatedNFA := range nfas {
		if i == 0 {
			continue
		}

		outNFA = autarch.NFAMergeOr(outNFA, generatedNFA, scratchAllocFn)
	}

	// 1. Define the resolution strategy
	resFn := func(states []uint64, outcomes []pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]) (pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]], bool) {
		var best pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]
		found := false

		for _, out := range outcomes {
			// Ignore the explicit non-terminal baseline
			if out.Value == nonTerminalOutcome {
				continue
			}

			// First valid outcome becomes the baseline
			if !found {
				best = out
				found = true
				continue
			}

			// If multiple patterns match, highest priority wins
			// (e.g., TokKWTrue priority 2 beats TokIdentifier priority 1)
			if out.Value.Priority > best.Value.Priority {
				best = out
			}
		}

		return best, found
	}

	// 2. Pass it to the subset constructor
	dfa := autarch.NFAToDFA(
		outNFA,
		nfaToDFAPipelineMinTemp,
		nfaToDFAPipelineMaxTemp,
		dfaAllocFn,
		pattern.SharedCompilationContextDeterministicResolverGet(ctx),
		resFn,
	)
	minimizedDFA := autarch.DFAMinimize(
		dfa,
		dfaAllocFn,
		nfaToDFAPipelineMinTemp, nfaToDFAPipelineMaxTemp,
		func(out pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]) pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]] {
			return out
		})
	return minimizedDFA
}
