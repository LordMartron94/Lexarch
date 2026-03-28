package lexarch

import (
	"autarch"
	"autarch/pattern"
	"cmp"
	"fmt"
	"memstruct"
)

func lexerGetDFAAndResolution[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	state TState,
) (*autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]], TokenResolutionStepFn[TToken], error) {

	dfa, ok := lexer.ruleSets[state]
	if !ok {
		return nil, nil, fmt.Errorf("no ruleset for state %v", state)
	}

	resolutionStep, ok := lexer.tokenResolutions[state]
	if !ok || resolutionStep == nil {
		resolutionStep = TokenResolutionStepLongest[TToken]
	}

	return dfa, resolutionStep, nil
}

func lexerSessionCursorGet[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
	dfa *autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]],
) memstruct.ArrayCursor[uint64] {
	if cursor, ok := session.dfaCursors[dfa]; ok {
		return cursor
	}
	cursor := autarch.DFACursorGet(dfa)
	session.dfaCursors[dfa] = cursor
	return cursor
}

func streamingLexerSessionCursorGet[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
	dfa *autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]],
) memstruct.ArrayCursor[uint64] {
	if cursor, ok := session.dfaCursors[dfa]; ok {
		return cursor
	}
	cursor := autarch.DFACursorGet(dfa)
	session.dfaCursors[dfa] = cursor
	return cursor
}

func lexemeBuild[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	observationFormatter ObservationFormatter[TObservation],
	token TToken,
	raw []TObservation,
	start, end int,
	startLine, startColumn int,
	endLine, endColumn int,
	tokenNumber int,
	role TTokenRole,
) Lexeme[TObservation, TToken, TTokenRole] {
	return Lexeme[TObservation, TToken, TTokenRole]{
		formatter:   observationFormatter,
		Token:       token,
		Raw:         raw,
		Start:       start,
		End:         end,
		StartLine:   startLine,
		StartColumn: startColumn,
		EndLine:     endLine,
		EndColumn:   endColumn,
		TokenNumber: tokenNumber,
		Role:        role,
	}

}

func lexemeEOF[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	eofToken TToken,
	pos int,
	line int,
	col int,
	tokenNum int,
) Lexeme[TObservation, TToken, TTokenRole] {
	return Lexeme[TObservation, TToken, TTokenRole]{
		Token:       eofToken,
		Raw:         nil,
		Start:       pos,
		End:         pos,
		StartLine:   line,
		StartColumn: col,
		EndLine:     line,
		EndColumn:   col,
		TokenNumber: tokenNum,
	}
}

func computePositionFromSlice[TObservation cmp.Ordered](
	observations []TObservation,
	tracking positionTrackingStrategy[TObservation],
	startLine, startColumn int,
) (endLine, endColumn int) {
	switch tracking.mode {
	case positionTrackingModeRuneFast:
		return computePositionFromSliceRuneFast(observations, tracking.tabWidth, startLine, startColumn)
	case positionTrackingModeByteFast:
		return computePositionFromSliceByteFast(observations, startLine, startColumn)
	default:
		return computePositionFromSliceGeneric(observations, tracking.newlineDetector, tracking.columnAdvanceFn, startLine, startColumn)
	}
}

func computePositionFromSliceGeneric[TObservation cmp.Ordered](
	observations []TObservation,
	newlineDetector NewlineDetector[TObservation],
	advanceColumn ColumnAdvanceFn[TObservation],
	startLine, startColumn int,
) (endLine, endColumn int) {
	line := startLine
	column := startColumn
	for _, obs := range observations {
		if newlineDetector(obs) {
			line++
			column = 1
		} else {
			column = advanceColumn(obs, column)
		}
	}
	return line, column
}

func computePositionFromSliceRuneFast[TObservation cmp.Ordered](
	observations []TObservation,
	tabWidth int,
	startLine, startColumn int,
) (endLine, endColumn int) {
	return computePositionFromSliceRuneFastRunes(unsafeSliceAsRune(observations), tabWidth, startLine, startColumn)
}

