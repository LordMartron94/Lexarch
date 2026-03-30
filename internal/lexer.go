package internal

import (
	"autarch"
	"autarch/pattern"
	"fmt"
	"foundation/bytes"
	"foundation/domain"
	"memarch"
	"memcore"
	"memforge"
	"memstruct"
	"slices"
	"unicode/utf8"
)

// ----------------------------------------------------------- SENTINELS

// Sentinel value used to explicitly mark states that are NOT a match
var nonTerminalOutcome = TokenOutcome{
	Kind:     ^TokenKind(0),
	Role:     ^TokenRole(0),
	Priority: -999,
}

// ----------------------------------------------------------- RESULT

type LexerLexResult struct {
	Tokens      []Token
	EOF         bool
	LexingError error
}

// ----------------------------------------------------------- CONFIGURATION

type LexerConfiguration struct {
	states          []LexingState
	startState      string
	patternCompiler PatternCompilerMode

	minScratchMem, maxScratchMem memcore.MemoryUnitBytes
	minMainMem, maxMainMem       memcore.MemoryUnitBytes

	kindFormatter func(kind TokenKind) string
}

func LexerConfigurationCreate() *LexerConfiguration {
	return &LexerConfiguration{
		states:          make([]LexingState, 0),
		startState:      "",
		patternCompiler: PATTERN_COMPILE_GLUSHKOV,
		minScratchMem:   memcore.KiloByte,
		maxScratchMem:   100 * memcore.MegaByte,
		minMainMem:      memcore.KiloByte,
		maxMainMem:      memcore.GigaByte,
		kindFormatter: func(kind TokenKind) string {
			return fmt.Sprintf("%d", kind)
		},
	}
}

func LexerConfigurationCompilerSetPatternCompiler(cfg *LexerConfiguration, compiler PatternCompilerMode) {
	cfg.patternCompiler = compiler
}

func LexerConfigurationRegisterState(cfg *LexerConfiguration, state LexingState, isStart bool) {
	if slices.ContainsFunc(cfg.states, func(item LexingState) bool {
		return item.descriptor == state.descriptor
	}) {
		panic(fmt.Errorf("state with descriptor '%s' already registered", state.descriptor))
	}

	cfg.states = append(cfg.states, state)

	if isStart {
		if cfg.startState != "" {
			panic("start state already set")
		}

		cfg.startState = state.descriptor
	}
}

func LexerConfigurationSetScratchMemory(cfg *LexerConfiguration, min, max memcore.MemoryUnitBytes) {
	if min < max {
		panic("max must be bigger than or equal to min")
	}

	cfg.minScratchMem = min
	cfg.maxScratchMem = max
}

func LexerConfigurationSetMainMemory(cfg *LexerConfiguration, min, max memcore.MemoryUnitBytes) {
	if min < max {
		panic("max must be bigger than or equal to min")
	}

	cfg.minMainMem = min
	cfg.maxMainMem = max
}

func LexerConfigurationSetTokenKindFormatter(cfg *LexerConfiguration, formatter func(kind TokenKind) string) {
	cfg.kindFormatter = formatter
}

// ----------------------------------------------------------- LEXING SESSION

type LexingSession struct {
	lexingState int
	dfaCursor   *memstruct.ArrayCursor[uint64]
}

// ----------------------------------------------------------- LEXER

type Lexer struct {
	config *LexerConfiguration

	scratchAllocator memcore.MarkRaw
	mainAllocator    memcore.MarkRaw

	startState int
	stateRules []*autarch.DFA[rune, pattern.AnnotatedOutcome[TokenOutcome]]

	destroyed bool
}

