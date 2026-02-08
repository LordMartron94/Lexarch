package lexarch

/*
TokenResolutionStepFn performs inline resolution during scanning, updating the best match
incrementally without collecting candidates. This eliminates allocations in the hot path.

Parameters:
- currentToken: The token from the current accepting state
- currentEnd: The end position of the current match
- currentPriority: The priority of the current token (0 if not specified)
- bestToken: The current best token (errorToken if none yet)
- bestEnd: The end position of the best match
- bestPriority: The priority of the best token (0 if not specified)

Returns:
- newBestToken: The updated best token
- newBestEnd: The updated best end position
- updated: Whether the best match was updated

Use cases:
- Zero-allocation token resolution during scanning
- Inline best-match tracking without candidate collection
- Performance-critical lexing operations

Time complexity: O(1) - single comparison operation
Space complexity: O(1) - no allocations

Prerequisites:
- errorToken must be distinct from valid tokens
- Priorities should be non-negative (0 means no priority)

Edge cases:
- Returns bestToken unchanged if current is not better
- Handles errorToken correctly (never selected as best)
- Priority 0 means no priority (treated as equal priority)
*/
type TokenResolutionStepFn[TToken comparable] func(
	currentToken TToken,
	currentEnd int,
	currentPriority int,
	bestToken TToken,
	bestEnd int,
	bestPriority int,
) (newBestToken TToken, newBestEnd int, updated bool)

/*
TokenResolutionStepLongest performs inline longest-match resolution.

Updates the best match if the current token spans a longer input region
(larger end position).

Use cases:
- Default token resolution during scanning
- Longest match strategy (keywords over identifiers)
- Zero-allocation token selection

Time complexity: O(1)
Space complexity: O(1)

Edge cases:
- Updates if currentEnd > bestEnd
- Leaves best unchanged otherwise
*/
func TokenResolutionStepLongest[TToken comparable](
	currentToken TToken,
	currentEnd int,
	currentPriority int,
	bestToken TToken,
	bestEnd int,
	bestPriority int,
) (newBestToken TToken, newBestEnd int, updated bool) {

	if currentEnd > bestEnd {
		return currentToken, currentEnd, true
	}

	return bestToken, bestEnd, false
}

/*
TokenResolutionStepShortest performs inline shortest-match resolution.

Updates the best match if the current token spans a shorter input region
(smaller end position).

Use cases:
- Shortest match strategy
- Preferring shorter tokens over longer ones
- Zero-allocation token selection

Time complexity: O(1)
Space complexity: O(1)

Edge cases:
- Updates if currentEnd < bestEnd
- Leaves best unchanged otherwise
*/
func TokenResolutionStepShortest[TToken comparable](
	currentToken TToken,
	currentEnd int,
	currentPriority int,
	bestToken TToken,
	bestEnd int,
	bestPriority int,
) (newBestToken TToken, newBestEnd int, updated bool) {

	if currentEnd < bestEnd {
		return currentToken, currentEnd, true
	}

	return bestToken, bestEnd, false
}

/*
TokenResolutionStepFirst performs inline first-match resolution.

Keeps the earliest encountered valid token and ignores subsequent ones.

Use cases:
- Rule-order priority (first rule wins)
- Deterministic token selection
- Zero-allocation token selection

Time complexity: O(1)
Space complexity: O(1)

Edge cases:
- Never updates once a best match is chosen
*/
func TokenResolutionStepFirst[TToken comparable](
	currentToken TToken,
	currentEnd int,
	currentPriority int,
	bestToken TToken,
	bestEnd int,
	bestPriority int,
) (newBestToken TToken, newBestEnd int, updated bool) {
	return currentToken, currentEnd, true
}

/*
TokenResolutionStepPriority performs inline priority-based resolution.

Higher priority tokens win. If priorities are equal, the longer match
is selected as a tiebreaker.

Use cases:
- Priority-based token selection
- Custom token precedence rules
- Zero-allocation token selection

Time complexity: O(1)
Space complexity: O(1)

Edge cases:
- Higher priority always wins
- If priorities equal, longer match wins
- Otherwise best remains unchanged
*/
func TokenResolutionStepPriority[TToken comparable](
	currentToken TToken,
	currentEnd int,
	currentPriority int,
	bestToken TToken,
	bestEnd int,
	bestPriority int,
) (newBestToken TToken, newBestEnd int, updated bool) {

	if currentPriority > bestPriority {
		return currentToken, currentEnd, true
	}

	if currentPriority == bestPriority && currentEnd > bestEnd {
		return currentToken, currentEnd, true
	}

	return bestToken, bestEnd, false
}