func computePositionFromSliceRuneFastRunes(
	observations []rune,
	tabWidth int,
	startLine, startColumn int,
) (endLine, endColumn int) {
	line := startLine
	column := startColumn
	for _, r := range observations {
		if r == '\n' {
			line++
			column = 1
			continue
		}
		if r == '\t' {
			column = column + (tabWidth - ((column - 1) % tabWidth))
			continue
		}
		column++
	}
	return line, column
}

func computePositionFromSliceByteFast[TObservation cmp.Ordered](
	observations []TObservation,
	startLine, startColumn int,
) (endLine, endColumn int) {
	return computePositionFromSliceByteFastBytes(unsafeSliceAsByte(observations), startLine, startColumn)
}

func computePositionFromSliceByteFastBytes(
	observations []byte,
	startLine, startColumn int,
) (endLine, endColumn int) {
	line := startLine
	column := startColumn
	for _, b := range observations {
		if b == '\n' {
			line++
			column = 1
			continue
		}
		column++
	}
	return line, column
}

func lexerCheckEOF[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) (Lexeme[TObservation, TToken, TTokenRole], bool) {
	// fmt.Printf("Checking: pos=%05d, max=%05d; EOF? %v\n", session.position, len(session.input), session.position >= len(session.input))

	if session.position >= len(session.input) {
		return lexerBuildEOF(
			lexer,
			len(session.input),
			session.currentLine,
			session.currentColumn,
			session.tokenNumber,
		), true
	}
	var zero Lexeme[TObservation, TToken, TTokenRole]
	return zero, false
}

func lexerBuildEOFSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) Lexeme[TObservation, TToken, TTokenRole] {
	return lexerBuildEOF(lexer, session.position, session.currentLine, session.currentColumn, session.tokenNumber)
}

func lexerBuildEOFSessionStream[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
) Lexeme[TObservation, TToken, TTokenRole] {
	return lexerBuildEOF(lexer, session.absPos, session.currentLine, session.currentColumn, session.tokenNumber)
}

func lexerBuildEOF[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	pos int,
	line int,
	col int,
	tokenNum int,
) Lexeme[TObservation, TToken, TTokenRole] {
	return Lexeme[TObservation, TToken, TTokenRole]{
		Token:       lexer.eofToken,
		Raw:         nil,
		Start:       pos,
		End:         pos,
		StartLine:   line,
		StartColumn: col,
		EndLine:     line,
		EndColumn:   col,
		TokenNumber: tokenNum,
	}
}

