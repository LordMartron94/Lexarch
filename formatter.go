package lexarch

import (
	"fmt"
	"strings"
)

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

	formatOne := func(r rune) string {
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

	formatMany := func(rs []rune) string {
		if len(rs) == 0 {
			return ""
		}

		return string(rs)
	}

	return ObservationFormatter[rune]{
		FormatOne:  formatOne,
		FormatMany: formatMany,
	}
}

func RuneFormatterDefault() ObservationFormatter[rune] {
	return RuneFormatterCreate(RuneFormatterConfig{
		Style: RuneFormatMixed,
	})
}

func ByteFormatterCreate(cfg RuneFormatterConfig) ObservationFormatter[byte] {
	rf := RuneFormatterCreate(cfg)

	return ObservationFormatter[byte]{
		FormatOne: func(b byte) string {
			return rf.FormatOne(rune(b))
		},

		FormatMany: func(bs []byte) string {
			if len(bs) == 0 {
				return ""
			}

			var bld strings.Builder
			bld.Grow(len(bs) * 2)

			for _, b := range bs {
				bld.WriteString(rf.FormatOne(rune(b)))
			}
			return bld.String()
		},
	}
}

func ByteFormatterDefault() ObservationFormatter[byte] {
	return ByteFormatterCreate(RuneFormatterConfig{
		Style: RuneFormatMixed,
	})
}
