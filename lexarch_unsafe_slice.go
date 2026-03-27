package lexarch

import (
	"cmp"
	"unsafe"
)

/*
unsafeSliceAsRune reinterprets the observation slice as []rune without per-element boxing.

Only valid when TObservation is rune and position tracking is in rune-fast mode; the caller
(LexerSessionCreateRuneFast / matching LangSpec wiring) establishes that invariant.
*/
func unsafeSliceAsRune[TObservation cmp.Ordered](observations []TObservation) []rune {
	if len(observations) == 0 {
		return nil
	}
	head := unsafe.SliceData(observations)
	return unsafe.Slice((*rune)(unsafe.Pointer(head)), len(observations))
}

/*
unsafeSliceAsByte reinterprets the observation slice as []byte without per-element boxing.

Only valid when TObservation is byte and position tracking is in byte-fast mode.
*/
func unsafeSliceAsByte[TObservation cmp.Ordered](observations []TObservation) []byte {
	if len(observations) == 0 {
		return nil
	}
	head := unsafe.SliceData(observations)
	return unsafe.Slice((*byte)(unsafe.Pointer(head)), len(observations))
}
