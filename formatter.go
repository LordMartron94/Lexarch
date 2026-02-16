package lexarch

import "fmt"

type RuneFormatStyle int

const (
	RuneFormatQuoted RuneFormatStyle = iota // 'a', '\n'
	RuneFormatHex                           // U+0041
	RuneFormatMixed                         // printable → 'a', others → U+XXXX
)

type RuneFormatterConfig struct {
	Style RuneFormatStyle

	// Printable ASCII range (defaults 32–126)
	MinPrintable rune
	MaxPrintable rune
}

func RuneFormatterCreate(cfg RuneFormatterConfig) ObservationFormatter[rune] {

	min := cfg.MinPrintable
	max := cfg.MaxPrintable

	if min == 0 {
		min = 32
	}
	if max == 0 {
		max = 126
	}

	return func(r rune) string {

		switch r {
		case '\n':
			return `\n`
		case '\t':
			return `\t`
		case '\r':
			return `\r`
		case '\v':
			return `\v`
		case '\f':
			return `\f`
		}

		printable := r >= min && r <= max

		switch cfg.Style {

		case RuneFormatQuoted:
			if printable {
				return fmt.Sprintf("'%c'", r)
			}
			return fmt.Sprintf("U+%04X", r)

		case RuneFormatHex:
			return fmt.Sprintf("U+%04X", r)

		case RuneFormatMixed:
			if printable {
				return fmt.Sprintf("'%c'", r)
			}
			return fmt.Sprintf("U+%04X", r)

		default:
			if printable {
				return fmt.Sprintf("'%c'", r)
			}
			return fmt.Sprintf("U+%04X", r)
		}
	}
}

func RuneFormatterDefault() ObservationFormatter[rune] {
	return RuneFormatterCreate(RuneFormatterConfig{
		Style: RuneFormatMixed,
	})
}

func ByteFormatterCreate(cfg RuneFormatterConfig) ObservationFormatter[byte] {
	rf := RuneFormatterCreate(cfg)
	return func(b byte) string {
		return rf(rune(b))
	}
}

func ByteFormatterDefault() ObservationFormatter[byte] {
	return ByteFormatterCreate(RuneFormatterConfig{
		Style: RuneFormatMixed,
	})
}
