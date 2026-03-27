package lexarch

import (
	"autarch"
	"autarch/pattern"
	"cmp"
	"fmt"
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

func streamingNextFn[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
) func(int) (TObservation, bool, error) {

	return func(i int) (TObservation, bool, error) {
		if err := streamingEnsureAt(session, i); err != nil {
			var zero TObservation
			return zero, false, err
		}
		if i >= len(session.buffer) {
			var zero TObservation
			return zero, false, nil
		}
		return session.buffer[i], true, nil
	}
}

func computePositionFromSlice[TObservation cmp.Ordered](
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

func scanCore[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	dfa *autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]],
	nextObservation func(int) (obs TObservation, ok bool, err error),
	resolutionStep TokenResolutionStepFn[TToken],
	strictEOF bool,
	nonTerminalOutcome TokenOutcome[TToken, TTokenRole],
) (bestToken TToken, role TTokenRole, bestEnd int, found bool, dfaState uint64, lexErr *LexingError[TObservation, TToken]) {

	cursor := autarch.DFACursorGet(dfa)

	state := uint64(0)
	pos := 0

	bestPriority := 0
	var bestRole TTokenRole

	// Diagnostic tracking
	furthestPos := 0

	for {
		obs, hasObs, err := nextObservation(pos)
		if err != nil {
			return bestToken, bestRole, bestEnd, found, state, &LexingError[TObservation, TToken]{
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
					return bestToken, bestRole, bestEnd, found, state, &LexingError[TObservation, TToken]{
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

		nextState, err := autarch.DFAStep(dfa, state, obs, cursor)

		if err != nil || autarch.DFAIsDeadState(dfa, nextState) {
			if found {
				break
			}

			expected := autarch.DFAAvailableSymbols(dfa, state)

			return bestToken, bestRole, bestEnd, found, state, &LexingError[TObservation, TToken]{
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

		// update furthest progress
		furthestPos = pos + 1

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
			}
		}

		pos++
	}

	// If we matched something successfully, return it
	if found {
		return bestToken, bestRole, bestEnd, true, state, nil
	}

	return bestToken, bestRole, bestEnd, false, state, nil
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
	out []Lexeme[TObservation, TToken, TTokenRole],
	ctx scannerContext[TObservation],
	lexerState TState,
	newlineDetector NewlineDetector[TObservation],
	columnAdvanceFn ColumnAdvanceFn[TObservation],
	startLine, startCol, startToken int,
	count int,
) ([]Lexeme[TObservation, TToken, TTokenRole], *LexingError[TObservation, TToken]) {
	dfa, resolutionStep, err := lexerGetDFAAndResolution(lexer, lexerState)
	if err != nil {
		return nil, &LexingError[TObservation, TToken]{
			Position:    ctx.position(),
			StartLine:   startLine,
			StartColumn: startCol,
			Reason:      LexErrNoTransition,
			Formatter:   lexer.formatter,
		}
	}

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

		token, tokenRole, raw, found, currentDFAState, lexErr := scanOne(ctx, dfa, resolutionStep, lexer.scanConfig.ForceRawCopy, lexer.nonTerminalOutcome)

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
				newlineDetector,
				columnAdvanceFn,
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

		endLine, endCol := computePositionFromSlice(
			raw,
			newlineDetector,
			columnAdvanceFn,
			line,
			col,
		)

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
	ctx scannerContext[TObservation],
	dfa *autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]],
	resolutionStep TokenResolutionStepFn[TToken],
	forceRawCopy bool,
	nonTerminalOutcome TokenOutcome[TToken, TTokenRole],
) (token TToken, tokenRole TTokenRole, raw []TObservation, found bool, state uint64, err *LexingError[TObservation, TToken]) {

	token, role, endRel, found, state, lexErr := scanCore(dfa, ctx.next, resolutionStep, false, nonTerminalOutcome)

	if lexErr != nil {
		return token, role, nil, found, state, lexErr
	}

	if !found {
		return token, role, nil, false, state, nil
	}

	if forceRawCopy || ctx.rawRequiresCopy {
		raw = copyRaw(ctx.slice(0, endRel))
	} else {
		raw = ctx.slice(0, endRel)
	}
	return token, role, raw, true, state, nil
}

type scannerContext[TObservation cmp.Ordered] struct {
	next            func(int) (TObservation, bool, error)
	slice           func(start, end int) []TObservation
	remaining       func() int
	atEOF           func() bool
	position        func() int
	advanceRaw      func(raw []TObservation)
	rawRequiresCopy bool
}

func scannerFromSlice[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) scannerContext[TObservation] {

	return scannerContext[TObservation]{
		next: func(i int) (TObservation, bool, error) {
			pos := session.position + i
			if pos >= len(session.input) {
				var zero TObservation
				return zero, false, nil
			}
			return session.input[pos], true, nil
		},

		slice: func(start, end int) []TObservation {
			base := session.position
			return session.input[base+start : base+end]
		},
		remaining: func() int {
			return len(session.input) - session.position
		},

		atEOF: func() bool {
			return session.position >= len(session.input)
		},

		position: func() int {
			return session.position
		},

		advanceRaw: func(raw []TObservation) {
			session.position += len(raw)
		},
		rawRequiresCopy: false,
	}
}

func scannerFromStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
) scannerContext[TObservation] {

	next := streamingNextFn(session)

	return scannerContext[TObservation]{
		next: next,

		slice: func(start, end int) []TObservation {
			return session.buffer[start:end]
		},
		remaining: func() int {
			return -1
		},

		atEOF: func() bool {
			return session.eof && len(session.buffer) == 0
		},

		position: func() int {
			return session.absPos
		},

		advanceRaw: func(raw []TObservation) {
			n := len(raw)
			session.absPos += n
			session.buffer = session.buffer[n:]
			streamingMaybeCompact(session)
		},
		rawRequiresCopy: true,
	}
}