func scanCoreStreaming[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	dfa *autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]],
	cursor memstruct.ArrayCursor[uint64],
	nextObservation func(int) (obs TObservation, ok bool, err error),
	resolutionStep TokenResolutionStepFn[TToken],
	strictEOF bool,
	nonTerminalOutcome TokenOutcome[TToken, TTokenRole],
	tracking positionTrackingStrategy[TObservation],
	startLine, startCol int,
	stats *LexScanStats,
) (bestToken TToken, role TTokenRole, bestEnd int, found bool, dfaState uint64, endLine, endCol int, lexErr *LexingError[TObservation, TToken]) {
	state := uint64(0)
	pos := 0

	bestPriority := 0
	var bestRole TTokenRole

	furthestPos := 0

	curLine := startLine
	curCol := startCol
	bestEndLine := startLine
	bestEndCol := startCol

	for {
		obs, hasObs, err := nextObservation(pos)
		if err != nil {
			return bestToken, bestRole, bestEnd, found, state, bestEndLine, bestEndCol, &LexingError[TObservation, TToken]{
				Position:    pos,
				Reason:      LexErrNoTransition,
				DFAState:    state,
				HasDFAState: true,
			}
		}

		if !hasObs {
			if strictEOF && !found {
				expected := autarch.DFAAvailableSymbols(dfa, state)
				if len(expected) > 0 {
					return bestToken, bestRole, bestEnd, found, state, bestEndLine, bestEndCol, &LexingError[TObservation, TToken]{
						Position:    pos,
						Furthest:    furthestPos,
						Expected:    expected,
						Reason:      LexErrUnexpectedEOF,
						DFAState:    state,
						HasDFAState: true,
					}
				}
			}
			break
		}

		if stats != nil {
			stats.ObservationSteps++
		}

		nextState, err := autarch.DFAStep(dfa, state, obs, cursor)
		if err != nil || autarch.DFAIsDeadState(dfa, nextState) {
			if found {
				break
			}

			expected := autarch.DFAAvailableSymbols(dfa, state)
			return bestToken, bestRole, bestEnd, found, state, bestEndLine, bestEndCol, &LexingError[TObservation, TToken]{
				Position:    pos,
				Furthest:    furthestPos,
				Found:       obs,
				HasFound:    true,
				Expected:    expected,
				Reason:      LexErrNoTransition,
				DFAState:    state,
				HasDFAState: true,
			}
		}

		state = nextState
		furthestPos = pos + 1

		// Advance inline position tracking for the current observation.
		switch tracking.mode {
		case positionTrackingModeRuneFast:
			if obs == tracking.newlineObs {
				curLine++
				curCol = 1
			} else if obs == tracking.tabObs {
				curCol += tracking.tabWidth - ((curCol - 1) % tracking.tabWidth)
			} else {
				curCol++
			}
		case positionTrackingModeByteFast:
			if obs == tracking.newlineObs {
				curLine++
				curCol = 1
			} else {
				curCol++
			}
		default:
			if tracking.newlineDetector(obs) {
				curLine++
				curCol = 1
			} else {
				curCol = tracking.columnAdvanceFn(obs, curCol)
			}
		}

		if outcome, ok := autarch.DFAStateOutcome(dfa, state); ok && outcome.Value != nonTerminalOutcome {
			newBest, newEnd, updated := resolutionStep(
				outcome.Value.Token,
				pos+1,
				outcome.Value.Priority,
				bestToken,
				bestEnd,
				bestPriority,
			)

			if updated {
				bestToken = newBest
				bestEnd = newEnd
				bestPriority = outcome.Value.Priority
				bestRole = outcome.Value.TokenRole
				found = true
				bestEndLine = curLine
				bestEndCol = curCol
			}
		}
		pos++
	}

	if found {
		return bestToken, bestRole, bestEnd, true, state, bestEndLine, bestEndCol, nil
	}
	return bestToken, bestRole, bestEnd, false, state, startLine, startCol, nil
}

func scanCoreSlice[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	dfa *autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]],
	cursor memstruct.ArrayCursor[uint64],
	input []TObservation,
	offset int,
	resolutionStep TokenResolutionStepFn[TToken],
	strictEOF bool,
	nonTerminalOutcome TokenOutcome[TToken, TTokenRole],
	tracking positionTrackingStrategy[TObservation],
	startLine, startCol int,
	stats *LexScanStats,
) (bestToken TToken, role TTokenRole, bestEnd int, found bool, dfaState uint64, endLine, endCol int, lexErr *LexingError[TObservation, TToken]) {
	state := uint64(0)
	pos := 0

	bestPriority := 0
	var bestRole TTokenRole

	furthestPos := 0

	curLine := startLine
	curCol := startCol
	bestEndLine := startLine
	bestEndCol := startCol

	for {
		inputPos := offset + pos
		hasObs := inputPos >= 0 && inputPos < len(input)
		if !hasObs {
			if strictEOF && !found {
				expected := autarch.DFAAvailableSymbols(dfa, state)
				if len(expected) > 0 {
					return bestToken, bestRole, bestEnd, found, state, bestEndLine, bestEndCol, &LexingError[TObservation, TToken]{
						Position:    pos,
						Furthest:    furthestPos,
						Expected:    expected,
						Reason:      LexErrUnexpectedEOF,
						DFAState:    state,
						HasDFAState: true,
					}
				}
			}
			break
		}
		obs := input[inputPos]

		if stats != nil {
			stats.ObservationSteps++
		}

		nextState, err := autarch.DFAStep(dfa, state, obs, cursor)
		if err != nil || autarch.DFAIsDeadState(dfa, nextState) {
			if found {
				break
			}

			expected := autarch.DFAAvailableSymbols(dfa, state)
			return bestToken, bestRole, bestEnd, found, state, bestEndLine, bestEndCol, &LexingError[TObservation, TToken]{
				Position:    pos,
				Furthest:    furthestPos,
				Found:       obs,
				HasFound:    true,
				Expected:    expected,
				Reason:      LexErrNoTransition,
				DFAState:    state,
				HasDFAState: true,
			}
		}

		state = nextState
		furthestPos = pos + 1

		// Advance inline position tracking for the current observation.
		switch tracking.mode {
		case positionTrackingModeRuneFast:
			if obs == tracking.newlineObs {
				curLine++
				curCol = 1
			} else if obs == tracking.tabObs {
				curCol += tracking.tabWidth - ((curCol - 1) % tracking.tabWidth)
			} else {
				curCol++
			}
		case positionTrackingModeByteFast:
			if obs == tracking.newlineObs {
				curLine++
				curCol = 1
			} else {
				curCol++
			}
		default:
			if tracking.newlineDetector(obs) {
				curLine++
				curCol = 1
			} else {
				curCol = tracking.columnAdvanceFn(obs, curCol)
			}
		}

		if outcome, ok := autarch.DFAStateOutcome(dfa, state); ok && outcome.Value != nonTerminalOutcome {
			newBest, newEnd, updated := resolutionStep(
				outcome.Value.Token,
				pos+1,
				outcome.Value.Priority,
				bestToken,
				bestEnd,
				bestPriority,
			)

			if updated {
				bestToken = newBest
				bestEnd = newEnd
				bestPriority = outcome.Value.Priority
				bestRole = outcome.Value.TokenRole
				found = true
				bestEndLine = curLine
				bestEndCol = curCol
			}
		}

		pos++
	}

	if found {
		return bestToken, bestRole, bestEnd, true, state, bestEndLine, bestEndCol, nil
	}

	return bestToken, bestRole, bestEnd, false, state, startLine, startCol, nil
}

func lexerCheckEOFStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
) (Lexeme[TObservation, TToken, TTokenRole], bool) {
	if session.eof && len(session.buffer) == 0 {
		return lexerBuildEOF(
			lexer,
			session.absPos,
			session.currentLine,
			session.currentColumn,
			session.tokenNumber,
		), true
	}
	var zero Lexeme[TObservation, TToken, TTokenRole]
	return zero, false
}

func streamingEnsureAt[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
	i int,
) error {
	if i < 0 {
		return fmt.Errorf("invalid lookahead index %d", i)
	}

	// Need buffer length at least i+1, unless EOF occurs.
	for len(session.buffer) <= i && !session.eof {
		remaining := session.maxBufferedObservations - len(session.buffer)
		if remaining <= 0 {
			return fmt.Errorf(
				"streaming lexer buffer limit exceeded (max=%d); token may be longer than configured maxBufferedObservations",
				session.maxBufferedObservations,
			)
		}

		chunk := session.readChunkSize
		if chunk > remaining {
			chunk = remaining
		}

		if cap(session.refillScratch) < chunk {
			session.refillScratch = make([]TObservation, 0, chunk)
		}
		tmp := session.refillScratch[:chunk]
		n, eof, err := session.producer(tmp)
		if err != nil {
			return err
		}
		if n < 0 || n > len(tmp) {
			return fmt.Errorf("producer returned invalid n=%d (dst len=%d)", n, len(tmp))
		}

		if n > 0 {
			streamingEnsureAppendCapacity(session, n)
			session.buffer = append(session.buffer, tmp[:n]...)
		}

		if eof {
			session.eof = true
			break
		}

		// If n==0 and not eof, we loop; this is allowed by contract.
		// Caller controls whether producer blocks or yields no data temporarily.
	}

	return nil
}

func streamingEnsureAppendCapacity[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
	appendCount int,
) {
	if appendCount <= 0 {
		return
	}
	needed := len(session.buffer) + appendCount
	if needed <= cap(session.buffer) {
		return
	}
	newCap := cap(session.buffer)
	if newCap == 0 {
		newCap = min(max(appendCount, session.readChunkSize), session.maxBufferedObservations)
	}
	for newCap < needed {
		grown := newCap * 2
		if grown <= newCap {
			grown = needed
		}
		if grown > session.maxBufferedObservations {
			grown = session.maxBufferedObservations
		}
		newCap = grown
	}
	buf := make([]TObservation, len(session.buffer), newCap)
	copy(buf, session.buffer)
	session.buffer = buf
}

