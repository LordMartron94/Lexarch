package lexarch

import "cmp"

/*
LexerSessionPreTokenizedLexemeCount returns len(preTokens) when the session cache holds a
ScanModePreTokenizeAll stream. ok is false if not pretokenized or empty.

Time complexity: O(1)
Space complexity: O(1)
*/
func LexerSessionPreTokenizedLexemeCount[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) (n int, ok bool) {
	if session == nil {
		return 0, false
	}
	c := &session.scanCache
	if !c.initialized || c.mode != ScanModePreTokenizeAll || len(c.preTokens) == 0 {
		return 0, false
	}
	return len(c.preTokens), true
}

/*
StreamingLexerSessionPreTokenizedLexemeCount is the streaming-session counterpart of
LexerSessionPreTokenizedLexemeCount.

Time complexity: O(1)
Space complexity: O(1)
*/
func StreamingLexerSessionPreTokenizedLexemeCount[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
) (n int, ok bool) {
	if session == nil {
		return 0, false
	}
	c := &session.scanCache
	if !c.initialized || c.mode != ScanModePreTokenizeAll || len(c.preTokens) == 0 {
		return 0, false
	}
	return len(c.preTokens), true
}

/*
LexerSessionNextTokenNumber returns the session’s next token sequence number (1-indexed),
i.e. the cursor position in the raw lexeme stream for pretokenized mode.

Time complexity: O(1)
Space complexity: O(1)
*/
func LexerSessionNextTokenNumber[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) int {
	if session == nil {
		return 0
	}
	return session.tokenNumber
}

/*
StreamingLexerSessionNextTokenNumber returns the streaming session’s next token sequence number.

Time complexity: O(1)
Space complexity: O(1)
*/
func StreamingLexerSessionNextTokenNumber[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	session *StreamingLexerSession[TObservation, TState, TToken, TTokenRole],
) int {
	if session == nil {
		return 0
	}
	return session.tokenNumber
}

/*
LexerScanStatsBind attaches optional scan stats to the lexer. Pass nil to detach.

Time complexity: O(1)
Space complexity: O(1)
*/
func LexerScanStatsBind[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	stats *LexScanStats,
) {
	if lexer == nil {
		return
	}
	lexer.scanConfig.Stats = stats
}

/*
LexerScanStatsObservationSteps returns ObservationSteps from the lexer’s bound LexScanStats,
or zero when unset.

Time complexity: O(1)
Space complexity: O(1)
*/
func LexerScanStatsObservationSteps[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
) uint64 {
	if lexer == nil || lexer.scanConfig.Stats == nil {
		return 0
	}
	return lexer.scanConfig.Stats.ObservationSteps
}
