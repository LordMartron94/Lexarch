package lexarch

import (
	"autarch"
	"autarch/pattern"
	"cmp"
	"foundation/domain"
	"memcore"
	"unicode/utf8"
)

/*
Lexer is the main lexical analyzer that contains compiled DFAs for each state and provides
tokenization operations. All patterns are compiled to minimized DFAs at creation time for
optimal runtime performance.

Use cases:
- Tokenizing input streams into sequences of tokens
- Building parsers and language processors
- Implementing lexical analysis for programming languages, protocols, or data formats

Time complexity: N/A - data structure
Space complexity: O(s * a) where s is states and a is alphabet size

Prerequisites:
- Created via LexerCreate with valid rulesets
- Must be closed via LexerClose to free resources

Edge cases:
- Holds references to allocated DFAs, so must be closed to prevent leaks
- Error token is returned when no pattern matches
- EOF token is returned when end of input is reached
- Multiple sessions can use the same lexer concurrently
*/
type Lexer[TObservation cmp.Ordered, TState, TToken, TTokenRole comparable] struct {
	ruleSets         map[TState]*autarch.DFA[TObservation, pattern.AnnotatedOutcome[TokenOutcome[TToken, TTokenRole]]]
	tokenResolutions map[TState]TokenResolutionStepFn[TToken]

	nonTerminalOutcome TokenOutcome[TToken, TTokenRole]

	dfaAllocator memcore.MarkRaw
	eofToken     TToken

	formatter  ObservationFormatter[TObservation]
	scanConfig LexerScanConfig
}

/* ObservationCTX encapsulates the context for observation handling. */
type ObservationCTX[TObservation cmp.Ordered] struct {
	formatter         ObservationFormatter[TObservation]
	observationDomain *domain.DiscreteDomain[TObservation]
	toBytes           func(observations []TObservation) []byte
}

func ObservationCTXCreate[TObservation cmp.Ordered](
	formatter ObservationFormatter[TObservation],
	observationDomain *domain.DiscreteDomain[TObservation],
	toBytes func(observations []TObservation) []byte,
) ObservationCTX[TObservation] {
	return ObservationCTX[TObservation]{
		formatter:         formatter,
		observationDomain: observationDomain,
		toBytes:           toBytes,
	}
}

/* LexarchRuneDomain returns the canonical discrete domain for rune observations (Unicode code points). */
func LexarchRuneDomain() *domain.DiscreteDomain[rune] {
	return domain.DiscreteDomainRuneCreate()
}

/*
RunesToBytesDefault returns a UTF-8 encoder for rune observation streams.

This provides a canonical, lossless projection from abstract rune symbols
to concrete byte representation suitable for hashing, debugging, and
automaton compilation internals.

Properties:
  - Unicode-correct
  - Deterministic
  - Order-preserving
  - Minimal encoding (UTF-8)

Time complexity: O(n)
Space complexity: O(n)
*/
func RunesToBytesDefault() func(observations []rune) []byte {
	return func(observations []rune) []byte {
		if len(observations) == 0 {
			return nil
		}

		// Worst case: 4 bytes per rune (UTF-8 max width)
		buf := make([]byte, 0, len(observations)*utf8.UTFMax)

		var tmp [utf8.UTFMax]byte

		for _, r := range observations {
			n := utf8.EncodeRune(tmp[:], r)
			buf = append(buf, tmp[:n]...)
		}

		return buf
	}
}

//go:generate stringer -type CompilerMode
type CompilerMode int

const (
	Thompson CompilerMode = iota + 1
	Glushkov
)

/* LexerScanMode controls how the lexer serves Peek/Consume operations. */
type LexerScanMode int

const (
	/* ScanModeAsIs uses direct DFA scanning for each operation. */
	ScanModeAsIs LexerScanMode = iota + 1
	/* ScanModePreTokenizeAll tokenizes from current session cursor once and reuses tokens by index. */
	ScanModePreTokenizeAll
	/* ScanModeCircularTokenBuffer keeps a bounded upcoming token window near the current cursor. */
	ScanModeCircularTokenBuffer
)

/*
LexScanStats accumulates optional scan counters when LexerScanConfig.Stats is non-nil.

ObservationSteps counts DFA input observations processed (incremented in scanCoreSlice /
scanCoreStreaming). Nil or zero disables all increments.
*/
type LexScanStats struct {
	ObservationSteps uint64
}

