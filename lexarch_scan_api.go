package lexarch

import "cmp"

func lexerPeekRangeWithContext[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	out []Lexeme[TObservation, TToken, TTokenRole],
	ctx scannerContext[TObservation],
	lexerState TState,
	newlineDetector NewlineDetector[TObservation],
	columnAdvanceFn ColumnAdvanceFn[TObservation],
	startLine int,
	startColumn int,
	startToken int,
	count int,
	setLastError func(*LexingError[TObservation, TToken]),
) []Lexeme[TObservation, TToken, TTokenRole] {
	lexemes, err := lexerPeekRangeCoreInto(
		lexer,
		out,
		ctx,
		lexerState,
		newlineDetector,
		columnAdvanceFn,
		startLine,
		startColumn,
		startToken,
		count,
	)
	if err != nil {
		err.Formatter = lexer.formatter
		setLastError(err)
	}
	return lexemes
}

func lexerSetMismatchErrorAndEOFSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
	lex Lexeme[TObservation, TToken, TTokenRole],
) Lexeme[TObservation, TToken, TTokenRole] {
	session.lastError = &LexingError[TObservation, TToken]{
		Position:    lex.Start,
		StartLine:   lex.StartLine,
		StartColumn: lex.StartColumn,
		Reason:      LexErrNoTransition,
		Formatter:   lexer.formatter,
	}
	return lexerBuildEOFSession(lexer, session)
}

func lexerSetMismatchErrorAndEOFStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
	lex Lexeme[TObservation, TToken, TTokenRole],
) Lexeme[TObservation, TToken, TTokenRole] {
	session.lastError = &LexingError[TObservation, TToken]{
		Position:    lex.Start,
		StartLine:   lex.StartLine,
		StartColumn: lex.StartColumn,
		Reason:      LexErrNoTransition,
		Formatter:   lexer.formatter,
	}
	return lexerBuildEOFSessionStream(lexer, session)
}

/*
LexerConsume recognizes and consumes the next token.

If an error occurs or the session already has an error, it returns an EOF lexeme
and sets session.lastError.

Time complexity: O(length of matched token)
*/
func LexerConsume[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) Lexeme[TObservation, TToken, TTokenRole] {
	switch lexer.scanConfig.Mode {
	case ScanModePreTokenizeAll:
		return lexerConsumeFromPreTokenizedSession(lexer, session)
	case ScanModeCircularTokenBuffer:
		return lexerConsumeFromCircularWindowSession(lexer, session)
	default:
	}

	session.begin()
	defer session.end()

	if session.lastError != nil {
		return lexerBuildEOFSession(lexer, session)
	}

	if eofLexeme, atEOF := lexerCheckEOF(lexer, session); atEOF {
		return eofLexeme
	}

	dfa, resolutionStep, err := lexerGetDFAAndResolution(lexer, session.currentState)
	if err != nil {
		lexErr := &LexingError[TObservation, TToken]{
			Position:    session.position,
			StartLine:   session.currentLine,
			StartColumn: session.currentColumn,
			Reason:      LexErrNoTransition,
			Formatter:   lexer.formatter,
		}
		session.lastError = lexErr
		return lexerBuildEOFSession(lexer, session)
	}

	next := func(i int) (TObservation, bool, error) {
		pos := session.position + i
		if pos >= len(session.input) {
			var zero TObservation
			return zero, false, nil
		}
		return session.input[pos], true, nil
	}

	token, tokenRole, endRel, found, _, lexErr := scanCore(dfa, next, resolutionStep, true, lexer.nonTerminalOutcome)
	if lexErr != nil {
		lexErr.Position = session.position + lexErr.Position
		lexErr.Furthest = session.position + lexErr.Furthest
		lexErr.Formatter = lexer.formatter

		line, col := computePositionFromSlice(
			session.input[session.position:lexErr.Position],
			session.newlineDetector,
			session.columnAdvanceFn,
			session.currentLine,
			session.currentColumn,
		)
		lexErr.StartLine = line
		lexErr.StartColumn = col

		session.lastError = lexErr
		return lexerBuildEOFSession(lexer, session)
	}

	if !found {
		return lexerBuildEOFSession(lexer, session)
	}

	start := session.position
	end := session.position + endRel
	raw := copyRaw(session.input[start:end])

	endLine, endCol := computePositionFromSlice(
		raw,
		session.newlineDetector,
		session.columnAdvanceFn,
		session.currentLine,
		session.currentColumn,
	)

	lex := lexemeBuild(
		lexer.formatter,
		token,
		raw,
		start,
		end,
		session.currentLine,
		session.currentColumn,
		endLine,
		endCol,
		session.tokenNumber,
		tokenRole,
	)

	session.tokenNumber++
	session.position = end
	session.currentLine = lex.EndLine
	session.currentColumn = lex.EndColumn

	return lex
}

