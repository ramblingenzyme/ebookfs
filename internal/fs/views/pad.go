package views

import (
	"fmt"
	"strconv"
	"sync/atomic"
)

// padWidth is the zero-padding that makes a lexical 9P listing sort
// numerically.
//
// The width is the digit count of the largest value filed under it, and none
// below ten, so a small listing carries no leading zeros. A width fixed at two
// breaks as soon as a listing passes 99: "100" sorts ahead of "99".
type padWidth struct{ n atomic.Int32 }

func (p *padWidth) set(max int64) {
	var w int32
	if max >= 10 {
		w = int32(len(strconv.FormatInt(max, 10)))
	}
	p.n.Store(w)
}

func (p *padWidth) format(n int64) string {
	if w := p.n.Load(); w > 0 {
		return fmt.Sprintf("%0*d", w, n)
	}
	return strconv.FormatInt(n, 10)
}

func (p *padWidth) padded() bool { return p.n.Load() > 0 }