func LexerCreate(cfg *LexerConfiguration) *Lexer {
	var startState int
	stateRules := make([]*autarch.DFA[rune, pattern.AnnotatedOutcome[TokenOutcome]], len(cfg.states))

	scratchAllocator := memforge.DynamicLinearAllocatorCreateFunction(
		uint64(cfg.minScratchMem),
		memforge.DynamicLinearAllocatorGrowthTemplateDoubleOrNeededWithMaxPanic(uint64(cfg.maxScratchMem)),
	)
	mainAllocator := memforge.DynamicLinearAllocatorCreateFunction(
		uint64(cfg.minMainMem),
		memforge.DynamicLinearAllocatorGrowthTemplateDoubleOrNeededWithMaxPanic(uint64(cfg.maxMainMem)),
	)

	for i, state := range cfg.states {
		stateRules[i] = compileState(
			state, cfg,
			func(sizeBytes, alignment uint64) memcore.MarkRaw {
				return memforge.DynamicLinearAllocatorMallocUnsafe(scratchAllocator, sizeBytes, alignment)
			},
			func(sizeBytes, alignment uint64) memcore.MarkRaw {
				return memforge.DynamicLinearAllocatorMallocUnsafe(mainAllocator, sizeBytes, alignment)
			},
		)

		if state.descriptor == cfg.startState {
			startState = i
		}
	}

	return &Lexer{
		config:           cfg,
		startState:       startState,
		destroyed:        false,
		scratchAllocator: scratchAllocator,
		mainAllocator:    mainAllocator,
		stateRules:       stateRules,
	}
}

func LexerDestroy(lexer *Lexer) {
	memforge.DynamicLinearAllocatorDestroy(lexer.scratchAllocator)
	memforge.DynamicLinearAllocatorDestroy(lexer.mainAllocator)
	lexer.destroyed = true
}

func LexerLexContentFull(
	lexer *Lexer,
	content string,
) LexerLexResult {
	if lexer.destroyed {
		panic("cannot use a destroyed lexer")
	}

	result := LexerLexResult{
		Tokens: make([]Token, 0),
		EOF:    false,
	}

	session := &LexingSession{
		lexingState: lexer.startState,
	}

	lexContentFull(lexer, session, content, &result)

	return result
}

// ----------------------------------------------------------- PRIVATE HELPERS

func lexContentFull(lexer *Lexer, session *LexingSession, content string, result *LexerLexResult) {
	contentLength := len(content)
	if contentLength == 0 {
		result.EOF = true
		return
	}

	offset := uint32(0)
	for {
		spannedContent := content[offset:]
		if len(spannedContent) == 0 {
			return
		}

		ok, advanced := lexToken(lexer, session, spannedContent, offset, result)
		if !ok {
			return
		}

		offset += uint32(advanced)
	}
}

func lexToken(
	lexer *Lexer,
	session *LexingSession,
	contentSlice string,
	absoluteOffset uint32,
	result *LexerLexResult,
) (ok bool, advancedBytes int) {
	dfa := lexer.stateRules[session.lexingState] // TODO - implement proper state switching
	cursor := autarch.DFACursorGet(dfa)
	session.dfaCursor = &cursor

	dfaState := autarch.StartStateID

	bestToken := Token{
		Kind:   ^TokenKind(0),
		Role:   ^TokenRole(0),
		FileID: 0, // TODO - implement file IDs
		Span: ByteSpan{
			Offset: absoluteOffset,
			Length: 0,
		},
	}

	furthestMatchBytes := -1
	highestPriority := -1
	currentRelativeByte := uint32(0)

	for _, char := range contentSlice {
		byteLen := uint32(utf8.RuneLen(char))

		nextDFAState, err := autarch.DFAStep(dfa, dfaState, char, cursor)

		if err != nil {
			if furthestMatchBytes != -1 {
				break
			}
			result.LexingError = &LexerError{
				msg: fmt.Sprintf("unexpected character '%c'", char),
				area: ByteSpan{
					Offset: absoluteOffset + currentRelativeByte,
					Length: byteLen,
				},
			}
			return false, 0
		}

		if autarch.DFAIsDeadState(dfa, nextDFAState) {
			if furthestMatchBytes != -1 {
				break
			}
			result.LexingError = &LexerError{
				msg: "invalid token syntax",
				area: ByteSpan{
					Offset: absoluteOffset + currentRelativeByte,
					Length: byteLen,
				},
			}
			return false, 0
		}

		dfaState = nextDFAState

		if outcome, isTerminal := autarch.DFAStateOutcome(dfa, dfaState); isTerminal && outcome.Value != nonTerminalOutcome {
			currentLengthBytes := int(currentRelativeByte + byteLen)

			if currentLengthBytes > furthestMatchBytes {
				goto done
			} else if currentLengthBytes == furthestMatchBytes {
				if outcome.Value.Priority > highestPriority {
					goto done
				}
			}
		done:
			furthestMatchBytes = currentLengthBytes
			highestPriority = outcome.Value.Priority

			bestToken.Kind = outcome.Value.Kind
			bestToken.Role = outcome.Value.Role
			bestToken.Span.Length = uint32(currentLengthBytes)
		}

		currentRelativeByte += byteLen
	}

	if furthestMatchBytes != -1 {
		result.Tokens = append(result.Tokens, bestToken)
		return true, furthestMatchBytes
	}

	return false, 0
}

