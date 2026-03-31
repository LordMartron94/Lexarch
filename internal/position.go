package internal

type LexPosition struct {
	StartLine, EndLine     int
	StartColumn, EndColumn int
}

func SpanToPosition(span ByteSpan, content string, tabWidth int) LexPosition {
	currentLine := 1 // Start at line 1
	lineColumn := 1  // Start at column 1

	pos := LexPosition{
		StartLine:   0,
		EndLine:     0,
		StartColumn: 0,
		EndColumn:   0,
	}

	for byteIndex, char := range content {
		// Checks
		cnvByteIndex := uint32(byteIndex)
		if cnvByteIndex == span.Offset {
			pos.StartLine = currentLine
			pos.StartColumn = lineColumn
		}

		if cnvByteIndex == span.Offset+span.Length {
			break
		}

		// Standard Handling
		if char == '\n' {
			currentLine++
			lineColumn = 1
			continue
		}
		if char == '\t' {
			lineColumn += tabWidth
			continue
		}

		lineColumn++
	}

	// Set the end
	pos.EndLine = currentLine
	pos.EndColumn = lineColumn

	return pos
}
