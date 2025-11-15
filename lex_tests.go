package lexarch

import (
	"fmt"
	foundationtesting "foundation/testing"
	"memcore"
	"memforge"
	"testing"
)

type tokenType int

const (
	// Priority order
	Keyword tokenType = iota // 0
	Literal                  // 1

	// --- NEW ARCHITECTURE ---
	// Add a distinct type for skippable tokens
	Whitespace // 2

	// Invalid is now *only* for dead states
	Invalid // 3
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

	// --- Lexer Definition ---
	keywords := "if|else|elif"
	literal := "[a-zA-Z]+"
	whitespace := "[\t\n ]+"

	// Create the lexer using the correct Invalid token
	lexer := TextLexerCreate(
		allocFn,
		Invalid, // Pass the true 'Invalid' type
		1*memcore.Byte, 1*memcore.GigaByte,

		// Rules are in priority order
		TextLexerRuleCreate(keywords, Keyword),
		TextLexerRuleCreate(literal, Literal),

		// Whitespace rule now maps to the 'Whitespace' token
		TextLexerRuleCreate(whitespace, Whitespace),
	)

	// -----------------------------------------------------------------
	// Test Suite
	// -----------------------------------------------------------------

	type testCase struct {
		name     string
		input    string
		expected []Token[tokenType] // Expected *non-whitespace* tokens
		expError bool               // True if an error is expected
	}

	// This test table checks streams of tokens
	tests := []testCase{
		// --- Group 1: Single Valid Tokens ---
		{"Keyword 'if'", "if", []Token[tokenType]{{Lexeme: "if", TokenType: Keyword}}, false},
		{"Keyword 'else'", "else", []Token[tokenType]{{Lexeme: "else", TokenType: Keyword}}, false},
		{"Literal 'a'", "a", []Token[tokenType]{{Lexeme: "a", TokenType: Literal}}, false},
		{"Literal 'i'", "i", []Token[tokenType]{{Lexeme: "i", TokenType: Literal}}, false},
		{"Literal 'long'", "longword", []Token[tokenType]{{Lexeme: "longword", TokenType: Literal}}, false},
		{"Literal 'iff'", "iff", []Token[tokenType]{{Lexeme: "iff", TokenType: Literal}}, false},
		{"Literal 'elsea'", "elsea", []Token[tokenType]{{Lexeme: "elsea", TokenType: Literal}}, false},

		// --- Group 2: Empty and Whitespace ---
		{"Empty", "", []Token[tokenType]{}, false},
		{"Whitespace only", " \t \n ", []Token[tokenType]{}, false}, // Expects an empty *filtered* list

		// --- Group 3: Multi-Token Streams ---
		{"Keywords", "if else", []Token[tokenType]{
			{Lexeme: "if", TokenType: Keyword},
			{Lexeme: "else", TokenType: Keyword},
		}, false},
		{"Mixed", "if myVar else", []Token[tokenType]{
			{Lexeme: "if", TokenType: Keyword},
			{Lexeme: "myVar", TokenType: Literal},
			{Lexeme: "else", TokenType: Keyword},
		}, false},
		{"No Space", "ifelif", []Token[tokenType]{
			{Lexeme: "ifelif", TokenType: Literal},
		}, false},

		// --- Group 4: Invalid Inputs (Stuck Lexer) ---
		{"Invalid char '$'", "$", nil, true},
		{"Invalid char '1'", "1", nil, true},
		{"Valid then invalid", "if $ else", nil, true},
	}

	for _, test := range tests {
		// Shadowing 'test' for the sub-test
		test := test
		t.Run(test.name, func(t *testing.T) {

			allTokens, err := TextLexerLex(lexer, test.input)

			// 1. Check for expected error
			if test.expError {
				errMsg := fmt.Sprintf("expected an error for input %q, but got nil", test.input)
				successMsg := fmt.Sprintf("correctly got an error for input %q", test.input)
				foundationtesting.Assert(err != nil, errMsg, successMsg, t)
				return // Error was expected, test passes
			}

			// 2. Check for unexpected error
			errMsg := fmt.Sprintf("did not expect an error for input %q, but got: %v", test.input, err)
			successMsg := fmt.Sprintf("correctly got no error for input %q", test.input)
			foundationtesting.Assert(err == nil, errMsg, successMsg, t)

			// 3. Filter out 'Whitespace' tokens
			filteredTokens := make([]Token[tokenType], 0, len(allTokens))
			for _, tok := range allTokens {
				if tok.TokenType != Whitespace {
					filteredTokens = append(filteredTokens, tok)
				}
			}

			// 4. Check token slice length
			errMsg = fmt.Sprintf("wrong number of tokens for input %q:\n  Expected: %d\n  Got:      %d",
				test.input, len(test.expected), len(filteredTokens))
			successMsg = fmt.Sprintf("correct number of tokens for input %q (%d)", test.input, len(filteredTokens))
			lenOk := len(filteredTokens) == len(test.expected)
			foundationtesting.Assert(lenOk, errMsg, successMsg, t)

			if !lenOk {
				return
			}

			// 5. Check token content
			for i := range filteredTokens {
				expectedToken := test.expected[i]
				gotToken := filteredTokens[i]

				lexOk := gotToken.Lexeme == expectedToken.Lexeme
				typeOk := gotToken.TokenType == expectedToken.TokenType

				errMsg = fmt.Sprintf("token %d mismatch for input %q:\n  Expected: Lexeme=%q, Type=%v\n  Got:      Lexeme=%q, Type=%v",
					i, test.input, expectedToken.Lexeme, expectedToken.TokenType, gotToken.Lexeme, gotToken.TokenType)
				successMsg = fmt.Sprintf("token %d matched for input %q", i, test.input)

				foundationtesting.Assert(lexOk && typeOk, errMsg, successMsg, t)
			}
		})
	}
}
