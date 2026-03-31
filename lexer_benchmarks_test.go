package lexarch

import (
	"autarch/pattern"
	"fmt"
	"foundation/benchmarking"
	"foundation/benchreport"
	"foundation/domain"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const (
	benchTokIdentifier TokenKind = iota + 1000
	benchTokInteger
	benchTokWhitespace
	benchTokOperator
	benchTokPunctuation
)

type lexerBenchData struct {
	lexer      *Lexer
	session    *LexingSession
	out        *LexingNextResult
	content    string
	charCount  uint64
	tokenCount uint64

	iterationsTotal uint64
	tokensTotal     uint64
	charsTotal      uint64
}

func lexerBenchSetupLexer() *Lexer {
	cfg := LexerConfigurationCreate()

	obsDomain := domain.DiscreteDomainRuneCreate()
	factory := pattern.RegulaASTFactoryCreate(obsDomain)
	templates := pattern.RegulaTemplatesCreate(factory)

	rules := []*LexingRule{
		LexingRuleCreate(templates.Whitespace().Plus(), 1, benchTokWhitespace, 0),
		LexingRuleCreate(templates.Identifier(), 2, benchTokIdentifier, 0),
		LexingRuleCreate(templates.Integer(), 2, benchTokInteger, 0),
		LexingRuleCreate(
			factory.AnyOf(
				pattern.LiteralString(factory, "="),
				pattern.LiteralString(factory, "+"),
				pattern.LiteralString(factory, "*"),
				pattern.LiteralString(factory, "-"),
			),
			3,
			benchTokOperator,
			0,
		),
		LexingRuleCreate(pattern.LiteralString(factory, ";"), 3, benchTokPunctuation, 0),
		LexingRuleCreate(pattern.LiteralString(factory, "("), 3, benchTokPunctuation, 0),
		LexingRuleCreate(pattern.LiteralString(factory, ")"), 3, benchTokPunctuation, 0),
	}

	state := LexingStateCreate("INITIAL", rules)
	LexerConfigurationRegisterState(cfg, state, true)
	return LexerCreate(cfg)
}

func lexerBenchResolveCorpusPath(fileName string) (string, error) {
	candidates := []string{
		filepath.Join("assets", "benchmarks", "lexarch", fileName),
		filepath.Join("..", "..", "assets", "benchmarks", "lexarch", fileName),
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("unable to resolve corpus file %q", fileName)
}

func lexerBenchLoadCorpus(fileName string) (string, error) {
	path, err := lexerBenchResolveCorpusPath(fileName)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func lexerBenchLexOnce(lexer *Lexer, session *LexingSession, out *LexingNextResult, content string) (tokens uint64, err error) {
	benchmarking.BenchmarkingCallgrindInstrRegionMaybeBegin()
	defer benchmarking.BenchmarkingCallgrindInstrRegionMaybeEnd()

	LexerLexingSessionReset(lexer, session, content, 0)

	for {
		LexingSessionConsume(session, out)
		if out.LexingError != nil {
			return tokens, out.LexingError
		}
		if out.Token == nil {
			return tokens, fmt.Errorf("lexer returned nil token without error")
		}
		if out.Token.Kind == TokenKindEOF {
			break
		}
		tokens++
	}

	return tokens, nil
}

func lexerBenchCharsPerSec(chars uint64, dur time.Duration) float64 {
	if dur.Nanoseconds() <= 0 {
		return 0
	}
	return float64(chars) * 1e9 / float64(dur.Nanoseconds())
}

func lexerBenchTokensPerSec(tokens uint64, dur time.Duration) float64 {
	if dur.Nanoseconds() <= 0 {
		return 0
	}
	return float64(tokens) * 1e9 / float64(dur.Nanoseconds())
}

func benchmarkLexerCorpus(b *testing.B, corpusName, fileName string) {
	b.Run("Corpus="+corpusName, func(b *testing.B) {
		benchmarking.BenchmarkWithMetricsConfig(
			b,
			benchmarking.BenchmarkMetricsConfig{},
			func(b *testing.B) *lexerBenchData {
				content, err := lexerBenchLoadCorpus(fileName)
				if err != nil {
					b.Fatal(err)
				}

				lexer := lexerBenchSetupLexer()
				session := LexerLexingSessionCreate(lexer, content, 0)
				out := LexingSessionNextResultCreate()
				tokenCount, err := lexerBenchLexOnce(lexer, session, out, content)
				if err != nil {
					LexerDestroy(lexer)
					b.Fatal(err)
				}

				return &lexerBenchData{
					lexer:      lexer,
					session:    session,
					out:        out,
					content:    content,
					charCount:  uint64(len([]rune(content))),
					tokenCount: tokenCount,
				}
			},
			func(data *lexerBenchData) {
				_, _ = lexerBenchLexOnce(data.lexer, data.session, data.out, data.content)
			},
			func(data *lexerBenchData, b *testing.B) {
				for i := 0; i < b.N; i++ {
					tokenCount, err := lexerBenchLexOnce(data.lexer, data.session, data.out, data.content)
					if err != nil {
						panic(err)
					}

					data.iterationsTotal++
					data.tokensTotal += tokenCount
					data.charsTotal += data.charCount
				}

				b.StopTimer()

				if data.iterationsTotal == 0 {
					return
				}

				n := float64(data.iterationsTotal)
				benchmarking.BenchmarkingReportMetric(b, float64(data.charCount), "chars/op", benchreport.MetricKindCount)
				benchmarking.BenchmarkingReportMetric(b, float64(data.tokenCount), "tokens/op", benchreport.MetricKindCount)
				benchmarking.BenchmarkingReportMetric(b, float64(data.charsTotal)/n, "chars.per_iteration", benchreport.MetricKindCount)
				benchmarking.BenchmarkingReportMetric(b, float64(data.tokensTotal)/n, "tokens.per_iteration", benchreport.MetricKindCount)

				elapsed := b.Elapsed()
				benchmarking.BenchmarkingReportMetric(b, lexerBenchCharsPerSec(data.charsTotal, elapsed), "throughput.chars_per_sec", benchreport.MetricKindRatePerSec)
				benchmarking.BenchmarkingReportMetric(b, lexerBenchTokensPerSec(data.tokensTotal, elapsed), "throughput.tokens_per_sec", benchreport.MetricKindRatePerSec)
			},
			func(data *lexerBenchData, b *testing.B) {
				_ = b
				if data != nil && data.lexer != nil {
					LexerDestroy(data.lexer)
					data.lexer = nil
				}
			},
		)
	})
}

func BenchmarkLexerSuite(b *testing.B) {
	b.Run("LexerSuite", func(b *testing.B) {
		benchmarkLexerCorpus(b, "small", "lexer_small.txt")
		benchmarkLexerCorpus(b, "medium", "lexer_medium.txt")
		benchmarkLexerCorpus(b, "large", "lexer_large.txt")
	})
}

func TestProfile_LexerSuite(t *testing.T) {
	content, err := lexerBenchLoadCorpus("lexer_medium.txt")
	if err != nil {
		t.Fatal(err)
	}

	lexer := lexerBenchSetupLexer()
	defer LexerDestroy(lexer)
	session := LexerLexingSessionCreate(lexer, content, 0)
	out := LexingSessionNextResultCreate()

	warm := 0
	if s := os.Getenv(benchmarking.EnvAnvilProfileWarmupIterations); s != "" {
		warm, _ = strconv.Atoi(s)
	}
	work := 1
	if s := os.Getenv(benchmarking.EnvAnvilProfileWorkIterations); s != "" {
		if n, parseErr := strconv.Atoi(s); parseErr == nil && n > 0 {
			work = n
		}
	}

	for i := 0; i < warm; i++ {
		if _, lexErr := lexerBenchLexOnce(lexer, session, out, content); lexErr != nil {
			t.Fatal(lexErr)
		}
	}
	for i := 0; i < work; i++ {
		if _, lexErr := lexerBenchLexOnce(lexer, session, out, content); lexErr != nil {
			t.Fatal(lexErr)
		}
	}
}
