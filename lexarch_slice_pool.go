package lexarch

import (
	"cmp"
	"reflect"
	"sync"
)

var lexemeSlicePoolByType sync.Map

func lexemeSlicePoolGet[TObservation cmp.Ordered, TToken, TTokenRole comparable]() *sync.Pool {
	sliceType := reflect.TypeOf(*new([]Lexeme[TObservation, TToken, TTokenRole]))
	if existing, ok := lexemeSlicePoolByType.Load(sliceType); ok {
		return existing.(*sync.Pool)
	}
	pool := &sync.Pool{}
	actual, _ := lexemeSlicePoolByType.LoadOrStore(sliceType, pool)
	return actual.(*sync.Pool)
}

func lexemeSlicePoolAcquire[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	minCapacity int,
) []Lexeme[TObservation, TToken, TTokenRole] {
	if minCapacity <= 0 {
		return nil
	}
	pool := lexemeSlicePoolGet[TObservation, TToken, TTokenRole]()
	candidate := pool.Get()
	if candidate != nil {
		buf := candidate.([]Lexeme[TObservation, TToken, TTokenRole])
		if cap(buf) >= minCapacity {
			return buf[:0]
		}
	}
	return make([]Lexeme[TObservation, TToken, TTokenRole], 0, minCapacity)
}

func lexemeSlicePoolRelease[TObservation cmp.Ordered, TToken, TTokenRole comparable](
	buf []Lexeme[TObservation, TToken, TTokenRole],
	minCapacity int,
) {
	if cap(buf) < minCapacity {
		return
	}
	pool := lexemeSlicePoolGet[TObservation, TToken, TTokenRole]()
	pool.Put(buf[:0])
}
