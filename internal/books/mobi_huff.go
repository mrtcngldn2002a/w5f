package books

import "encoding/binary"

// huffReader decompresses HUFF/CDIC text (Huffman codes over a phrase
// dictionary whose phrases may themselves be compressed).
type huffReader struct {
	dict1   [256]huffCode
	mincode [33]uint64
	maxcode [33]uint64
	dict    []huffPhrase
}

type huffCode struct {
	codelen uint64
	term    bool
	maxcode uint64
}

type huffPhrase struct {
	data []byte
	done bool // literal, or already expanded
	busy bool // being expanded (a phrase that refers to itself)
}

// loadHuff reads the HUFF record first and the count-1 CDIC records after it.
func loadHuff(db *palmDB, first, count int) (*huffReader, error) {
	if count < 2 {
		return nil, errMOBIDamaged
	}
	rec, err := db.rec(first)
	if err != nil {
		return nil, err
	}
	if len(rec) < 24 || string(rec[:8]) != "HUFF\x00\x00\x00\x18" {
		return nil, errMOBIDamaged
	}
	off1, off2 := mu32(rec, 8), mu32(rec, 12)
	if off1+1024 > len(rec) || off2+256 > len(rec) {
		return nil, errMOBIDamaged
	}
	h := &huffReader{}
	for i := 0; i < 256; i++ {
		v := binary.BigEndian.Uint32(rec[off1+4*i:])
		codelen := uint64(v & 0x1f)
		if codelen == 0 {
			return nil, errMOBIDamaged
		}
		h.dict1[i] = huffCode{codelen: codelen, term: v&0x80 != 0, maxcode: ((uint64(v>>8) + 1) << (32 - codelen)) - 1}
	}
	for l := 1; l <= 32; l++ {
		minc := uint64(binary.BigEndian.Uint32(rec[off2+8*(l-1):]))
		maxc := uint64(binary.BigEndian.Uint32(rec[off2+8*(l-1)+4:]))
		h.mincode[l] = minc << (32 - l)
		h.maxcode[l] = ((maxc + 1) << (32 - l)) - 1
	}
	for i := 1; i < count; i++ {
		c, err := db.rec(first + i)
		if err != nil {
			return nil, err
		}
		if err := h.loadCDIC(c); err != nil {
			return nil, err
		}
	}
	return h, nil
}

func (h *huffReader) loadCDIC(rec []byte) error {
	if len(rec) < 16 || string(rec[:8]) != "CDIC\x00\x00\x00\x10" {
		return errMOBIDamaged
	}
	phrases, bits := mu32(rec, 8), mu32(rec, 12)
	if bits > 16 {
		return errMOBIDamaged
	}
	n := min(1<<bits, phrases-len(h.dict))
	for i := 0; i < n; i++ {
		if 16+2*i+2 > len(rec) {
			return errMOBIDamaged
		}
		off := mu16(rec, 16+2*i)
		if 16+off+2 > len(rec) {
			return errMOBIDamaged
		}
		blen := mu16(rec, 16+off)
		start, end := 18+off, 18+off+(blen&0x7fff)
		if end > len(rec) {
			return errMOBIDamaged
		}
		h.dict = append(h.dict, huffPhrase{data: rec[start:end], done: blen&0x8000 != 0})
	}
	return nil
}

// unpack decodes one text record.
func (h *huffReader) unpack(data []byte, depth int) ([]byte, error) {
	if depth > 32 {
		return nil, errMOBIDamaged
	}
	bitsLeft := len(data) * 8
	buf := append(append([]byte{}, data...), 0, 0, 0, 0, 0, 0, 0, 0)
	pos, n := 0, 32
	x := binary.BigEndian.Uint64(buf[pos:])
	var out []byte
	for {
		if n <= 0 {
			pos += 4
			if pos+8 > len(buf) {
				break
			}
			x = binary.BigEndian.Uint64(buf[pos:])
			n += 32
		}
		code := uint64(uint32(x >> uint(n)))
		c := h.dict1[code>>24]
		codelen, maxcode := c.codelen, c.maxcode
		if !c.term {
			for codelen < 32 && code < h.mincode[codelen] {
				codelen++
			}
			maxcode = h.maxcode[codelen]
		}
		n -= int(codelen)
		bitsLeft -= int(codelen)
		if bitsLeft < 0 {
			break
		}
		r := (maxcode - code) >> (32 - codelen)
		if r >= uint64(len(h.dict)) {
			return nil, errMOBIDamaged
		}
		p := &h.dict[r]
		if !p.done {
			if p.busy {
				return nil, errMOBIDamaged
			}
			p.busy = true
			d, err := h.unpack(p.data, depth+1)
			p.busy = false
			if err != nil {
				return nil, err
			}
			p.data, p.done = d, true
		}
		out = append(out, p.data...)
	}
	return out, nil
}
