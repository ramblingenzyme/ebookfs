package xref

import (
	"bytes"
	"fmt"

	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/cos"
)

// objStm is a decoded §7.5.7 object stream.
type objStm struct {
	data   []byte
	header []int64 // object number, offset from first, for each object
	first  int64
}

func (t *Table) compressed(num, stmNum, index int64) (cos.Object, error) {
	stm, err := t.objStm(stmNum)
	if err != nil {
		return nil, fmt.Errorf("object stream %d: %w", stmNum, err)
	}

	if index < 0 || 2*index >= int64(len(stm.header)) || stm.header[2*index] != num {
		return nil, fmt.Errorf("%w: object %d is not at index %d of object stream %d", cos.ErrMalformed, num, index, stmNum)
	}

	s := cos.NewScanner(bytes.NewReader(stm.data), int64(len(stm.data)), stm.first+stm.header[2*index+1])

	return s.Object()
}

func (t *Table) objStm(num int64) (*objStm, error) {
	if stm, ok := t.objStms[num]; ok {
		return stm, nil
	}

	// §7.5.7: an object stream is never itself compressed, which also keeps a
	// stream naming itself from recursing.
	e := t.entries[num]
	if e.kind != 1 {
		return nil, fmt.Errorf("%w: object stream %d is not an uncompressed object", cos.ErrMalformed, num)
	}

	o, err := t.uncompressed(num, e.a)
	if err != nil {
		return nil, err
	}

	s, ok := o.(*cos.Stream)
	if !ok || s.Dict["Type"] != cos.Name("ObjStm") {
		return nil, fmt.Errorf("%w: object %d is not an object stream", cos.ErrMalformed, num)
	}

	stm, err := t.decodeObjStm(s)
	if err != nil {
		return nil, err
	}

	t.objStms[num] = stm

	return stm, nil
}

func (t *Table) decodeObjStm(s *cos.Stream) (*objStm, error) {
	data, err := t.Data(s)
	if err != nil {
		return nil, err
	}

	n, first, ok := objStmHeader(s.Dict, int64(len(data)))
	if !ok {
		return nil, fmt.Errorf("%w: object stream N %v, First %v", cos.ErrMalformed, s.Dict["N"], s.Dict["First"])
	}

	header, err := cos.NewScanner(bytes.NewReader(data), first, 0).Ints(int(2 * n))
	if err != nil {
		return nil, err
	}

	return &objStm{data: data, header: header, first: first}, nil
}

// objStmHeader reads N and First. The header lies in the first First bytes,
// and each pair takes at least four, which bounds N before the header is
// allocated.
func objStmHeader(d cos.Dict, size int64) (n, first int64, ok bool) {
	n, ok1 := cos.Int(d["N"])
	first, ok2 := cos.Int(d["First"])

	return n, first, ok1 && ok2 && first >= 0 && first <= size && n >= 0 && n <= first/4+1
}
