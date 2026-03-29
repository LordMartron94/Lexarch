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

func lexerBuildEOFSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) Lexeme[TObservation, TToken, TTokenRole] {
	return lexerBuildEOF(lexer, session.position, session.currentLine, session.currentColumn, session.tokenNumber)
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

type scanCoreResult[TObservation cmp.Ordered, TToken, TTokenRole comparable] struct {
	bestToken               TToken
	bestRole                TTokenRole
	bestEnd                 int
	found                   bool
	dfaState                uint64
	bestEndLine, bestEndCol int
	lexErr                  *LexingError[TObservation, TToken]
}

func scanCoreResultAssign[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	out *scanCoreResult[TObservation, TToken, TTokenRole],
	bestToken TToken,
	bestRole TTokenRole,
	bestEnd int,
	found bool,
	dfaState uint64,
	endLine, endCol int,
	lexErr *LexingError[TObservation, TToken],
) {
	out.bestToken = bestToken
	out.bestRole = bestRole
	out.bestEnd = bestEnd
	out.found = found
	out.dfaState = dfaState
	out.bestEndLine = endLine
	out.bestEndCol = endCol
	out.lexErr = lexErr
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
	out *scanCoreResult[TObservation, TToken, TTokenRole],
) {
	state := uint64(0)

	bestPriority := 0
	var bestRole TTokenRole
	var bestToken TToken

	furthestPos := 0

	curLine := startLine
	curCol := startCol
	bestEndLine := startLine
	bestEndCol := startCol
	bestEnd := 0

	scanWindow := input[offset:]

	found := false
	for pos, obs := range scanWindow {
		if stats != nil {
			stats.ObservationSteps++
		}

		nextState, err := autarch.DFAStep(dfa, state, obs, cursor)
		if err != nil || autarch.DFAIsDeadState(dfa, nextState) {
			if found {
				break
			}

			expected := autarch.DFAAvailableSymbols(dfa, state)
			scanCoreResultAssign(out, bestToken, bestRole, bestEnd, found, state, bestEndLine, bestEndCol, &LexingError[TObservation, TToken]{
				Position:    pos,
				Furthest:    furthestPos,
				Found:       obs,
				HasFound:    true,
				Expected:    expected,
				Reason:      LexErrNoTransition,
				DFAState:    state,
				HasDFAState: true,
			})
			return
		}

		state = nextState
		furthestPos = pos + 1

		// Advance inline position tracking for the current observation.
		switch tracking.mode {
		case positionTrackingModeRuneFast:
			switch obs {
			case tracking.newlineObs:
				curLine++
				curCol = 1
			case tracking.tabObs:
				curCol += tracking.tabWidth - ((curCol - 1) % tracking.tabWidth)
			default:
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
	}

	if strictEOF && !found {
		expected := autarch.DFAAvailableSymbols(dfa, state)
		if len(expected) > 0 {
			scanCoreResultAssign(out, bestToken, bestRole, bestEnd, found, state, bestEndLine, bestEndCol, &LexingError[TObservation, TToken]{
				Position:    len(scanWindow),
				Furthest:    furthestPos,
				Expected:    expected,
				Reason:      LexErrUnexpectedEOF,
				DFAState:    state,
				HasDFAState: true,
			})
			return
		}
	}

	if found {
		scanCoreResultAssign(out, bestToken, bestRole, bestEnd, true, state, bestEndLine, bestEndCol, nil)
		return
	}
	scanCoreResultAssign(out, bestToken, bestRole, bestEnd, false, state, startLine, startCol, nil)
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

	var scanCore scanCoreResult[TObservation, TToken, TTokenRole]
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

		token, tokenRole, raw, found, currentDFAState, endLine, endCol, lexErr := scanOne(ctx, dfa, cursor, resolutionStep, lexer.scanConfig.ForceRawCopy, lexer.nonTerminalOutcome, positionTracking, line, col, lexer.scanConfig.Stats, &scanCore)

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
	coreOut *scanCoreResult[TObservation, TToken, TTokenRole],
) (token TToken, tokenRole TTokenRole, raw []TObservation, found bool, state uint64, endLine, endCol int, err *LexingError[TObservation, TToken]) {
	input, offset := ctx.directInput()
	scanCoreSlice(dfa, cursor, input, offset, resolutionStep, false, nonTerminalOutcome, tracking, startLine, startCol, stats, coreOut)

	token = coreOut.bestToken
	tokenRole = coreOut.bestRole
	endRel := coreOut.bestEnd
	found = coreOut.found
	state = coreOut.dfaState
	endLine = coreOut.bestEndLine
	endCol = coreOut.bestEndCol
	err = coreOut.lexErr

	if err != nil {
		return token, tokenRole, nil, found, state, startLine, startCol, err
	}

	if !found {
		return token, tokenRole, nil, false, state, startLine, startCol, nil
	}

	if forceRawCopy || ctx.rawRequiresCopy {
		raw = copyRaw(ctx.slice(0, endRel))
	} else {
		raw = ctx.slice(0, endRel)
	}
	return token, tokenRole, raw, true, state, endLine, endCol, nil
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

type sliceScannerSimulatedContext[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable] struct {
	session *LexerSession[TObservation, TState, TToken, TTokenRole]
	pos     int
	ctx     scannerContext[TObservation]
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

func lexerSessionScannerContextsInit[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) {
	sliceScannerSimulatedContextInit(&session.simulatedScanner, session)
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

func copyRaw[T any](src []T) []T {
	dst := make([]T, len(src))
	copy(dst, src)
	return dst
}
