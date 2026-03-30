package internal

type TokenKind uint32
type TokenRole uint32

type Token struct {
	// We keep this explicitly small to fit in cache (~64 bytes)

	// Metadata = 12 bytes total
	Kind   TokenKind
	Role   TokenRole
	FileID uint16 // Support for ~65k files in a shared context.

	// 8 Bytes: location
	ByteOffset uint32 // ~4.2billion bytes = ~4.2GiB -- should be enough for virtually any file
	ByteLength uint32

	// We explicitly do NOT store line + column counters, see: docs/adr/0001-token-location.md
}
