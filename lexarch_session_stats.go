package lexarch

import "cmp"

/*
LexerSessionPreTokenizedLexemeCount returns len(preTokens) when the session cache holds
pretokenized lexemes. ok is false if not yet materialized or empty.

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
	if !c.initialized || len(c.preTokens) == 0 {
		return 0, false
	}
	return len(c.preTokens), true
}

/*
LexerSessionNextTokenNumber returns the session’s next token sequence number (1-indexed),
i.e. the cursor position in the raw lexeme stream relative to pretokenized materialization.

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
