//go:build !debug

package lexarch

func (s *LexerSession[TObservation, TState, TToken, TTokenRole]) begin() {
}

func (s *LexerSession[TObservation, TState, TToken, TTokenRole]) end() {
}

func (s *StreamingLexerSession[TObservation, TState, TToken, TTokenRole]) begin() {
}

func (s *StreamingLexerSession[TObservation, TState, TToken, TTokenRole]) end() {
}
