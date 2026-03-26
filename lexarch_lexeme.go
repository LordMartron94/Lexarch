package lexarch

import (
	"cmp"
	"fmt"
)

type Lexeme[TObservation cmp.Ordered, TToken, TTokenRole comparable] struct {
	Raw   []TObservation
	Token TToken

	Start, End int

	// StartColumn inclusive, EndColumn exclusive (half-open span)

	// Position information (1-indexed)
	StartLine   int // Line number where token starts
	StartColumn int // Column number where token starts
	EndLine     int // Line number where token ends
	EndColumn   int // Column number where token ends
	TokenNumber int // Sequence number of this token

	Role TTokenRole

	formatter ObservationFormatter[TObservation]
}

func (l Lexeme[TObservation, TToken, TTokenRole]) DebugString(
	fmtToken func(TToken) string,
	fmtRole func(TTokenRole) string,
) string {

	tokenStr := ""
	if fmtToken != nil {
		tokenStr = fmtToken(l.Token)
	} else {
		tokenStr = fmt.Sprintf("%v", l.Token)
	}

	roleStr := ""
	if fmtRole != nil {
		roleStr = fmtRole(l.Role)
	} else {
		roleStr = fmt.Sprintf("%v", l.Role)
	}

	rawStr := l.FormatRawDiagnostic()

	return fmt.Sprintf(
		"Lexeme{token=%s, role=%s, raw=%q, span=[%d:%d], pos=(%d:%d → %d:%d), #=%d}",
		tokenStr,
		roleStr,
		rawStr,
		l.Start,
		l.End,
		l.StartLine,
		l.StartColumn,
		l.EndLine,
		l.EndColumn,
		l.TokenNumber,
	)
}

func (l *Lexeme[TObs, TToken, TTokenRole]) FormatRawDiagnostic() string {
	if len(l.Raw) == 0 {
		return ""
	}

	return l.formatter.FormatMany(l.Raw)
}
