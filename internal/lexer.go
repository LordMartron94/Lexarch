package internal

import "fmt"

// ----------------------------------------------------------- RESULT

type LexerLexResult struct {
	Tokens      []Token
	EOF         bool
	LexingError error
}

// ----------------------------------------------------------- LEXER

type Lexer struct {
}

func LexerCreate() *Lexer {
	return &Lexer{}
}

func LexerLexContent(
	lexer *Lexer,
	content string,
) LexerLexResult {
	result := LexerLexResult{
		Tokens: make([]Token, 0),
		EOF:    false,
	}

	lexContent(content, &result)

	return result
}

// ----------------------------------------------------------- PRIVATE HELPERS

func lexContent(content string, result *LexerLexResult) {
	contentLength := len(content)
	if contentLength == 0 {
		result.EOF = true
		return
	}

	pos := 0

	for pos < contentLength {
		char := content[pos]
		result.LexingError = &LexerError{
			msg: fmt.Sprintf("invalid character '%s'", char),
			area: ByteSpan{
				Offset: 0,
				Length: 0,
			},
		}
		return

		pos++
	}
}
