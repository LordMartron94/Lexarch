package lexarch

import (
	"autarch"
	"cmp"
	"fmt"
	"strings"
)

/* ObservationFormatter turns an observation into a string. */
type ObservationFormatter[TObservation cmp.Ordered] struct {
	FormatOne  func(observation TObservation) string
	FormatMany func(observations []TObservation) string
}

type LexingError[TObservation cmp.Ordered, TToken comparable] struct {
	Position int

	StartLine   int
	StartColumn int

	DFAState    uint64
	HasDFAState bool

	// Furthest DFA progress
	Furthest int

	// What was seen (if any)
	Found    TObservation
	HasFound bool

	// What transitions were possible
	Expected []autarch.SymbolDefinition[TObservation]

	// Best partial matches (by priority / length)
	Candidates []TToken

	Reason LexingErrorReason

	Formatter ObservationFormatter[TObservation]
}

func (e *LexingError[TObservation, TToken]) formatExpected(
	fmtObs func(TObservation) string,
) string {
	if len(e.Expected) == 0 {
		return "<none>"
	}

	var sb strings.Builder
	sb.WriteString("[")

	for i, expectedSymbol := range e.Expected {
		if i > 0 {
			sb.WriteString(", ")
		}

		if expectedSymbol.Observation != nil {
			sb.WriteString(fmtObs(*expectedSymbol.Observation))
		} else {
			sb.WriteString(expectedSymbol.Name)
		}
	}

	sb.WriteString("]")
	return sb.String()
}

func (e *LexingError[TObs, TToken]) Error() string {
	if e == nil {
		return "<nil lexing error>"
	}

	switch e.Reason {

	case LexErrUnexpectedEOF:
		var stateStr string
		if e.HasDFAState {
			stateStr = fmt.Sprintf("%d", e.DFAState)
		} else {
			stateStr = "?"
		}

		return fmt.Sprintf(
			"unexpected EOF at line %d:%d (expected %s) (absolute position %d) [dfaState=%s]",
			e.StartLine,
			e.StartColumn,
			e.formatExpected(e.Formatter.FormatOne),
			e.Position,
			stateStr,
		)

	case LexErrNoTransition:
		var stateStr string
		if e.HasDFAState {
			stateStr = fmt.Sprintf("%d", e.DFAState)
		} else {
			stateStr = "?"
		}

		if e.HasFound {
			return fmt.Sprintf(
				"unexpected %s at line %d:%d (expected %s) [dfaState=%s]",
				e.Formatter.FormatOne(e.Found),
				e.StartLine,
				e.StartColumn,
				e.formatExpected(e.Formatter.FormatOne),
				stateStr,
			)
		}

		return fmt.Sprintf(
			"invalid input at line %d:%d (expected %s) [dfaState=%s]",
			e.StartLine,
			e.StartColumn,
			e.formatExpected(e.Formatter.FormatOne),
			stateStr,
		)

	case LexErrBufferLimit:
		return "lexer buffer limit exceeded"

	default:
		return "lexing error"
	}
}

type LexingErrorReason int

const (
	LexErrNoTransition LexingErrorReason = iota
	LexErrUnexpectedEOF
	LexErrBufferLimit
)
