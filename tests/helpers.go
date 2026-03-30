package tests

import (
	"fmt"
	"strings"
)

const errMarker = "‸" // U+2038 CARET, highly unlikely to appear in actual code

func parseMarkedInput(marked string) (clean string, offset uint32) {
	idx := strings.Index(marked, errMarker)
	if idx == -1 {
		panic(fmt.Sprintf("Test setup flaw: missing marker '%s' in %q", errMarker, marked))
	}
	
	clean = strings.Replace(marked, errMarker, "", 1)
	return clean, uint32(idx)
}