func scannerFromSliceSimulated[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) scannerContext[TObservation] {

	pos := session.position

	return scannerContext[TObservation]{
		next: func(i int) (TObservation, bool, error) {
			p := pos + i
			if p >= len(session.input) {
				var zero TObservation
				return zero, false, nil
			}
			return session.input[p], true, nil
		},

		slice: func(start, end int) []TObservation {
			return session.input[pos+start : pos+end]
		},
		remaining: func() int {
			return len(session.input) - pos
		},

		atEOF: func() bool {
			return pos >= len(session.input)
		},

		position: func() int {
			return pos
		},

		advanceRaw: func(raw []TObservation) {
			pos += len(raw)
		},
		rawRequiresCopy: false,
	}
}

func scannerFromStreamingSimulated[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
) scannerContext[TObservation] {

	buffer := session.buffer
	absPos := session.absPos
	atEOF := func() bool {
		return session.eof && len(buffer) == 0
	}

	next := func(i int) (TObservation, bool, error) {
		if i >= len(buffer) {
			if atEOF() {
				var zero TObservation
				return zero, false, nil
			}
			if err := streamingEnsureAt(session, i); err != nil {
				var zero TObservation
				return zero, false, err
			}
			buffer = session.buffer
		}
		if i >= len(buffer) {
			var zero TObservation
			return zero, false, nil
		}
		return buffer[i], true, nil
	}

	return scannerContext[TObservation]{
		next: next,

		slice: func(start, end int) []TObservation {
			return buffer[start:end]
		},
		remaining: func() int {
			return -1
		},

		atEOF: func() bool {
			return atEOF() && len(buffer) == 0
		},

		position: func() int {
			return absPos
		},

		advanceRaw: func(raw []TObservation) {
			n := len(raw)
			absPos += n
			buffer = buffer[n:]
		},
		rawRequiresCopy: true,
	}
}

func copyRaw[T any](src []T) []T {
	dst := make([]T, len(src))
	copy(dst, src)
	return dst
}
