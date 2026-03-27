package lexarch

import "cmp"

type positionTrackingMode int

const (
	positionTrackingModeGeneric positionTrackingMode = iota + 1
	positionTrackingModeRuneFast
	positionTrackingModeByteFast
)

type positionTrackingStrategy[TObservation cmp.Ordered] struct {
	mode            positionTrackingMode
	newlineDetector NewlineDetector[TObservation]
	columnAdvanceFn ColumnAdvanceFn[TObservation]
	tabWidth        int
}

func positionTrackingStrategyGeneric[TObservation cmp.Ordered](
	newlineDetector NewlineDetector[TObservation],
	columnAdvanceFn ColumnAdvanceFn[TObservation],
) positionTrackingStrategy[TObservation] {
	return positionTrackingStrategy[TObservation]{
		mode:            positionTrackingModeGeneric,
		newlineDetector: newlineDetector,
		columnAdvanceFn: columnAdvanceFn,
	}
}

func positionTrackingStrategyRuneFast(tabWidth int) positionTrackingStrategy[rune] {
	return positionTrackingStrategy[rune]{
		mode:     positionTrackingModeRuneFast,
		tabWidth: tabWidth,
	}
}

func positionTrackingStrategyByteFast() positionTrackingStrategy[byte] {
	return positionTrackingStrategy[byte]{
		mode: positionTrackingModeByteFast,
	}
}

func columnAdvanceByteDefault(obs byte, currentColumn int) int {
	_ = obs
	return currentColumn + 1
}

func LexerSessionCreateRuneFast[TState, TToken, TTokenRole comparable](
	initialState TState,
	input []rune,
	tabWidth int,
) *LexerSession[rune, TState, TToken, TTokenRole] {
	session := LexerSessionCreate[rune, TState, TToken, TTokenRole](
		initialState,
		input,
		NewlineDetectorRune(),
		ColumnAdvanceRune(tabWidth),
	)
	session.positionTracking = positionTrackingStrategyRuneFast(tabWidth)
	return session
}

func LexerSessionCreateByteFast[TState, TToken, TTokenRole comparable](
	initialState TState,
	input []byte,
) *LexerSession[byte, TState, TToken, TTokenRole] {
	session := LexerSessionCreate[byte, TState, TToken, TTokenRole](
		initialState,
		input,
		NewlineDetectorByte(),
		columnAdvanceByteDefault,
	)
	session.positionTracking = positionTrackingStrategyByteFast()
	return session
}

func StreamingLexerSessionCreateRuneFast[TState, TToken, TTokenRole comparable](
	initialState TState,
	producer ObservationProducerFn[rune],
	readChunkSize int,
	maxBufferedObservations int,
	tabWidth int,
) *StreamingLexerSession[rune, TState, TToken, TTokenRole] {
	session := StreamingLexerSessionCreate[rune, TState, TToken, TTokenRole](
		initialState,
		producer,
		NewlineDetectorRune(),
		ColumnAdvanceRune(tabWidth),
		readChunkSize,
		maxBufferedObservations,
	)
	session.positionTracking = positionTrackingStrategyRuneFast(tabWidth)
	return session
}

func StreamingLexerSessionCreateByteFast[TState, TToken, TTokenRole comparable](
	initialState TState,
	producer ObservationProducerFn[byte],
	readChunkSize int,
	maxBufferedObservations int,
) *StreamingLexerSession[byte, TState, TToken, TTokenRole] {
	session := StreamingLexerSessionCreate[byte, TState, TToken, TTokenRole](
		initialState,
		producer,
		NewlineDetectorByte(),
		columnAdvanceByteDefault,
		readChunkSize,
		maxBufferedObservations,
	)
	session.positionTracking = positionTrackingStrategyByteFast()
	return session
}
