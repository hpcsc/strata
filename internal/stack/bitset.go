package stack

import "math/bits"

type bitset []uint64

func newBitset(size int) bitset {
	return make(bitset, (size+63)/64)
}

func (b bitset) set(i int) {
	b[i/64] |= 1 << (i % 64)
}

func (b bitset) has(i int) bool {
	return b[i/64]&(1<<(i%64)) != 0
}

func (b bitset) count() int {
	n := 0
	for _, word := range b {
		n += bits.OnesCount64(word)
	}
	return n
}

func (b bitset) and(other bitset) bitset {
	out := make(bitset, len(b))
	for i := range b {
		out[i] = b[i] & other[i]
	}
	return out
}

func (b bitset) andNot(other bitset) bitset {
	out := make(bitset, len(b))
	for i := range b {
		out[i] = b[i] &^ other[i]
	}
	return out
}

func (b bitset) first() int {
	for i, word := range b {
		if word != 0 {
			return i*64 + bits.TrailingZeros64(word)
		}
	}
	return -1
}
