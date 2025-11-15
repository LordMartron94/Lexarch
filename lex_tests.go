package lexarch

import (
	"autarch"
	"autarch/regex"
	"fmt"
	foundationtesting "foundation/testing"
	"memarch"
	"memcore"
	"memforge"
	"testing"
)

type tokenType int

const (
	// Keyword has priority (added first)
	Keyword tokenType = iota // 0
	Literal                  // 1
	Invalid                  // 2
)

func TestLexingSimpleLanguage(t *testing.T) {
	allocator := memforge.DynamicLinearAllocatorCreateFunction(uint64(1*memcore.Byte), func(currentCap, neededCap uint64) uint64 {
		newSize := currentCap * 2
		if newSize < neededCap {
			newSize = neededCap
		}

		if newSize > uint64(1*memcore.GigaByte) {
			panic("too much memory for a test")
		}

		return newSize
	})

	allocFn := func(sizeBytes, alignment uint64) memcore.MarkRaw {
		return memforge.DynamicLinearAllocatorMallocUnsafe(allocator, sizeBytes, alignment)
	}

	defer memforge.DynamicLinearAllocatorDestroy(allocator)

	keywords := "if|else|elif"
	literal := "[a-zA-Z]+"

	keywordNFA := produceNFAForRegex(allocFn, keywords, Keyword)
	literalNFA := produceNFAForRegex(allocFn, literal, Literal)

	// Merge keywords (nfaA) and literals (nfaB).
	// This gives 'Keyword' priority over 'Literal'
	merged := autarch.NFAMergeOr(
		keywordNFA,
		literalNFA,
		allocFn,
		Invalid,
		func(r rune) rune {
			return r
		},
	)

	minimized := minimizeNFA(allocFn, merged)

	// -----------------------------------------------------------------
	// Test Suite
	// -----------------------------------------------------------------

	type testCase struct {
		input    string
		expected tokenType
	}

	tests := []testCase{
		// --- Group 1: Priority Test (Keywords) ---
		// These must be 'Keyword', not 'Literal'
		{"if", Keyword},
		{"else", Keyword},
		{"elif", Keyword},

		// --- Group 2: Literal Test ---
		// These are single letters that are *not* keywords
		{"a", Literal},
		{"b", Literal},
		{"z", Literal},
		// These are prefixes of keywords, but are valid 'Literal' matches
		{"i", Literal},
		{"e", Literal},
		{"ab", Literal},
		{"zz", Literal},
		{"A", Literal},

		// --- Group 3: Invalid Inputs ---
		// Empty string
		{"", Invalid},
		// Prefixes of keywords that are not valid tokens
		{"el", Literal},
		{"els", Literal},
		{"eli", Literal},
		// Superstrings of keywords
		{"iff", Literal},
		{"elsea", Literal},
		// Out of alphabet
		{"1", Invalid},
		{"$", Invalid},
	}

	for _, test := range tests {
		outcome, err := autarch.DFARun(minimized, []rune(test.input))
		if err != nil {
			outcome = Invalid
		}

		errMsg := fmt.Sprintf("incorrect outcome, expected=%v, got=%v, input=%q",
			test.expected, outcome, test.input)
		successMsg := fmt.Sprintf("correct outcome, expected=%v, got=%v, input=%q",
			test.expected, outcome, test.input)

		foundationtesting.Assert(outcome == test.expected, errMsg, successMsg, t)
	}
}

func produceNFAForRegex(
	allocationFn memarch.AllocationFn,
	regexLiteral string, tokType tokenType,
) *autarch.NFA[rune, tokenType] {
	nfa, err := regex.RegexToNFA(
		allocationFn,
		regexLiteral,
		tokType,
		Invalid,
	)

	if err != nil {
		panic(err)
	}
	return nfa
}

func minimizeNFA(
	allocationFn memarch.AllocationFn,
	nfa *autarch.NFA[rune, tokenType],
) *autarch.DFA[rune, tokenType] {
	dfa := autarch.NFAToDFA(
		nfa,
		1*memcore.Byte, 1*memcore.GigaByte,
		allocationFn,
		Invalid,
	)

	minimized := autarch.DFAMinimize(
		dfa,
		allocationFn,
		1*memcore.Byte, 1*memcore.GigaByte,
		Invalid,
	)

	return minimized
}