func streamingMaybeCompact[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole]) {
	// If buffer is empty, drop backing array.
	if len(session.buffer) == 0 {
		session.buffer = session.buffer[:0:0]
		return
	}

	// If the backing capacity is much larger than current length, compact.
	// Heuristic: cap >= 4*len and cap is meaningfully large.
	if cap(session.buffer) >= 4*len(session.buffer) && cap(session.buffer) >= 1024 {
		buf := make([]TObservation, len(session.buffer))
		copy(buf, session.buffer)
		session.buffer = buf
	}
}

func lexerPeekRangeCoreInto[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	dfa *autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]],
	resolutionStep TokenResolutionStepFn[TToken],
	cursor memstruct.ArrayCursor[uint64],
	out []Lexeme[TObservation, TToken, TTokenRole],
	ctx *scannerContext[TObservation],
	positionTracking positionTrackingStrategy[TObservation],
	startLine, startCol, startToken int,
	count int,
) ([]Lexeme[TObservation, TToken, TTokenRole], *LexingError[TObservation, TToken]) {
	line := startLine
	col := startCol
	tokenNum := startToken

	if count <= 0 {
		return out[:0], nil
	}
	if cap(out) < count {
		out = make([]Lexeme[TObservation, TToken, TTokenRole], 0, count)
	} else {
		out = out[:0]
	}

	for i := 0; i < count; i++ {
		if ctx.atEOF() {
			pos := ctx.position()
			out = append(out, lexemeEOF[TObservation, TToken, TTokenRole](
				lexer.eofToken,
				pos,
				line,
				col,
				tokenNum,
			))
			break
		}

		token, tokenRole, raw, found, currentDFAState, endLine, endCol, lexErr := scanOne(ctx, dfa, cursor, resolutionStep, lexer.scanConfig.ForceRawCopy, lexer.nonTerminalOutcome, positionTracking, line, col, lexer.scanConfig.Stats)

		// =====================================================
		// scanOne produced structured error
		// =====================================================
		if lexErr != nil {
			base := ctx.position()
			absoluteErrPos := base + lexErr.Position

			lexErr.Position = absoluteErrPos
			lexErr.Furthest = base + lexErr.Furthest
			lexErr.Formatter = lexer.formatter

			errLine, errCol := computePositionFromSlice(
				ctx.slice(0, lexErr.Position-base),
				positionTracking,
				line,
				col,
			)

			lexErr.StartLine = errLine
			lexErr.StartColumn = errCol

			relativeOffset := absoluteErrPos - base
			if obs, ok, _ := ctx.next(relativeOffset); ok {
				lexErr.Found = obs
				lexErr.HasFound = true
			}

			return nil, lexErr
		}

		// =====================================================
		// No transition at all (Immediate failure)
		// =====================================================
		if !found {
			if ctx.atEOF() {
				return out, nil
			}

			var foundObs TObservation
			if obs, ok, _ := ctx.next(0); ok {
				foundObs = obs
			}

			return nil, &LexingError[TObservation, TToken]{
				Position:    ctx.position(),
				StartLine:   line,
				StartColumn: col,
				Found:       foundObs,
				HasFound:    true,
				Expected:    autarch.DFAAvailableSymbols(dfa, currentDFAState),
				Reason:      LexErrNoTransition,
				DFAState:    currentDFAState,
				HasDFAState: true,
				Formatter:   lexer.formatter,
			}
		}

		// =====================================================
		// Normal token production
		// =====================================================
		start := ctx.position()

		lex := lexemeBuild(
			lexer.formatter,
			token,
			raw,
			start,
			start+len(raw),
			line,
			col,
			endLine,
			endCol,
			tokenNum,
			tokenRole,
		)

		out = append(out, lex)

		ctx.advanceRaw(raw)

		line = endLine
		col = endCol
		tokenNum++
	}

	return out, nil
}

