package internal

import "fmt"

// ----------------------------------------------------------- RESULT

type LexerLexResult struct {
	Tokens []Token
	EOF    bool
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
	if len(content) == 0 {
		result.EOF = true
		return
	}

	pos := 0
	for {
		char := content[pos]
		fmt.Printf("%03d) %s\n", pos, string(char))

		pos++
	}
}
