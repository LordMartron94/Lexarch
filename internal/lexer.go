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

type NextResult struct {
	Token       *Token
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

// ----------------------------------------------------------- LEXER

type stackOperation struct {
	kind    StackOperationKind
	payload StackOperationPayload
}

type Lexer struct {
	config *LexerConfiguration

	scratchAllocator memcore.MarkRaw
	mainAllocator    memcore.MarkRaw

	startState int
	stateRules []*autarch.DFA[rune, pattern.AnnotatedOutcome[TokenOutcome]]
	stateMap   map[string]int

	stackOperations []stackOperation

	destroyed bool
}

func LexerCreate(cfg *LexerConfiguration) *Lexer {
	var startState int
	stateRules := make([]*autarch.DFA[rune, pattern.AnnotatedOutcome[TokenOutcome]], len(cfg.states))
	stateMap := make(map[string]int)

	scratchAllocator := memforge.DynamicLinearAllocatorCreateFunction(
		uint64(cfg.minScratchMem),
		memforge.DynamicLinearAllocatorGrowthTemplateDoubleOrNeededWithMaxPanic(uint64(cfg.maxScratchMem)),
	)
	mainAllocator := memforge.DynamicLinearAllocatorCreateFunction(
		uint64(cfg.minMainMem),
		memforge.DynamicLinearAllocatorGrowthTemplateDoubleOrNeededWithMaxPanic(uint64(cfg.maxMainMem)),
	)

	stackOperations := computeStackOperations(cfg.states)

	for i, state := range cfg.states {
		stateRules[i] = compileState(
			state, cfg, stackOperations,
			func(sizeBytes, alignment uint64) memcore.MarkRaw {
				return memforge.DynamicLinearAllocatorMallocUnsafe(scratchAllocator, sizeBytes, alignment)
			},
			func(sizeBytes, alignment uint64) memcore.MarkRaw {
				return memforge.DynamicLinearAllocatorMallocUnsafe(mainAllocator, sizeBytes, alignment)
			},
		)
		stateMap[state.descriptor] = i

		if state.descriptor == cfg.startState {
			startState = i
		}
	}

	for _, dfa := range stateRules {
		autarch.DFARefreshCursors(dfa)
	}

	return &Lexer{
		config:           cfg,
		startState:       startState,
		destroyed:        false,
		scratchAllocator: scratchAllocator,
		mainAllocator:    mainAllocator,
		stateRules:       stateRules,
		stateMap:         stateMap,
		stackOperations:  stackOperations,
	}
}

func LexerDestroy(lexer *Lexer) {
	memforge.DynamicLinearAllocatorDestroy(lexer.scratchAllocator)
	memforge.DynamicLinearAllocatorDestroy(lexer.mainAllocator)
	lexer.destroyed = true
}

type stackFrame struct {
	id           int
	ownedByLexer bool
}

type LexingSession struct {
	lexer *Lexer

	content string

	lexingStateStack []stackFrame

	dfa       *autarch.DFA[rune, pattern.AnnotatedOutcome[TokenOutcome]]
	dfaCursor memstruct.ArrayCursor[uint64]

	lexingContentCache map[uint64]Token // byte offset + state -> token ; TODO - if this is a perf bottleneck, find a better way to store

	contentOffsetBytes uint32
	fileID             uint16
}

type LexingSessionSnapshot struct {
	contentOffsetBytes uint32
	lexingStateStack   []stackFrame
}

func LexingSessionSnapshotCreate(session *LexingSession) LexingSessionSnapshot {
	cp := make([]stackFrame, len(session.lexingStateStack))
	copy(cp, session.lexingStateStack)

	return LexingSessionSnapshot{
		contentOffsetBytes: session.contentOffsetBytes,
		lexingStateStack:   cp,
	}
}

func LexingSessionSnapshotRestore(session *LexingSession, snapshot LexingSessionSnapshot) {
	cp := make([]stackFrame, len(snapshot.lexingStateStack))
	copy(cp, snapshot.lexingStateStack)

	session.contentOffsetBytes = snapshot.contentOffsetBytes
	session.lexingStateStack = cp
	session.lexingContentCache = make(map[uint64]Token) // TODO - find a way to optimize this and not nuke the entire cache

	restoredTop := session.lexingStateStack[len(session.lexingStateStack)-1]
	lexingSessionSetDFAForState(session, restoredTop.id)
}

const bottomOfStackMarker = ^int(0)

func LexerLexingSessionCreate(lexer *Lexer, content string, fileID uint16) *LexingSession {
	dfa := lexer.stateRules[lexer.startState]
	cursor := autarch.DFACursorGet(dfa)

	return &LexingSession{
		lexer:   lexer,
		content: content,
		lexingStateStack: []stackFrame{
			{
				id:           bottomOfStackMarker,
				ownedByLexer: true,
			},
			{
				id:           lexer.startState,
				ownedByLexer: true,
			},
		}, // TODO - maybe replace with memstruct dynamic stack if faster than normal slice
		dfa:                dfa,
		dfaCursor:          cursor,
		contentOffsetBytes: 0,
		lexingContentCache: make(map[uint64]Token),
		fileID:             fileID,
	}
}

func LexingSessionNextResultCreate() *NextResult {
	return &NextResult{
		Token:       nil,
		EOF:         false,
		LexingError: nil,
	}
}

func LexingSessionPrefillCache(session *LexingSession) error {
	// This is a no-op if there are more than 1 state rules...
	// thus this is safe to call for clients every time

	if len(session.lexer.stateRules) == 1 {
		out := LexingSessionNextResultCreate()
		for {
			LexingSessionConsumeUnsafe(session, out)

			if out.EOF {
				break
			}

			if out.LexingError != nil {
				return out.LexingError
			}
		}

		session.contentOffsetBytes = 0
	}

	return nil
}

func LexingSessionConsume(session *LexingSession, out *NextResult) {
	if session.lexer.destroyed {
		out.LexingError = &LexerValidationError{
			msg: "cannot use a destroyed lexer",
		}
		return
	}

	advanced := lexingSessionNext(session, out)
	session.contentOffsetBytes += uint32(advanced)
}

func LexingSessionConsumeUnsafe(session *LexingSession, out *NextResult) {
	advanced := lexingSessionNext(session, out)
	session.contentOffsetBytes += uint32(advanced)
}

func LexingSessionPeek(session *LexingSession, out *NextResult, n int) {
	if session.lexer.destroyed {
		out.LexingError = &LexerValidationError{
			msg: "cannot use a destroyed lexer",
		}
		return
	}

	snap := LexingSessionSnapshotCreate(session)

	for i := 0; i < n; i++ {
		advanced := lexingSessionNext(session, out)
		session.contentOffsetBytes += uint32(advanced)
	}

	LexingSessionSnapshotRestore(session, snap)
}

func LexingSessionPeekUnsafe(session *LexingSession, out *NextResult, n int) {
	snap := LexingSessionSnapshotCreate(session)

	for i := 0; i < n; i++ {
		advanced := lexingSessionNext(session, out)
		session.contentOffsetBytes += uint32(advanced)
	}

	LexingSessionSnapshotRestore(session, snap)
}

func LexingSessionPushStates(session *LexingSession, lexerOwned bool, states ...string) {
	if len(states) == 0 {
		return
	}

	for _, state := range states {
		session.lexingStateStack = append(session.lexingStateStack, stackFrame{id: session.lexer.stateMap[state], ownedByLexer: lexerOwned})
	}

	lastResolved := session.lexingStateStack[len(session.lexingStateStack)-1]
	lexingSessionSetDFAForState(session, lastResolved.id)
}

func LexingSessionPop(session *LexingSession, lexerRequested bool, amount int) {
	stack := session.lexingStateStack
	currentLen := len(stack)

	if amount <= 0 || currentLen <= 1 {
		return
	}

	targetIdx := currentLen - amount
	if targetIdx < 1 {
		targetIdx = 1
	}

	newLen := currentLen
	for i := currentLen - 1; i >= targetIdx; i-- {
		frame := stack[i]

		if frame.ownedByLexer != lexerRequested { // valids are: (ownedByLexer AND lexerRequested) OR (!ownedByLexer AND !lexerRequested)
			panic("engine-error: pop must be executed by owner")
		}

		if frame.id == bottomOfStackMarker {
			break
		}
		newLen = i
	}

	if newLen < 1 {
		newLen = 1
	}

	session.lexingStateStack = stack[:newLen]

	lexingSessionSetDFAForState(session, session.lexingStateStack[newLen-1].id)
}

func LexingSessionSet(session *LexingSession, ownedByLexer bool, targets ...string) {
	// TODO - inline the logic for performance

	LexingSessionPop(session, ownedByLexer, 1)
	LexingSessionPushStates(session, ownedByLexer, targets...)
}

// ----------------------------------------------------------- PRIVATE HELPERS

//go:inline
//go:nosplit
func lexingSessionSetDFAForState(session *LexingSession, state int) {
	dfa := session.lexer.stateRules[state]
	cursor := autarch.DFACursorGet(dfa)

	session.dfa = dfa
	session.dfaCursor = cursor
}

//go:inline
//go:nosplit
func lexingSessionNext(session *LexingSession, out *NextResult) (advanced uint32) {
	out.Token = nil
	out.EOF = false
	out.LexingError = nil

	currentState := session.lexingStateStack[len(session.lexingStateStack)-1]

	if session.contentOffsetBytes >= uint32(len(session.content)) {
		out.EOF = true
		return 0
	}

	if cachedToken, ok := session.lexingContentCache[getCacheKey(session.contentOffsetBytes, currentState.id)]; ok {
		out.Token = &cachedToken
		return cachedToken.Span.Length
	}

	spannedContent := session.content[session.contentOffsetBytes:]
	ok, tokenAdvanced := lexToken(session, spannedContent, session.contentOffsetBytes, out)
	if !ok {
		return 0
	}

	return uint32(tokenAdvanced)
}

//go:inline
func getCacheKey(offset uint32, state int) uint64 {
	return (uint64(state) << 32) | uint64(offset)
}

func lexToken(
	session *LexingSession,
	contentSlice string,
	absoluteOffset uint32,
	result *NextResult,
) (ok bool, advancedBytes int) {
	dfaState := autarch.StartStateID

	bestToken := Token{
		Kind:   ^TokenKind(0),
		Role:   ^TokenRole(0),
		FileID: session.fileID,
		Span: ByteSpan{
			Offset: absoluteOffset,
			Length: 0,
		},
	}

	var bestStackOperation *int
	furthestMatchBytes := -1
	highestPriority := -1
	currentRelativeByte := uint32(0)

	for _, char := range contentSlice {
		byteLen := uint32(utf8.RuneLen(char))

		nextDFAState, err := autarch.DFAStep(session.dfa, dfaState, char, session.dfaCursor)

		if err != nil {
			if furthestMatchBytes != -1 {
				break
			}
			result.LexingError = &LexerRuntimeError{
				msg: fmt.Sprintf("unexpected character '%c'", char),
				area: ByteSpan{
					Offset: absoluteOffset + currentRelativeByte,
					Length: byteLen,
				},
			}
			return false, 0
		}

		if autarch.DFAIsDeadState(session.dfa, nextDFAState) {
			if furthestMatchBytes != -1 {
				break
			}
			result.LexingError = &LexerRuntimeError{
				msg: "invalid token syntax",
				area: ByteSpan{
					Offset: absoluteOffset + currentRelativeByte,
					Length: byteLen,
				},
			}
			return false, 0
		}

		dfaState = nextDFAState

		if outcome, isTerminal := autarch.DFAStateOutcome(session.dfa, dfaState); isTerminal && outcome.Value != nonTerminalOutcome {
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
			bestStackOperation = outcome.Value.StackOperationID
		}

		currentRelativeByte += byteLen
	}

	if furthestMatchBytes != -1 {
		result.Token = &bestToken

		currentState := session.lexingStateStack[len(session.lexingStateStack)-1]
		session.lexingContentCache[getCacheKey(absoluteOffset, currentState.id)] = bestToken

		if bestStackOperation != nil {
			applyStackOperation(session, *bestStackOperation)
		}

		return true, furthestMatchBytes
	}

	return false, 0
}

func applyStackOperation(session *LexingSession, operationID int) {
	resolvedOperation := session.lexer.stackOperations[operationID]
	switch resolvedOperation.kind {
	case STACK_PUSH:
		LexingSessionPushStates(session, true, resolvedOperation.payload.targets...)
	case STACK_POP:
		LexingSessionPop(session, true, *resolvedOperation.payload.pop)
	case STACK_SET:
		LexingSessionSet(session, true, resolvedOperation.payload.targets...)
	}
}

func compileState(
	state LexingState,
	cfg *LexerConfiguration,
	stackOperations []stackOperation,
	scratchAllocFn, mainAllocFn memarch.AllocationFn,
) *autarch.DFA[rune, pattern.AnnotatedOutcome[TokenOutcome]] {
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

		if rule.stackOpKind != STACK_NONE {
			stackOpID := findStackID(stackOperations, rule.stackOpKind, *rule.stackPayload)
			if stackOpID == -1 {
				panic("engine error: unresolved stack operation ID")
			}
			ruleOutcome.StackOperationID = &stackOpID
		}

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

func computeStackOperations(states []LexingState) []stackOperation {
	out := make([]stackOperation, 0)

	for _, state := range states {
		for _, rule := range state.rules {
			if rule.stackOpKind == STACK_NONE {
				continue
			}

			if targetID := findStackID(out, rule.stackOpKind, *rule.stackPayload); targetID == -1 {
				out = append(out, stackOperation{
					kind:    rule.stackOpKind,
					payload: *rule.stackPayload,
				})
			}
		}
	}

	return out
}

func findStackID(stackOperations []stackOperation, kind StackOperationKind, payload StackOperationPayload) int {
	for i, stackOp := range stackOperations {
		if stackOp.kind == kind {
			if stackOp.payload.equal(payload) {
				return i
			}
		}
	}

	return -1
}