/*
LexerConsumeRange consumes and returns up to `count` tokens.

If an error occurs, the returned slice contains tokens found up to the error point.
The session state is updated to reflect the last successfully consumed token.
*/
func LexerConsumeRange[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	switch lexer.scanConfig.Mode {
	case ScanModePreTokenizeAll:
		return lexerConsumeRangeFromPreTokenizedSession(lexer, session, count)
	case ScanModeCircularTokenBuffer:
		return lexerConsumeRangeFromCircularWindowSession(lexer, session, count)
	default:
	}

	session.begin()
	defer session.end()

	if count <= 0 || session.lastError != nil {
		return nil
	}

	ctx := scannerFromSlice(session)
	coreOut := lexemeScratchCoreRangeReset(&session.scanCache, count)
	lexemes := lexerPeekRangeWithContext(
		lexer, coreOut, ctx, session.currentState, session.newlineDetector, session.columnAdvanceFn,
		session.currentLine, session.currentColumn, session.tokenNumber, count,
		func(err *LexingError[TObservation, TToken]) { session.lastError = err },
	)

	if len(lexemes) > 0 {
		last := lexemes[len(lexemes)-1]
		session.currentLine = last.EndLine
		session.currentColumn = last.EndColumn
		session.tokenNumber += len(lexemes)
		session.position = last.End
	}

	return lexemes
}

/*
LexerPeek returns the n-th token ahead without consuming input.
*/
func LexerPeek[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
	n int,
) Lexeme[TObservation, TToken, TTokenRole] {
	switch lexer.scanConfig.Mode {
	case ScanModePreTokenizeAll:
		return lexerPeekFromPreTokenizedSession(lexer, session, n)
	case ScanModeCircularTokenBuffer:
		return lexerPeekFromCircularWindowSession(lexer, session, n)
	default:
	}

	session.begin()
	defer session.end()

	if n < 0 || session.lastError != nil {
		return lexerBuildEOFSession(lexer, session)
	}

	ctx := scannerFromSliceSimulated(session)
	coreOut := lexemeScratchCoreRangeReset(&session.scanCache, n+1)
	lexemes := lexerPeekRangeWithContext(
		lexer, coreOut, ctx, session.currentState, session.newlineDetector, session.columnAdvanceFn,
		session.currentLine, session.currentColumn, session.tokenNumber, n+1,
		func(err *LexingError[TObservation, TToken]) { session.lastError = err },
	)
	if session.lastError != nil {
		return lexerBuildEOFSession(lexer, session)
	}

	if len(lexemes) <= n {
		return lexerBuildEOFSession(lexer, session)
	}

	return lexemes[n]
}

