package lexarch

import "cmp"

func lexerCollectAllFromContext[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	cache *lexerSessionScanCache[TObservation, TToken, TTokenRole],
	ctx scannerContext[TObservation],
	lexerState TState,
	newlineDetector NewlineDetector[TObservation],
	columnAdvanceFn ColumnAdvanceFn[TObservation],
	startLine, startCol, startToken int,
) ([]Lexeme[TObservation, TToken, TTokenRole], *LexingError[TObservation, TToken]) {
	initialCap := lexerCollectAllInitialCapacity(ctx.remaining())
	out := lexemeScratchCollectReset(cache, initialCap)
	chunkScratch := lexemeScratchCoreChunkReset(cache, 256)

	for {
		chunk, err := lexerPeekRangeCoreInto(
			lexer,
			chunkScratch,
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

		chunkScratch = chunk[:0]
		cache.coreChunkScratch = chunkScratch

		if len(chunk) == 0 {
			return out, nil
		}

		out = lexemeEnsureCapacity(out, len(chunk), ctx.remaining())
		out = append(out, chunk...)
		cache.collectScratch = out

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

	newCap := calculateNewCapacity(cap(items), needed, remainingObservations)
	out := make([]Lexeme[TObservation, TToken, TTokenRole], len(items), newCap)
	copy(out, items)

	return out
}

func calculateNewCapacity(currentCap, needed, remainingObservations int) int {
	newCap := currentCap
	if newCap == 0 {
		newCap = max(needed, 256)
	}
	for newCap < needed {
		if remainingObservations >= 0 {
			newCap *= 2
		} else {
			newCap += max(newCap/2, 256)
		}
	}
	return newCap
}

func lexerPreTokensGet[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	cache *lexerSessionScanCache[TObservation, TToken, TTokenRole],
) ([]Lexeme[TObservation, TToken, TTokenRole], bool) {
	if len(cache.preTokens) == 0 {
		return nil, false
	}
	return cache.preTokens, true
}

func lexerWindowTokensGet[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	cache *lexerSessionScanCache[TObservation, TToken, TTokenRole],
) ([]Lexeme[TObservation, TToken, TTokenRole], bool) {
	if len(cache.windowTokens) == 0 {
		return nil, false
	}
	return cache.windowTokens, true
}

func lexemeScratchOutReset[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	cache *lexerSessionScanCache[TObservation, TToken, TTokenRole],
	required int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	if cap(cache.outScratch) < required {
		cache.outScratch = make([]Lexeme[TObservation, TToken, TTokenRole], 0, required)
	}
	return cache.outScratch[:required]
}

func lexemeScratchCollectReset[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	cache *lexerSessionScanCache[TObservation, TToken, TTokenRole],
	required int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	if cap(cache.collectScratch) < required {
		cache.collectScratch = make([]Lexeme[TObservation, TToken, TTokenRole], 0, required)
	}
	return cache.collectScratch[:0]
}

func lexemeScratchCoreChunkReset[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	cache *lexerSessionScanCache[TObservation, TToken, TTokenRole],
	required int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	if cap(cache.coreChunkScratch) < required {
		cache.coreChunkScratch = make([]Lexeme[TObservation, TToken, TTokenRole], 0, required)
	}
	return cache.coreChunkScratch[:0]
}

func lexemeScratchCoreRangeReset[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	cache *lexerSessionScanCache[TObservation, TToken, TTokenRole],
	required int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	if cap(cache.coreRangeScratch) < required {
		cache.coreRangeScratch = make([]Lexeme[TObservation, TToken, TTokenRole], 0, required)
	}
	return cache.coreRangeScratch[:0]
}

func lexerApplyConsumedLexemeToSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
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
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
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
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) ([]Lexeme[TObservation, TToken, TTokenRole], bool) {
	if !session.scanCache.initialized || session.scanCache.mode != ScanModePreTokenizeAll {
		ctx := scannerFromSliceSimulated(session)
		toks, err := lexerCollectAllFromContext(
			lexer,
			&session.scanCache,
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
	toks, ok := lexerPreTokensGet(&session.scanCache)
	if !ok {
		lexerSessionScanCacheResetSoft(&session.scanCache)
		return nil, false
	}
	return toks, true
}

func lexerEnsurePreTokenizedStreamingCache[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
) ([]Lexeme[TObservation, TToken, TTokenRole], bool) {
	if !session.scanCache.initialized || session.scanCache.mode != ScanModePreTokenizeAll {
		ctx := scannerFromStreamingSimulated(session)
		toks, err := lexerCollectAllFromContext(
			lexer,
			&session.scanCache,
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
	toks, ok := lexerPreTokensGet(&session.scanCache)
	if !ok {
		lexerSessionScanCacheResetSoft(&session.scanCache)
		return nil, false
	}
	return toks, true
}

func lexerEnsureCircularWindowSessionCache[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
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
	coreOut := lexemeScratchCoreRangeReset(&session.scanCache, need)
	lexemes, err := lexerPeekRangeCoreInto(
		lexer,
		coreOut,
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
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
	minCount int,
) ([]Lexeme[TObservation, TToken, TTokenRole], bool) {
	window, ok := lexerWindowTokensGet(&session.scanCache)
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
	coreOut := lexemeScratchCoreRangeReset(&session.scanCache, need)
	lexemes, err := lexerPeekRangeCoreInto(
		lexer,
		coreOut,
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
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
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
	out := lexemeScratchOutReset(&session.scanCache, end-start)
	copy(out, toks[start:end])
	return out
}

func lexerPeekFromPreTokenizedSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
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
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
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
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	if count <= 0 {
		return nil
	}
	out := lexemeScratchOutReset(&session.scanCache, count)
	out = out[:0]
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
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
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
	out := lexemeScratchOutReset(&session.scanCache, count)
	copy(out, window[:count])
	return out
}

func lexerPeekFromCircularWindowSession[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
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
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
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
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	if count <= 0 {
		return nil
	}
	out := lexemeScratchOutReset(&session.scanCache, count)
	out = out[:0]
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
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
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
	out := lexemeScratchOutReset(&session.scanCache, end-start)
	copy(out, toks[start:end])
	return out
}

func lexerPeekFromPreTokenizedStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
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
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
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
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	if count <= 0 {
		return nil
	}
	out := lexemeScratchOutReset(&session.scanCache, count)
	out = out[:0]
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
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
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
	out := lexemeScratchOutReset(&session.scanCache, count)
	copy(out, window[:count])
	return out
}

func lexerPeekFromCircularWindowStreaming[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
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
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
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
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	if count <= 0 {
		return nil
	}
	out := lexemeScratchOutReset(&session.scanCache, count)
	out = out[:0]
	for i := 0; i < count; i++ {
		lex := lexerConsumeFromCircularWindowStreaming(lexer, session)
		out = append(out, lex)
		if lex.Token == lexer.eofToken {
			break
		}
	}
	return out
}
