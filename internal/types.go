package internal

import "fmt"

type TokenKind uint32
type TokenRole uint32

type Token struct {
	// We keep this explicitly small to fit in cache (~64 bytes)

	// Metadata = 12 bytes total
	Kind   TokenKind
	Role   TokenRole
	FileID uint16 // Support for ~65k files in a shared context.

	// 8 Bytes: location
	Span ByteSpan

	// We explicitly do NOT store line + column counters, see: docs/adr/0001-token-location.md
}

type ByteSpan struct {
	Offset uint32 // ~4.2billion bytes = ~4.2GiB -- should be enough for virtually any file
	Length uint32
}

type LexerError struct {
	msg  string
	area ByteSpan
}

func (e *LexerError) Error() string {
	return fmt.Sprintf("lexer error: %s at %d", e.msg, e.area.Offset)
}

func (e *LexerError) Span() ByteSpan {
	return e.area
}