/*
LexerPeekRange returns up to `count` upcoming tokens without consuming input.
*/
func LexerPeekRange[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	switch lexer.scanConfig.Mode {
	case ScanModePreTokenizeAll:
		return lexerPeekRangeFromPreTokenizedSession(lexer, session, count)
	case ScanModeCircularTokenBuffer:
		return lexerPeekRangeFromCircularWindowSession(lexer, session, count)
	default:
	}

	session.begin()
	defer session.end()

	if count <= 0 || session.lastError != nil {
		return nil
	}

	ctx := scannerFromSliceSimulated(session)
	coreOut := lexemeScratchCoreRangeReset(&session.scanCache, count)
	return lexerPeekRangeWithContext(
		lexer, coreOut, ctx, session.currentState, session.newlineDetector, session.columnAdvanceFn,
		session.currentLine, session.currentColumn, session.tokenNumber, count,
		func(err *LexingError[TObservation, TToken]) { session.lastError = err },
	)
}

/*
LexerAssertConsume consumes the next token and verifies it matches the expected type.
*/
func LexerAssertConsume[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
	expected TToken,
) Lexeme[TObservation, TToken, TTokenRole] {
	lex := LexerConsume(lexer, session)

	if session.lastError == nil && lex.Token != expected {
		return lexerSetMismatchErrorAndEOFSession(lexer, session, lex)
	}

	return lex
}

/*
LexerAssertPeek peeks at the n-th token and verifies it matches the expected type.
*/
func LexerAssertPeek[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
	expected TToken,
	n int,
) Lexeme[TObservation, TToken, TTokenRole] {
	lex := LexerPeek(lexer, session, n)

	if session.lastError == nil && lex.Token != expected {
		return lexerSetMismatchErrorAndEOFSession(lexer, session, lex)
	}

	return lex
}

/*
LexerConsumeStreaming consumes the next token from a streaming session.
*/
func LexerConsumeStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
) Lexeme[TObservation, TToken, TTokenRole] {
	switch lexer.scanConfig.Mode {
	case ScanModePreTokenizeAll:
		return lexerConsumeFromPreTokenizedStreaming(lexer, session)
	case ScanModeCircularTokenBuffer:
		return lexerConsumeFromCircularWindowStreaming(lexer, session)
	default:
	}

	session.begin()
	defer session.end()

	if session.lastError != nil {
		return lexerBuildEOFSessionStream(lexer, session)
	}

	if eofLexeme, atEOF := lexerCheckEOFStreaming(lexer, session); atEOF {
		return eofLexeme
	}

	dfa, resolutionStep, err := lexerGetDFAAndResolution(lexer, session.currentState)
	if err != nil {
		session.lastError = &LexingError[TObservation, TToken]{
			Position:    session.absPos,
			StartLine:   session.currentLine,
			StartColumn: session.currentColumn,
			Reason:      LexErrNoTransition,
			Formatter:   lexer.formatter,
		}
		return lexerBuildEOFSessionStream(lexer, session)
	}

	next := streamingNextFn(session)
	token, role, endRel, found, _, lexErr := scanCore(dfa, next, resolutionStep, true, lexer.nonTerminalOutcome)

	if lexErr != nil {
		lexErr.Formatter = lexer.formatter
		session.lastError = lexErr
		return lexerBuildEOFSessionStream(lexer, session)
	}

	if !found {
		return lexerBuildEOFSessionStream(lexer, session)
	}

	var raw []TObservation
	if endRel > 0 && endRel <= len(session.buffer) {
		raw = copyRaw(session.buffer[:endRel])
	}

	endLine, endCol := computePositionFromSlice(raw, session.newlineDetector, session.columnAdvanceFn, session.currentLine, session.currentColumn)

	lex := lexemeBuild(lexer.formatter, token, raw, session.absPos, session.absPos+endRel, session.currentLine, session.currentColumn, endLine, endCol, session.tokenNumber, role)

	session.tokenNumber++
	session.absPos += endRel
	session.currentLine = lex.EndLine
	session.currentColumn = lex.EndColumn
	session.buffer = session.buffer[endRel:]

	streamingMaybeCompact(session)
	return lex
}

