package lexarch

import "cmp"

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

/*
LexerConsume recognizes and consumes the next token.

The session uses a pretokenized lexeme cache (filled on demand from the current cursor).

If an error occurs or the session already has an error, it returns an EOF lexeme
and sets session.lastError.

Time complexity: O(1) per call after the cache is warm; initial fill is O(input).
*/
func LexerConsume[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
) Lexeme[TObservation, TToken, TTokenRole] {
	return lexerConsumeFromPreTokenizedSession(lexer, session)
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
	return lexerConsumeRangeFromPreTokenizedSession(lexer, session, count)
}

/*
LexerPeek returns the n-th token ahead without consuming input.
*/
func LexerPeek[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
	n int,
) Lexeme[TObservation, TToken, TTokenRole] {
	return lexerPeekFromPreTokenizedSession(lexer, session, n)
}

/*
LexerPeekRange returns up to `count` upcoming tokens without consuming input.
*/
func LexerPeekRange[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable](
	lexer *Lexer[TObservation, TState, TToken, TTokenRole],
	session *LexerSession[TObservation, TState, TToken, TTokenRole],
	count int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	return lexerPeekRangeFromPreTokenizedSession(lexer, session, count)
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
