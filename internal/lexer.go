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
	"slices"
	"unicode/utf8"
)

// ----------------------------------------------------------- SENTINELS

// Sentinel value used to explicitly mark states that are NOT a match
var nonTerminalOutcome = TokenOutcome{
	Kind:     ^TokenKind(0),
	Role:     SentinelTokenRole,
	Priority: -999,
}

const ErrorToken TokenKind = 0
const EOFToken TokenKind = 1
const SentinelTokenRole TokenRole = ^TokenRole(0)
const SentinelToken TokenKind = ^TokenKind(0)

// ----------------------------------------------------------- RESULT

type NextResult struct {
	Token       *Token
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

	noClientStackMutations bool
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
		noClientStackMutations: false,
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

	for _, rule := range state.rules {
		if rule.kind == ErrorToken || rule.kind == EOFToken {
			panic(fmt.Errorf("a rule inside state '%s' makes use of reserved tokens ERROR or EOF", state.descriptor))
		}
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
	if min > max {
		panic("max must be bigger than or equal to min")
	}

	cfg.minScratchMem = min
	cfg.maxScratchMem = max
}

func LexerConfigurationSetMainMemory(cfg *LexerConfiguration, min, max memcore.MemoryUnitBytes) {
	if min > max {
		panic("max must be bigger than or equal to min")
	}

	cfg.minMainMem = min
	cfg.maxMainMem = max
}

func LexerConfigurationSetTokenKindFormatter(cfg *LexerConfiguration, formatter func(kind TokenKind) string) {
	cfg.kindFormatter = formatter
}

func LexerConfigurationDisableClientStackMutations(cfg *LexerConfiguration) {
	cfg.noClientStackMutations = true
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
		"lexarch scratch",
	)
	mainAllocator := memforge.DynamicLinearAllocatorCreateFunction(
		uint64(cfg.minMainMem),
		memforge.DynamicLinearAllocatorGrowthTemplateDoubleOrNeededWithMaxPanic(uint64(cfg.maxMainMem)),
		"lexarch main",
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

const lexingStackDepthMax = 16

type LexingSession struct {
	lexer *Lexer

	content string

	lexingStateStack      [lexingStackDepthMax]stackFrame
	lexingStateStackDepth uint8

	dfa                *autarch.DFA[rune, pattern.AnnotatedOutcome[TokenOutcome]]
	lexingContentCache *TokenCache

	contentOffsetBytes uint32
	fileID             uint16

	tempToken *Token

	popStack func(session *LexingSession, lexerRequested bool, amount int)
}

const bottomOfStackMarker = ^int(0)

func LexerLexingSessionCreate(lexer *Lexer, content string, fileID uint16) *LexingSession {
	dfa := lexer.stateRules[lexer.startState]
	stack := [lexingStackDepthMax]stackFrame{}
	stack[0] = stackFrame{
		id:           bottomOfStackMarker,
		ownedByLexer: true,
	}
	stack[1] = stackFrame{
		id:           lexer.startState,
		ownedByLexer: true,
	}

	popStack := lexingSessionPopWithValidation

	if lexer.config.noClientStackMutations {
		popStack = lexingSessionPopNoValidation
	}

	return &LexingSession{
		lexer:                 lexer,
		content:               content,
		lexingStateStack:      stack,
		lexingStateStackDepth: 2,
		dfa:                   dfa,
		contentOffsetBytes:    0,
		lexingContentCache:    TokenCacheCreate(),
		fileID:                fileID,
		tempToken: &Token{
			Kind:   SentinelToken,
			Role:   SentinelTokenRole,
			FileID: fileID,
			Span: ByteSpan{
				Offset: 0,
				Length: 0,
			},
		},
		popStack: popStack,
	}
}

func LexerLexingSessionReset(lexer *Lexer, session *LexingSession, content string, fileID uint16) {
	dfa := lexer.stateRules[lexer.startState]

	session.lexer = lexer
	session.content = content
	session.dfa = dfa
	session.contentOffsetBytes = 0
	session.fileID = fileID

	TokenCacheClear(session.lexingContentCache)

	session.lexingStateStackDepth = 2
	session.lexingStateStack[0] = stackFrame{
		id:           bottomOfStackMarker,
		ownedByLexer: true,
	}
	session.lexingStateStack[1] = stackFrame{
		id:           lexer.startState,
		ownedByLexer: true,
	}

	// No need to reset the temp token explicitly
	session.tempToken.FileID = fileID

	// No need to reset the snapshot explicitly
}

type LexingSessionSnapshot struct {
	contentOffsetBytes uint32
	lexingStateStack   [lexingStackDepthMax]stackFrame
	stackDepth         uint8
}

func LexingSessionSnapshotCreate(session *LexingSession) LexingSessionSnapshot {
	snapshot := LexingSessionSnapshot{
		contentOffsetBytes: session.contentOffsetBytes,
		stackDepth:         session.lexingStateStackDepth,
	}
	snapshot.lexingStateStack = session.lexingStateStack
	return snapshot
}

func LexingSessionSnapshotRestore(session *LexingSession, snapshot LexingSessionSnapshot) {
	session.contentOffsetBytes = snapshot.contentOffsetBytes
	session.lexingStateStackDepth = snapshot.stackDepth
	session.lexingStateStack = snapshot.lexingStateStack

	if session.lexingStateStackDepth == 0 {
		panic("engine-error: snapshot restored with empty stack")
	}

	restoredTop := session.lexingStateStack[session.lexingStateStackDepth-1]
	lexingSessionSetDFAForState(session, restoredTop.id)
}

func LexingSessionNextResultCreate() *NextResult {
	return &NextResult{
		Token:       nil,
		LexingError: nil,
	}
}

func LexingSessionPrefillCache(session *LexingSession) error {
	// This is a no-op if there are more than 1 state rules...
	// thus this is safe to call for clients every time

	if session.lexer.config.noClientStackMutations || len(session.lexer.stateRules) == 1 {
		out := LexingSessionNextResultCreate()
		for {
			LexingSessionConsumeUnsafe(session, out)

			if out.Token.Kind == EOFToken {
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

func LexingSessionCurrent(session *LexingSession, out *NextResult) {
	if session.lexer.destroyed {
		out.LexingError = &LexerValidationError{
			msg: "cannot use a destroyed lexer",
		}
		return
	}

	snapshot := LexingSessionSnapshotCreate(session)
	lexingSessionNext(session, out)
	LexingSessionSnapshotRestore(session, snapshot)
}

func LexingSessionCurrentUnsafe(session *LexingSession, out *NextResult) {
	snapshot := LexingSessionSnapshotCreate(session)
	lexingSessionNext(session, out)
	LexingSessionSnapshotRestore(session, snapshot)
}

func LexingSessionPeek(session *LexingSession, out *NextResult, n int) {
	if session.lexer.destroyed {
		out.LexingError = &LexerValidationError{
			msg: "cannot use a destroyed lexer",
		}
		return
	}

	snapshot := LexingSessionSnapshotCreate(session)

	for i := 0; i < n; i++ {
		advanced := lexingSessionNext(session, out)
		session.contentOffsetBytes += uint32(advanced)
	}

	LexingSessionSnapshotRestore(session, snapshot)
}

func LexingSessionPeekUnsafe(session *LexingSession, out *NextResult, n int) {
	snapshot := LexingSessionSnapshotCreate(session)

	for i := 0; i < n; i++ {
		advanced := lexingSessionNext(session, out)
		session.contentOffsetBytes += uint32(advanced)
	}

	LexingSessionSnapshotRestore(session, snapshot)
}

func LexingSessionPushStates(session *LexingSession, lexerOwned bool, states ...string) {
	if len(states) == 0 {
		return
	}

	newDepth := int(session.lexingStateStackDepth) + len(states)
	if newDepth > lexingStackDepthMax {
		panic("engine-error: lexing state stack overflow")
	}

	for _, state := range states {
		next := session.lexingStateStackDepth
		session.lexingStateStack[next] = stackFrame{id: session.lexer.stateMap[state], ownedByLexer: lexerOwned}
		session.lexingStateStackDepth++
	}

	lastResolved := session.lexingStateStack[session.lexingStateStackDepth-1]
	lexingSessionSetDFAForState(session, lastResolved.id)
}

func LexingSessionPop(session *LexingSession, lexerRequested bool, amount int) {
	session.popStack(session, lexerRequested, amount)
}

func LexingSessionSet(session *LexingSession, ownedByLexer bool, targets ...string) {
	// TODO - inline the logic for performance

	LexingSessionPop(session, ownedByLexer, 1)
	LexingSessionPushStates(session, ownedByLexer, targets...)
}

func LexingSessionStateCurrent(session *LexingSession) string {
	if session == nil || session.lexingStateStackDepth < 1 {
		return ""
	}
	top := session.lexingStateStack[session.lexingStateStackDepth-1]
	if top.id == bottomOfStackMarker {
		return ""
	}
	return session.lexer.config.states[top.id].descriptor
}

func LexingSessionValidateStateMutation(session *LexingSession) {
	if session.lexer.config.noClientStackMutations {
		panic("error: client is not allowed to mutate the lexer state stack")
	}
}

// ----------------------------------------------------------- PRIVATE HELPERS

//go:inline
func lexingSessionPopWithValidation(session *LexingSession, lexerRequested bool, amount int) {
	currentLen := int(session.lexingStateStackDepth)

	if amount <= 0 || currentLen <= 1 {
		return
	}

	targetIdx := currentLen - amount
	if targetIdx < 1 {
		targetIdx = 1
	}

	newLen := currentLen
	for i := currentLen - 1; i >= targetIdx; i-- {
		frame := session.lexingStateStack[i]

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

	session.lexingStateStackDepth = uint8(newLen)

	lexingSessionSetDFAForState(session, session.lexingStateStack[session.lexingStateStackDepth-1].id)
}

//go:inline
func lexingSessionPopNoValidation(session *LexingSession, _ bool, amount int) {
	currentLen := int(session.lexingStateStackDepth)

	if amount <= 0 || currentLen <= 1 {
		return
	}

	targetIdx := currentLen - amount
	if targetIdx < 1 {
		targetIdx = 1
	}

	newLen := currentLen
	for i := currentLen - 1; i >= targetIdx; i-- {
		frame := session.lexingStateStack[i]

		if frame.id == bottomOfStackMarker {
			break
		}
		newLen = i
	}

	if newLen < 1 {
		newLen = 1
	}

	session.lexingStateStackDepth = uint8(newLen)

	lexingSessionSetDFAForState(session, session.lexingStateStack[session.lexingStateStackDepth-1].id)
}

//go:inline
//go:nosplit
func lexingSessionSetDFAForState(session *LexingSession, state int) {
	session.dfa = session.lexer.stateRules[state]
}

//go:inline
//go:nosplit
func lexingSessionNext(session *LexingSession, out *NextResult) (advanced uint32) {
	out.Token = nil
	out.LexingError = nil

	// Perf note:
	// Do not add a separate cached "current state id" field here.
	// In the current architecture, stack mutations (push/pop/set/restore) already perform
	// DFA + stack synchronization. Keeping an extra cached state id adds update/sync work on
	// those mutation paths and regressed benchmark throughput in measured runs.
	currentState := session.lexingStateStack[session.lexingStateStackDepth-1]

	if session.contentOffsetBytes >= uint32(len(session.content)) {
		session.tempToken.Kind = EOFToken
		session.tempToken.Role = SentinelTokenRole
		session.tempToken.Span.Offset = 0
		session.tempToken.Span.Length = 0

		out.Token = session.tempToken
		return 0
	}

	currentStateID := currentState.id
	if cachedToken, ok, hasStackOperation, stackOperationID := TokenCacheGet(session.lexingContentCache, getCacheKey(session.contentOffsetBytes, currentStateID)); ok {
		out.Token = cachedToken
		if hasStackOperation {
			applyStackOperation(session, stackOperationID)
		}
		return cachedToken.Span.Length
	}

	spannedContent := session.content[session.contentOffsetBytes:]
	ok, tokenAdvanced := lexToken(session, spannedContent, session.contentOffsetBytes, currentStateID, out)
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
	currentStateID int,
	result *NextResult,
) (ok bool, advancedBytes int) {
	session.tempToken.Span.Offset = absoluteOffset // We only need to reset the offset, the rest is done automatically.

	dfaState := autarch.StartStateID
	bestStackOperationID := -1
	furthestMatchBytes := -1
	highestPriority := -1

	for offset, char := range contentSlice {
		nextDFAState, err := autarch.DFAStep(session.dfa, dfaState, char)

		if err != nil {
			if furthestMatchBytes != -1 {
				break
			}
			return failWithUnexpectedChar(session, result, absoluteOffset, uint32(offset), char)
		}

		if nextDFAState == autarch.DeadState {
			if furthestMatchBytes != -1 {
				break
			}
			return failWithInvalidSyntax(session, result, absoluteOffset, uint32(offset), char)
		}

		dfaState = nextDFAState

		// Fast-path ASCII byte length calculation
		charLen := 1
		if char >= utf8.RuneSelf {
			charLen = utf8.RuneLen(char)
		}
		currentLengthBytes := offset + charLen

		outcome := autarch.DFAStateOutcomeUnsafe(session.dfa, dfaState)
		if outcome.Value != nonTerminalOutcome {
			isFurther := currentLengthBytes > furthestMatchBytes
			isEqualButHigherPriority := currentLengthBytes == furthestMatchBytes && outcome.Value.Priority > highestPriority

			if isFurther || isEqualButHigherPriority {
				furthestMatchBytes = currentLengthBytes
				highestPriority = outcome.Value.Priority

				session.tempToken.Kind = outcome.Value.Kind
				session.tempToken.Role = outcome.Value.Role
				session.tempToken.Span.Length = uint32(currentLengthBytes)

				if outcome.Value.HasStackOperation {
					bestStackOperationID = outcome.Value.StackOperationID
				} else {
					bestStackOperationID = -1
				}
			}
		}
	}

	if furthestMatchBytes != -1 {
		result.Token = session.tempToken
		TokenCachePut(
			session.lexingContentCache,
			getCacheKey(absoluteOffset, currentStateID),
			session.tempToken,
			bestStackOperationID != -1,
			bestStackOperationID,
		)

		if bestStackOperationID != -1 {
			applyStackOperation(session, bestStackOperationID)
		}
		return true, furthestMatchBytes
	}

	return false, 0
}

//go:noinline
func failWithUnexpectedChar(session *LexingSession, result *NextResult, absOffset, relOffset uint32, char rune) (bool, int) {
	charLen := 1
	if char >= utf8.RuneSelf {
		charLen = utf8.RuneLen(char)
	}

	currentSpan := ByteSpan{
		Offset: absOffset + relOffset,
		Length: uint32(charLen),
	}

	session.tempToken.Kind = ErrorToken
	session.tempToken.Role = SentinelTokenRole
	session.tempToken.Span = currentSpan

	result.Token = session.tempToken

	result.LexingError = &LexerRuntimeError{
		msg:  fmt.Sprintf("unexpected character '%c'", char),
		area: currentSpan,
	}
	return true, int(relOffset) + charLen
}

//go:noinline
func failWithInvalidSyntax(session *LexingSession, result *NextResult, absOffset, relOffset uint32, char rune) (bool, int) {
	charLen := 1
	if char >= utf8.RuneSelf {
		charLen = utf8.RuneLen(char)
	}

	currentSpan := ByteSpan{
		Offset: absOffset + relOffset,
		Length: uint32(charLen),
	}

	session.tempToken.Kind = ErrorToken
	session.tempToken.Role = SentinelTokenRole
	session.tempToken.Span = currentSpan

	result.Token = session.tempToken

	result.LexingError = &LexerRuntimeError{
		msg:  "invalid token syntax",
		area: currentSpan,
	}
	return true, int(relOffset) + charLen
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
			ruleOutcome.StackOperationID = stackOpID
			ruleOutcome.HasStackOperation = true
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
