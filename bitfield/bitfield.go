package bitfield

import "slices"

type Bitfield []byte

func (b *Bitfield) Has(i int) bool {
	bi := i / 8
	if bi >= len(*b) {
		return false
	}
	bj := 7 - (i % 8)
	return (*b)[bi]&(1<<bj) != 0
}

func (b *Bitfield) Add(i int) {
	bi := i / 8
	if bi >= len(*b) {
		*b = slices.Grow(*b, 1+bi-len(*b))
		*b = (*b)[:bi+1]
	}
	bj := 7 - (i % 8)
	(*b)[bi] |= 1 << bj
}

func (b *Bitfield) Items() []int {
	var res []int
	for bi, mask := range *b {
		for j := range 8 {
			bj := 7 - j
			if mask&(1<<bj) != 0 {
				res = append(res, bi*8+j)
			}
		}
	}
	return res
}

func (b *Bitfield) Difference(a Bitfield) Bitfield {
	res := make(Bitfield, len(*b))
	for i := range *b {
		res[i] = (*b)[i]
		if i < len(a) {
			res[i] -= (*b)[i] & a[i]
		}
	}
	return res
}