func compileState(state LexingState, cfg *LexerConfiguration, scratchAllocFn, mainAllocFn memarch.AllocationFn) *autarch.DFA[rune, pattern.AnnotatedOutcome[TokenOutcome]] {
	var compiler pattern.RegulaToNFACompiler[rune, TokenOutcome]
	switch cfg.patternCompiler {
	case PATTERN_COMPILE_THOMPSON:
		compiler = pattern.RegulaCompileToNFAThompson
	case PATTERN_COMPILE_GLUSHKOV:
		compiler = pattern.RegulaCompileToNFAGlushkov
	default:
		panic("unknown compilation mode")
	}

	rules := state.rules
	numRules := len(rules)

	for i := 0; i < numRules; i++ {
		for j := i + 1; j < numRules; j++ {
			if pattern.PatternEquivalent(&rules[i].pattern, &rules[j].pattern, func(a, b rune) bool { return a == b }) {
				panic(fmt.Sprintf(
					"lexing ruleset: duplicate pattern: tokens '%s' and '%s' have equivalent patterns",
					cfg.kindFormatter(rules[i].kind),
					cfg.kindFormatter(rules[j].kind),
				))
			}
		}
	}

	ctx := pattern.CreateSharedCompilationContext[rune, pattern.RegulaAST[rune]](
		domain.DiscreteDomainRuneCreate(),
		pattern.ObservationFormatter[rune]{
			ToBytes: func(observations []rune) []byte {
				return bytes.StringToBytes(string(observations))
			},
		},
	)

	instructions := make([]pattern.PatternCompilationInstruction[rune, TokenOutcome, pattern.RegulaAST[rune]], numRules)
	for i, rule := range rules {
		ruleOutcome := TokenOutcome{Kind: rule.kind, Priority: rule.priority, Role: rule.role}
		instructions[i] = pattern.PatternCompilationInstruction[rune, TokenOutcome, pattern.RegulaAST[rune]]{
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
	resFn := func(states []uint64, outcomes []pattern.AnnotatedOutcome[TokenOutcome]) (pattern.AnnotatedOutcome[TokenOutcome], bool) {
		var best pattern.AnnotatedOutcome[TokenOutcome]
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
		cfg.minScratchMem,
		cfg.maxScratchMem,
		scratchAllocFn,
		pattern.SharedCompilationContextDeterministicResolverGet(ctx),
		resFn,
	)
	minimizedDFA := autarch.DFAMinimize(
		dfa,
		mainAllocFn,
		cfg.minScratchMem, cfg.maxScratchMem,
		func(out pattern.AnnotatedOutcome[TokenOutcome]) pattern.AnnotatedOutcome[TokenOutcome] {
			return out
		})

	return minimizedDFA
}