/* LexerScanConfig configures scanner behavior and mode-specific tuning values. */
type LexerScanConfig struct {
	Mode               LexerScanMode
	CircularBufferSize int
	ForceRawCopy       bool
	Stats              *LexScanStats
}

/* LexerScanConfigDefault returns the default scanner configuration. */
func LexerScanConfigDefault() LexerScanConfig {
	return LexerScanConfig{
		Mode:               ScanModeAsIs,
		CircularBufferSize: 256,
		ForceRawCopy:       false,
	}
}

type lexerSessionScanCache[TObservation cmp.Ordered, TToken, TTokenRole comparable] struct {
	initialized bool
	mode        LexerScanMode

	baseTokenNumber int
	preTokens       []Lexeme[TObservation, TToken, TTokenRole]

	windowStartToken int
	windowTokens     []Lexeme[TObservation, TToken, TTokenRole]
	outScratch       []Lexeme[TObservation, TToken, TTokenRole]
	collectScratch   []Lexeme[TObservation, TToken, TTokenRole]
	coreRangeScratch []Lexeme[TObservation, TToken, TTokenRole]
	coreChunkScratch []Lexeme[TObservation, TToken, TTokenRole]
}

func lexerSessionScanCacheSliceResetRetain[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	items []Lexeme[TObservation, TToken, TTokenRole],
) []Lexeme[TObservation, TToken, TTokenRole] {
	if items == nil {
		return nil
	}
	return items[:0]
}

func lexerSessionScanCacheResetSoft[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	cache *lexerSessionScanCache[TObservation, TToken, TTokenRole],
) {
	cache.initialized = false
	cache.mode = 0
	cache.baseTokenNumber = 0
	cache.preTokens = lexerSessionScanCacheSliceResetRetain(cache.preTokens)
	cache.windowStartToken = 0
	cache.windowTokens = lexerSessionScanCacheSliceResetRetain(cache.windowTokens)
	cache.outScratch = lexerSessionScanCacheSliceResetRetain(cache.outScratch)
	cache.collectScratch = lexerSessionScanCacheSliceResetRetain(cache.collectScratch)
	cache.coreRangeScratch = lexerSessionScanCacheSliceResetRetain(cache.coreRangeScratch)
	cache.coreChunkScratch = lexerSessionScanCacheSliceResetRetain(cache.coreChunkScratch)
}

func lexerSessionScanCacheResetHard[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	cache *lexerSessionScanCache[TObservation, TToken, TTokenRole],
) {
	cache.initialized = false
	cache.mode = 0
	cache.baseTokenNumber = 0
	cache.preTokens = nil
	cache.windowStartToken = 0
	cache.windowTokens = nil
	cache.outScratch = nil
	cache.collectScratch = nil
	cache.coreRangeScratch = nil
	cache.coreChunkScratch = nil
}

func lexerSessionScanCacheReset[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	cache *lexerSessionScanCache[TObservation, TToken, TTokenRole],
) {
	lexerSessionScanCacheResetSoft(cache)
}

/*
LexerCreate compiles a set of rulesets into a ready-to-use lexer. Each state's ruleset is
compiled to a minimized DFA for efficient token recognition. The compilation process:
1. Converts each pattern to an NFA
2. Merges NFAs for each ruleset using alternation
3. Converts to DFA via subset construction
4. Minimizes the DFA using Hopcroft's algorithm

Use cases:
- Building lexers from pattern definitions
- Creating reusable tokenization engines
- Initializing lexical analyzers for parsers

Time complexity: O(2^n * a) worst case for NFA-to-DFA conversion per ruleset, where n is NFA states and a is alphabet size
Space complexity: O(s * a) where s is DFA states and a is alphabet size

Prerequisites:
- inputRulesets must contain at least one state
- Each ruleset must contain at least one rule
- errorToken must be distinct from all valid token values
- eofToken must be distinct from all valid token values and errorToken
- scratchAllocationFn must be a valid allocation function
- maxDFAAllocatorMemory must be sufficient for DFA storage
- nfaToDFAPipelineMinTemp and nfaToDFAPipelineMaxTemp define the temporary allocator bounds for NFA-to-DFA conversion and DFA minimization; max must be sufficient for the conversion working set

Edge cases:
- Panics if DFA allocator exceeds maxDFAAllocatorMemory
- Panics if NFA-to-DFA or minimization temp allocator exceeds nfaToDFAPipelineMaxTemp
- Empty rulesets create DFAs that never accept
- Rules are evaluated with longest match priority
- The lexer must be closed via LexerClose to free resources
- EOF token is returned when position reaches end of input
*/
