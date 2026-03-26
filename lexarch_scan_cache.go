package lexarch

import "cmp"

func lexerCollectAllFromContext[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	ctx scannerContext[TObservation],
	lexerState TState,
	newlineDetector NewlineDetector[TObservation],
	columnAdvanceFn ColumnAdvanceFn[TObservation],
	startLine, startCol, startToken int,
) ([]Lexeme[TObservation, TToken, TTokenRole], *LexingError[TObservation, TToken]) {
	initialCap := lexerCollectAllInitialCapacity(ctx.remaining())
	out := make([]Lexeme[TObservation, TToken, TTokenRole], 0, initialCap)
	for {
		chunk, err := lexerPeekRangeCore(
			lexer,
			ctx,
			lexerState,
			newlineDetector,
			columnAdvanceFn,
			startLine,
			startCol,
			startToken+len(out),
			256,
		)
		if err != nil {
			return nil, err
		}
		if len(chunk) == 0 {
			return out, nil
		}
		out = lexemeEnsureCapacity(out, len(chunk), ctx.remaining())
		out = append(out, chunk...)
		if chunk[len(chunk)-1].Token == lexer.eofToken {
			return out, nil
		}
	}
}

func lexerCollectAllInitialCapacity(remainingObservations int) int {
	if remainingObservations <= 0 {
		return 256
	}
	estimated := remainingObservations / 3
	if estimated < 256 {
		return 256
	}
	if estimated > 16384 {
		return 16384
	}
	return estimated
}

func lexemeEnsureCapacity[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	items []Lexeme[TObservation, TToken, TTokenRole],
	appendCount int,
	remainingObservations int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	if appendCount <= 0 {
		return items
	}
	needed := len(items) + appendCount
	if needed <= cap(items) {
		return items
	}
	newCap := cap(items)
	if newCap == 0 {
		newCap = max(appendCount, 256)
	}
	for newCap < needed {
		if remainingObservations >= 0 {
			newCap *= 2
		} else {
			newCap += max(newCap/2, 256)
		}
	}
	out := make([]Lexeme[TObservation, TToken, TTokenRole], len(items), newCap)
	copy(out, items)
	return out
}

func lexerPreTokensGet[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	cache *lexerSessionScanCache,
) ([]Lexeme[TObservation, TToken, TTokenRole], bool) {
	if cache.preTokens == nil {
		return nil, false
	}
	out, ok := cache.preTokens.([]Lexeme[TObservation, TToken, TTokenRole])
	return out, ok
}

func lexerWindowTokensGet[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	cache *lexerSessionScanCache,
) ([]Lexeme[TObservation, TToken, TTokenRole], bool) {
	if cache.windowTokens == nil {
		return nil, false
	}
	out, ok := cache.windowTokens.([]Lexeme[TObservation, TToken, TTokenRole])
	return out, ok
}

func lexerApplyConsumedLexemeToSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *LexerSession[TObservation, TState, TToken],
	lex Lexeme[TObservation, TToken, TTokenRole],
) {
	if len(lex.Raw) == 0 {
		return
	}
	session.position = lex.End
	session.currentLine = lex.EndLine
	session.currentColumn = lex.EndColumn
	session.tokenNumber++
}

func lexerApplyConsumedLexemeToStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *StreamingLexerSession[TObservation, TState, TToken],
	lex Lexeme[TObservation, TToken, TTokenRole],
) {
	if len(lex.Raw) == 0 {
		return
	}
	session.absPos = lex.End
	session.currentLine = lex.EndLine
	session.currentColumn = lex.EndColumn
	session.tokenNumber++
	if len(lex.Raw) <= len(session.buffer) {
		session.buffer = session.buffer[len(lex.Raw):]
		streamingMaybeCompact(session)
	}
}

