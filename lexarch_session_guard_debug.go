//go:build debug

package lexarch

func (s *LexerSession[TObservation, TState, TToken, TTokenRole]) begin() {
	if s.inUse {
		panic("LexerSession is already in use (concurrent or re-entrant use detected)")
	}
	s.inUse = true
}

func (s *LexerSession[TObservation, TState, TToken, TTokenRole]) end() {
	s.inUse = false
}

func (s *StreamingLexerSession[TObservation, TState, TToken, TTokenRole]) begin() {
	if s.inUse {
		panic("LexerSession is already in use (concurrent or re-entrant use detected)")
	}
	s.inUse = true
}

func (s *StreamingLexerSession[TObservation, TState, TToken, TTokenRole]) end() {
	s.inUse = false
}