func scanOne[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	ctx *scannerContext[TObservation],
	dfa *autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]],
	cursor memstruct.ArrayCursor[uint64],
	resolutionStep TokenResolutionStepFn[TToken],
	forceRawCopy bool,
	nonTerminalOutcome TokenOutcome[TToken, TTokenRole],
	tracking positionTrackingStrategy[TObservation],
	startLine, startCol int,
	stats *LexScanStats,
) (token TToken, tokenRole TTokenRole, raw []TObservation, found bool, state uint64, endLine, endCol int, err *LexingError[TObservation, TToken]) {
	var (
		role   TTokenRole
		endRel int
		lexErr *LexingError[TObservation, TToken]
		eLine  int
		eCol   int
	)
	if ctx.directInput != nil {
		input, offset := ctx.directInput()
		token, role, endRel, found, state, eLine, eCol, lexErr = scanCoreSlice(dfa, cursor, input, offset, resolutionStep, false, nonTerminalOutcome, tracking, startLine, startCol, stats)
	} else {
		token, role, endRel, found, state, eLine, eCol, lexErr = scanCoreStreaming(dfa, cursor, ctx.next, resolutionStep, false, nonTerminalOutcome, tracking, startLine, startCol, stats)
	}

	if lexErr != nil {
		return token, role, nil, found, state, startLine, startCol, lexErr
	}

	if !found {
		return token, role, nil, false, state, startLine, startCol, nil
	}

	if forceRawCopy || ctx.rawRequiresCopy {
		raw = copyRaw(ctx.slice(0, endRel))
	} else {
		raw = ctx.slice(0, endRel)
	}
	return token, role, raw, true, state, eLine, eCol, nil
}

type scannerContext[TObservation cmp.Ordered] struct {
	next            func(int) (TObservation, bool, error)
	slice           func(start, end int) []TObservation
	directInput     func() ([]TObservation, int)
	remaining       func() int
	atEOF           func() bool
	position        func() int
	advanceRaw      func(raw []TObservation)
	rawRequiresCopy bool
}

type sliceScannerLiveContext[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable] struct {
	session *LexerSession[TObservation, TState, TToken, TTokenRole]
	ctx     scannerContext[TObservation]
}

type sliceScannerSimulatedContext[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable] struct {
	session *LexerSession[TObservation, TState, TToken, TTokenRole]
	pos     int
	ctx     scannerContext[TObservation]
}

type streamingScannerLiveContext[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable] struct {
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole]
	ctx     scannerContext[TObservation]
}

type streamingScannerSimulatedContext[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable] struct {
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole]
	buffer  []TObservation
	absPos  int
	ctx     scannerContext[TObservation]
}