/*
LexerConsumeRangeStreaming consumes and returns up to `count` tokens from a streaming session.
*/
func LexerConsumeRangeStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	switch lexer.scanConfig.Mode {
	case ScanModePreTokenizeAll:
		return lexerConsumeRangeFromPreTokenizedStreaming(lexer, session, count)
	case ScanModeCircularTokenBuffer:
		return lexerConsumeRangeFromCircularWindowStreaming(lexer, session, count)
	default:
	}

	session.begin()
	defer session.end()

	if count <= 0 || session.lastError != nil {
		return nil
	}

	ctx := scannerFromStreaming(session)
	coreOut := lexemeScratchCoreRangeReset(&session.scanCache, count)
	lexemes := lexerPeekRangeWithContext(
		lexer, coreOut, ctx, session.currentState, session.newlineDetector, session.columnAdvanceFn,
		session.currentLine, session.currentColumn, session.tokenNumber, count,
		func(err *LexingError[TObservation, TToken]) { session.lastError = err },
	)

	if len(lexemes) > 0 {
		last := lexemes[len(lexemes)-1]
		session.currentLine = last.EndLine
		session.currentColumn = last.EndColumn
		session.tokenNumber += len(lexemes)
	}

	return lexemes
}

/*
LexerPeekStreaming performs lookahead on a streaming session.
*/
func LexerPeekStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
	n int,
) Lexeme[TObservation, TToken, TTokenRole] {
	switch lexer.scanConfig.Mode {
	case ScanModePreTokenizeAll:
		return lexerPeekFromPreTokenizedStreaming(lexer, session, n)
	case ScanModeCircularTokenBuffer:
		return lexerPeekFromCircularWindowStreaming(lexer, session, n)
	default:
	}

	if n < 0 || session.lastError != nil {
		return lexerBuildEOFSessionStream(lexer, session)
	}

	lexemes := LexerPeekRangeStreaming(lexer, session, n+1)
	if n >= len(lexemes) {
		return lexerBuildEOFSessionStream(lexer, session)
	}

	return lexemes[n]
}

/*
LexerPeekRangeStreaming returns up to `count` upcoming tokens without consuming input.
*/
func LexerPeekRangeStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	switch lexer.scanConfig.Mode {
	case ScanModePreTokenizeAll:
		return lexerPeekRangeFromPreTokenizedStreaming(lexer, session, count)
	case ScanModeCircularTokenBuffer:
		return lexerPeekRangeFromCircularWindowStreaming(lexer, session, count)
	default:
	}

	session.begin()
	defer session.end()

	if count <= 0 || session.lastError != nil {
		return nil
	}

	ctx := scannerFromStreamingSimulated(session)
	coreOut := lexemeScratchCoreRangeReset(&session.scanCache, count)
	return lexerPeekRangeWithContext(
		lexer, coreOut, ctx, session.currentState, session.newlineDetector, session.columnAdvanceFn,
		session.currentLine, session.currentColumn, session.tokenNumber, count,
		func(err *LexingError[TObservation, TToken]) { session.lastError = err },
	)
}

/*
LexerAssertConsumeStreaming consumes and verifies the next token from a streaming session.
*/
func LexerAssertConsumeStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
	expected TToken,
) Lexeme[TObservation, TToken, TTokenRole] {
	lex := LexerConsumeStreaming(lexer, session)

	if session.lastError == nil && lex.Token != expected {
		return lexerSetMismatchErrorAndEOFStreaming(lexer, session, lex)
	}

	return lex
}

/*
LexerAssertPeekStreaming peeks and verifies the next token from a streaming session.
*/
func LexerAssertPeekStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
	expected TToken,
	n int,
) Lexeme[TObservation, TToken, TTokenRole] {
	lex := LexerPeekStreaming(lexer, session, n)

	if session.lastError == nil && lex.Token != expected {
		return lexerSetMismatchErrorAndEOFStreaming(lexer, session, lex)
	}

	return lex
}