func lexerEnsurePreTokenizedSessionCache[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
) ([]Lexeme[TObservation, TToken, TTokenRole], bool) {
	if !session.scanCache.initialized || session.scanCache.mode != ScanModePreTokenizeAll {
		ctx := scannerFromSliceSimulated(session)
		toks, err := lexerCollectAllFromContext(
			lexer,
			ctx,
			session.currentState,
			session.newlineDetector,
			session.columnAdvanceFn,
			session.currentLine,
			session.currentColumn,
			session.tokenNumber,
		)
		if err != nil {
			err.Formatter = lexer.formatter
			session.lastError = err
			return nil, false
		}
		session.scanCache.initialized = true
		session.scanCache.mode = ScanModePreTokenizeAll
		session.scanCache.baseTokenNumber = session.tokenNumber
		session.scanCache.preTokens = toks
	}
	toks, ok := lexerPreTokensGet[TObservation, TToken, TTokenRole](&session.scanCache)
	if !ok {
		lexerSessionScanCacheReset(&session.scanCache)
		return nil, false
	}
	return toks, true
}

func lexerEnsurePreTokenizedStreamingCache[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
) ([]Lexeme[TObservation, TToken, TTokenRole], bool) {
	if !session.scanCache.initialized || session.scanCache.mode != ScanModePreTokenizeAll {
		ctx := scannerFromStreamingSimulated(session)
		toks, err := lexerCollectAllFromContext(
			lexer,
			ctx,
			session.currentState,
			session.newlineDetector,
			session.columnAdvanceFn,
			session.currentLine,
			session.currentColumn,
			session.tokenNumber,
		)
		if err != nil {
			err.Formatter = lexer.formatter
			session.lastError = err
			return nil, false
		}
		session.scanCache.initialized = true
		session.scanCache.mode = ScanModePreTokenizeAll
		session.scanCache.baseTokenNumber = session.tokenNumber
		session.scanCache.preTokens = toks
	}
	toks, ok := lexerPreTokensGet[TObservation, TToken, TTokenRole](&session.scanCache)
	if !ok {
		lexerSessionScanCacheReset(&session.scanCache)
		return nil, false
	}
	return toks, true
}

func lexerEnsureCircularWindowSessionCache[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
	minCount int,
) ([]Lexeme[TObservation, TToken, TTokenRole], bool) {
	window, ok := lexerWindowTokensGet[TObservation, TToken, TTokenRole](&session.scanCache)
	startMatches := session.scanCache.initialized &&
		session.scanCache.mode == ScanModeCircularTokenBuffer &&
		session.scanCache.windowStartToken == session.tokenNumber
	if ok && startMatches && len(window) >= minCount {
		return window, true
	}

	need := lexer.scanConfig.CircularBufferSize
	if need < minCount {
		need = minCount
	}
	ctx := scannerFromSliceSimulated(session)
	lexemes, err := lexerPeekRangeCore(
		lexer,
		ctx,
		session.currentState,
		session.newlineDetector,
		session.columnAdvanceFn,
		session.currentLine,
		session.currentColumn,
		session.tokenNumber,
		need,
	)
	if err != nil {
		err.Formatter = lexer.formatter
		session.lastError = err
		return nil, false
	}

	session.scanCache.initialized = true
	session.scanCache.mode = ScanModeCircularTokenBuffer
	session.scanCache.windowStartToken = session.tokenNumber
	session.scanCache.windowTokens = lexemes
	return lexemes, true
}

func lexerEnsureCircularWindowStreamingCache[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
	minCount int,
) ([]Lexeme[TObservation, TToken, TTokenRole], bool) {
	window, ok := lexerWindowTokensGet[TObservation, TToken, TTokenRole](&session.scanCache)
	startMatches := session.scanCache.initialized &&
		session.scanCache.mode == ScanModeCircularTokenBuffer &&
		session.scanCache.windowStartToken == session.tokenNumber
	if ok && startMatches && len(window) >= minCount {
		return window, true
	}

	need := lexer.scanConfig.CircularBufferSize
	if need < minCount {
		need = minCount
	}
	ctx := scannerFromStreamingSimulated(session)
	lexemes, err := lexerPeekRangeCore(
		lexer,
		ctx,
		session.currentState,
		session.newlineDetector,
		session.columnAdvanceFn,
		session.currentLine,
		session.currentColumn,
		session.tokenNumber,
		need,
	)
	if err != nil {
		err.Formatter = lexer.formatter
		session.lastError = err
		return nil, false
	}

	session.scanCache.initialized = true
	session.scanCache.mode = ScanModeCircularTokenBuffer
	session.scanCache.windowStartToken = session.tokenNumber
	session.scanCache.windowTokens = lexemes
	return lexemes, true
}

func lexerPeekRangeFromPreTokenizedSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	session.begin()
	defer session.end()
	if count <= 0 || session.lastError != nil {
		return nil
	}
	toks, ok := lexerEnsurePreTokenizedSessionCache(lexer, session)
	if !ok {
		return nil
	}
	start := session.tokenNumber - session.scanCache.baseTokenNumber
	if start < 0 || start >= len(toks) {
		return []Lexeme[TObservation, TToken, TTokenRole]{lexerBuildEOFSession(lexer, session)}
	}
	end := start + count
	if end > len(toks) {
		end = len(toks)
	}
	out := make([]Lexeme[TObservation, TToken, TTokenRole], end-start)
	copy(out, toks[start:end])
	return out
}

func lexerPeekFromPreTokenizedSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
	n int,
) Lexeme[TObservation, TToken, TTokenRole] {
	if n < 0 || session.lastError != nil {
		return lexerBuildEOFSession(lexer, session)
	}
	lexemes := lexerPeekRangeFromPreTokenizedSession(lexer, session, n+1)
	if n >= len(lexemes) {
		return lexerBuildEOFSession(lexer, session)
	}
	return lexemes[n]
}

func lexerConsumeFromPreTokenizedSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
) Lexeme[TObservation, TToken, TTokenRole] {
	session.begin()
	defer session.end()
	if session.lastError != nil {
		return lexerBuildEOFSession(lexer, session)
	}
	toks, ok := lexerEnsurePreTokenizedSessionCache(lexer, session)
	if !ok {
		return lexerBuildEOFSession(lexer, session)
	}
	idx := session.tokenNumber - session.scanCache.baseTokenNumber
	if idx < 0 || idx >= len(toks) {
		return lexerBuildEOFSession(lexer, session)
	}
	lex := toks[idx]
	lexerApplyConsumedLexemeToSession(session, lex)
	return lex
}

func lexerConsumeRangeFromPreTokenizedSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	if count <= 0 {
		return nil
	}
	out := make([]Lexeme[TObservation, TToken, TTokenRole], 0, count)
	for i := 0; i < count; i++ {
		lex := lexerConsumeFromPreTokenizedSession(lexer, session)
		out = append(out, lex)
		if lex.Token == lexer.eofToken {
			break
		}
	}
	return out
}

func lexerPeekRangeFromCircularWindowSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	session.begin()
	defer session.end()
	if count <= 0 || session.lastError != nil {
		return nil
	}
	window, ok := lexerEnsureCircularWindowSessionCache(lexer, session, count)
	if !ok {
		return nil
	}
	if len(window) < count {
		count = len(window)
	}
	out := make([]Lexeme[TObservation, TToken, TTokenRole], count)
	copy(out, window[:count])
	return out
}

func lexerPeekFromCircularWindowSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
	n int,
) Lexeme[TObservation, TToken, TTokenRole] {
	if n < 0 || session.lastError != nil {
		return lexerBuildEOFSession(lexer, session)
	}
	lexemes := lexerPeekRangeFromCircularWindowSession(lexer, session, n+1)
	if n >= len(lexemes) {
		return lexerBuildEOFSession(lexer, session)
	}
	return lexemes[n]
}

func lexerConsumeFromCircularWindowSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
) Lexeme[TObservation, TToken, TTokenRole] {
	session.begin()
	defer session.end()
	if session.lastError != nil {
		return lexerBuildEOFSession(lexer, session)
	}
	lexemes, ok := lexerEnsureCircularWindowSessionCache(lexer, session, max(1, lexer.scanConfig.CircularBufferSize))
	if !ok {
		return lexerBuildEOFSession(lexer, session)
	}
	if len(lexemes) == 0 {
		return lexerBuildEOFSession(lexer, session)
	}
	lex := lexemes[0]
	lexerApplyConsumedLexemeToSession(session, lex)
	return lex
}

func lexerConsumeRangeFromCircularWindowSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	if count <= 0 {
		return nil
	}
	out := make([]Lexeme[TObservation, TToken, TTokenRole], 0, count)
	for i := 0; i < count; i++ {
		lex := lexerConsumeFromCircularWindowSession(lexer, session)
		out = append(out, lex)
		if lex.Token == lexer.eofToken {
			break
		}
	}
	return out
}

func lexerPeekRangeFromPreTokenizedStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	session.begin()
	defer session.end()
	if count <= 0 || session.lastError != nil {
		return nil
	}
	toks, ok := lexerEnsurePreTokenizedStreamingCache(lexer, session)
	if !ok {
		return nil
	}
	start := session.tokenNumber - session.scanCache.baseTokenNumber
	if start < 0 || start >= len(toks) {
		return []Lexeme[TObservation, TToken, TTokenRole]{lexerBuildEOFSessionStream(lexer, session)}
	}
	end := start + count
	if end > len(toks) {
		end = len(toks)
	}
	out := make([]Lexeme[TObservation, TToken, TTokenRole], end-start)
	copy(out, toks[start:end])
	return out
}

func lexerPeekFromPreTokenizedStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
	n int,
) Lexeme[TObservation, TToken, TTokenRole] {
	if n < 0 || session.lastError != nil {
		return lexerBuildEOFSessionStream(lexer, session)
	}
	lexemes := lexerPeekRangeFromPreTokenizedStreaming(lexer, session, n+1)
	if n >= len(lexemes) {
		return lexerBuildEOFSessionStream(lexer, session)
	}
	return lexemes[n]
}

func lexerConsumeFromPreTokenizedStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
) Lexeme[TObservation, TToken, TTokenRole] {
	session.begin()
	defer session.end()
	if session.lastError != nil {
		return lexerBuildEOFSessionStream(lexer, session)
	}
	toks, ok := lexerEnsurePreTokenizedStreamingCache(lexer, session)
	if !ok {
		return lexerBuildEOFSessionStream(lexer, session)
	}
	idx := session.tokenNumber - session.scanCache.baseTokenNumber
	if idx < 0 || idx >= len(toks) {
		return lexerBuildEOFSessionStream(lexer, session)
	}
	lex := toks[idx]
	lexerApplyConsumedLexemeToStreaming(session, lex)
	return lex
}

func lexerConsumeRangeFromPreTokenizedStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	if count <= 0 {
		return nil
	}
	out := make([]Lexeme[TObservation, TToken, TTokenRole], 0, count)
	for i := 0; i < count; i++ {
		lex := lexerConsumeFromPreTokenizedStreaming(lexer, session)
		out = append(out, lex)
		if lex.Token == lexer.eofToken {
			break
		}
	}
	return out
}

func lexerPeekRangeFromCircularWindowStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	session.begin()
	defer session.end()
	if count <= 0 || session.lastError != nil {
		return nil
	}
	window, ok := lexerEnsureCircularWindowStreamingCache(lexer, session, count)
	if !ok {
		return nil
	}
	if len(window) < count {
		count = len(window)
	}
	out := make([]Lexeme[TObservation, TToken, TTokenRole], count)
	copy(out, window[:count])
	return out
}

func lexerPeekFromCircularWindowStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
	n int,
) Lexeme[TObservation, TToken, TTokenRole] {
	if n < 0 || session.lastError != nil {
		return lexerBuildEOFSessionStream(lexer, session)
	}
	lexemes := lexerPeekRangeFromCircularWindowStreaming(lexer, session, n+1)
	if n >= len(lexemes) {
		return lexerBuildEOFSessionStream(lexer, session)
	}
	return lexemes[n]
}

func lexerConsumeFromCircularWindowStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
) Lexeme[TObservation, TToken, TTokenRole] {
	session.begin()
	defer session.end()
	if session.lastError != nil {
		return lexerBuildEOFSessionStream(lexer, session)
	}
	lexemes, ok := lexerEnsureCircularWindowStreamingCache(lexer, session, max(1, lexer.scanConfig.CircularBufferSize))
	if !ok {
		return lexerBuildEOFSessionStream(lexer, session)
	}
	if len(lexemes) == 0 {
		return lexerBuildEOFSessionStream(lexer, session)
	}
	lex := lexemes[0]
	lexerApplyConsumedLexemeToStreaming(session, lex)
	return lex
}

func lexerConsumeRangeFromCircularWindowStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	if count <= 0 {
		return nil
	}
	out := make([]Lexeme[TObservation, TToken, TTokenRole], 0, count)
	for i := 0; i < count; i++ {
		lex := lexerConsumeFromCircularWindowStreaming(lexer, session)
		out = append(out, lex)
		if lex.Token == lexer.eofToken {
			break
		}
	}
	return out
}
