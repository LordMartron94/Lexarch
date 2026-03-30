/*
Package lexarch provides a highly efficient lexing framework.
*/
package lexarch

import "lexarch/internal"

type TokenKind = internal.TokenKind
type TokenRole = internal.TokenRole

type Lexer = internal.Lexer
type Token = internal.Token

type LexerLexResult = internal.LexerLexResult

type LexerInvalidContentError = internal.LexerInvalidContentError

func LexerCreate() *Lexer {
	return internal.LexerCreate()
}

func LexerLexContent(lexer *Lexer, content string) LexerLexResult {
	return internal.LexerLexContent(lexer, content)
}
