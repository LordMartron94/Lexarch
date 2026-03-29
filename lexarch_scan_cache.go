package lexarch

import (
	"autarch"
	"autarch/pattern"
	"cmp"
	"fmt"
	"memstruct"
)

func lexerCollectAllFromContext[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	dfa *autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]],
	resolutionStep TokenResolutionStepFn[TToken],
	cursor memstruct.ArrayCursor[uint64],
	cache *lexerSessionScanCache[TObservation, TToken, TTokenRole],
	ctx *scannerContext[TObservation],
	positionTracking positionTrackingStrategy[TObservation],
	startLine, startCol, startToken int,
) ([]Lexeme[TObservation, TToken, TTokenRole], *LexingError[TObservation, TToken]) {
	initialCap := lexerCollectAllInitialCapacity(ctx.remaining())
	out := lexemeScratchCollectReset(cache, initialCap)
	chunkScratch := lexemeScratchCoreChunkReset(cache, 256)

	for {
		chunk, err := lexerPeekRangeCoreInto(
			lexer,
			dfa,
			resolutionStep,
			cursor,
			chunkScratch,
			ctx,
			positionTracking,
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

func lexerEnsurePreTokenizedSessionCache[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) ([]Lexeme[TObservation, TToken, TTokenRole], bool) {
	if !session.scanCache.initialized {
		dfa, resolutionStep, err := lexerGetDFAAndResolution(lexer, session.currentState)
		if err != nil {
			session.lastError = &LexingError[TObservation, TToken]{
				Position:    session.position,
				StartLine:   session.currentLine,
				StartColumn: session.currentColumn,
				Reason:      LexErrNoTransition,
				Formatter:   lexer.formatter,
			}
			return nil, false
		}
		ctx := scannerFromSliceSimulated(session)
		toks, lexErr := lexerCollectAllFromContext(
			lexer,
			dfa,
			resolutionStep,
			lexerSessionCursorGet(session, dfa),
			&session.scanCache,
			ctx,
			session.positionTracking,
			session.currentLine,
			session.currentColumn,
			session.tokenNumber,
		)
		if lexErr != nil {
			lexErr.Formatter = lexer.formatter
			session.lastError = lexErr
			return nil, false
		}
		session.scanCache.initialized = true
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
	toks, ok := lexerEnsurePreTokenizedSessionCache(lexer, session)
	if !ok {
		return lexerBuildEOFSession(lexer, session)
	}
	base := session.tokenNumber - session.scanCache.baseTokenNumber
	idx := base + n
	if idx < 0 || idx >= len(toks) {
		return lexerBuildEOFSession(lexer, session)
	}
	return toks[idx]
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
	session.begin()
	defer session.end()
	if session.lastError != nil {
		return nil
	}
	toks, ok := lexerEnsurePreTokenizedSessionCache(lexer, session)
	if !ok {
		return []Lexeme[TObservation, TToken, TTokenRole]{lexerBuildEOFSession(lexer, session)}
	}
	start := session.tokenNumber - session.scanCache.baseTokenNumber
	if start < 0 || start >= len(toks) {
		return []Lexeme[TObservation, TToken, TTokenRole]{lexerBuildEOFSession(lexer, session)}
	}
	end := start + count
	if end > len(toks) {
		end = len(toks)
	}
	segment := toks[start:end]
	n := len(segment)
	for i, lex := range segment {
		if lex.Token == lexer.eofToken {
			n = i + 1
			break
		}
	}
	segment = segment[:n]
	out := lexemeScratchOutReset(&session.scanCache, len(segment))
	copy(out, segment)
	for _, lex := range out {
		lexerApplyConsumedLexemeToSession(session, lex)
	}
	return out
}

/*
LexerSessionEnsurePreTokenizedAll materializes the pretokenized lexeme slice when not yet built.

Time complexity: O(input) on first call from the current cursor
Space complexity: O(tokens)
*/
func LexerSessionEnsurePreTokenizedAll[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) error {
	if lexer == nil || session == nil {
		return nil
	}
	_, ok := lexerEnsurePreTokenizedSessionCache(lexer, session)
	if ok {
		return nil
	}
	if session.lastError != nil {
		return session.lastError
	}
	return fmt.Errorf("lexarch: pretokenize failed")
}