func sliceScannerLiveContextInit[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	scanner *sliceScannerLiveContext[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) {
	scanner.session = session
	scanner.ctx = scannerContext[TObservation]{
		next: func(i int) (TObservation, bool, error) {
			pos := scanner.session.position + i
			if pos >= len(scanner.session.input) {
				var zero TObservation
				return zero, false, nil
			}
			return scanner.session.input[pos], true, nil
		},
		slice: func(start, end int) []TObservation {
			base := scanner.session.position
			return scanner.session.input[base+start : base+end]
		},
		directInput: func() ([]TObservation, int) {
			return scanner.session.input, scanner.session.position
		},
		remaining: func() int {
			return len(scanner.session.input) - scanner.session.position
		},
		atEOF: func() bool {
			return scanner.session.position >= len(scanner.session.input)
		},
		position: func() int {
			return scanner.session.position
		},
		advanceRaw: func(raw []TObservation) {
			scanner.session.position += len(raw)
		},
		rawRequiresCopy: false,
	}
}

func sliceScannerSimulatedContextInit[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	scanner *sliceScannerSimulatedContext[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) {
	scanner.session = session
	scanner.ctx = scannerContext[TObservation]{
		next: func(i int) (TObservation, bool, error) {
			p := scanner.pos + i
			if p >= len(scanner.session.input) {
				var zero TObservation
				return zero, false, nil
			}
			return scanner.session.input[p], true, nil
		},
		slice: func(start, end int) []TObservation {
			return scanner.session.input[scanner.pos+start : scanner.pos+end]
		},
		directInput: func() ([]TObservation, int) {
			return scanner.session.input, scanner.pos
		},
		remaining: func() int {
			return len(scanner.session.input) - scanner.pos
		},
		atEOF: func() bool {
			return scanner.pos >= len(scanner.session.input)
		},
		position: func() int {
			return scanner.pos
		},
		advanceRaw: func(raw []TObservation) {
			scanner.pos += len(raw)
		},
		rawRequiresCopy: false,
	}
}

func sliceScannerSimulatedContextReset[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	scanner *sliceScannerSimulatedContext[TObservation, TState, TToken, TTokenRole],
) {
	scanner.pos = scanner.session.position
}

func streamingScannerLiveContextInit[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	scanner *streamingScannerLiveContext[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
) {
	scanner.session = session
	scanner.ctx = scannerContext[TObservation]{
		next: func(i int) (TObservation, bool, error) {
			if err := streamingEnsureAt(scanner.session, i); err != nil {
				var zero TObservation
				return zero, false, err
			}
			if i >= len(scanner.session.buffer) {
				var zero TObservation
				return zero, false, nil
			}
			return scanner.session.buffer[i], true, nil
		},
		slice: func(start, end int) []TObservation {
			return scanner.session.buffer[start:end]
		},
		remaining: func() int {
			return -1
		},
		atEOF: func() bool {
			return scanner.session.eof && len(scanner.session.buffer) == 0
		},
		position: func() int {
			return scanner.session.absPos
		},
		advanceRaw: func(raw []TObservation) {
			n := len(raw)
			scanner.session.absPos += n
			scanner.session.buffer = scanner.session.buffer[n:]
			streamingMaybeCompact(scanner.session)
		},
		rawRequiresCopy: true,
	}
}

func streamingScannerSimulatedContextInit[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	scanner *streamingScannerSimulatedContext[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
) {
	scanner.session = session
	scanner.ctx = scannerContext[TObservation]{
		next: func(i int) (TObservation, bool, error) {
			if i >= len(scanner.buffer) {
				if scanner.session.eof && len(scanner.buffer) == 0 {
					var zero TObservation
					return zero, false, nil
				}
				if err := streamingEnsureAt(scanner.session, i); err != nil {
					var zero TObservation
					return zero, false, err
				}
				scanner.buffer = scanner.session.buffer
			}
			if i >= len(scanner.buffer) {
				var zero TObservation
				return zero, false, nil
			}
			return scanner.buffer[i], true, nil
		},
		slice: func(start, end int) []TObservation {
			return scanner.buffer[start:end]
		},
		remaining: func() int {
			return -1
		},
		atEOF: func() bool {
			return scanner.session.eof && len(scanner.buffer) == 0
		},
		position: func() int {
			return scanner.absPos
		},
		advanceRaw: func(raw []TObservation) {
			n := len(raw)
			scanner.absPos += n
			scanner.buffer = scanner.buffer[n:]
		},
		rawRequiresCopy: true,
	}
}

func streamingScannerSimulatedContextReset[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	scanner *streamingScannerSimulatedContext[TObservation, TState, TToken, TTokenRole],
) {
	scanner.buffer = scanner.session.buffer
	scanner.absPos = scanner.session.absPos
}

func lexerSessionScannerContextsInit[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) {
	sliceScannerLiveContextInit(&session.liveScanner, session)
	sliceScannerSimulatedContextInit(&session.simulatedScanner, session)
}

func streamingLexerSessionScannerContextsInit[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
) {
	streamingScannerLiveContextInit(&session.liveScanner, session)
	streamingScannerSimulatedContextInit(&session.simulatedScanner, session)
}

func scannerFromSlice[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) *scannerContext[TObservation] {
	if session.liveScanner.ctx.next == nil {
		lexerSessionScannerContextsInit(session)
	}
	return &session.liveScanner.ctx
}

func scannerFromStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
) *scannerContext[TObservation] {
	if session.liveScanner.ctx.next == nil {
		streamingLexerSessionScannerContextsInit(session)
	}
	return &session.liveScanner.ctx
}

func scannerFromSliceSimulated[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) *scannerContext[TObservation] {
	if session.simulatedScanner.ctx.next == nil {
		lexerSessionScannerContextsInit(session)
	}
	sliceScannerSimulatedContextReset(&session.simulatedScanner)
	return &session.simulatedScanner.ctx
}

func scannerFromStreamingSimulated[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
) *scannerContext[TObservation] {
	if session.simulatedScanner.ctx.next == nil {
		streamingLexerSessionScannerContextsInit(session)
	}
	streamingScannerSimulatedContextReset(&session.simulatedScanner)
	return &session.simulatedScanner.ctx
}

func copyRaw[T any](src []T) []T {
	dst := make([]T, len(src))
	copy(dst, src)
	return dst
}
