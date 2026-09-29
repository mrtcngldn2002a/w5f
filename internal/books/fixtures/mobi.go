// Package fixtures builds small book files for tests: MOBI (MOBI 6, KF8,
// combo), FB2 and PDF. Nothing here is used by W5F itself.
package fixtures

import "encoding/binary"

// MOBI describes a test MOBI file.
type MOBI struct {
	Records     [][]byte // text records, already compressed as Compression says
	TextLength  int      // uncompressed text length
	Compression int      // 1 none, 2 PalmDOC, 17480 HUFF/CDIC
	Encoding    int      // 1252 or 65001
	Encryption  int
	Version     int  // 6 (MOBI) or 8 (KF8)
	Trailing    bool // add a multibyte byte and one TBS entry to each text record
	Title       string
	Author      string
	Lang        string
	Huff        [][]byte // HUFF record then CDIC records (Compression 17480)
	KF8         *MOBI    // a combo file: this KF8 half follows a BOUNDARY record
}

// Records cuts raw text into 4096-byte records and compresses each.
func Records(raw []byte, compress func([]byte) []byte) [][]byte {
	var out [][]byte
	for i := 0; i < len(raw); i += 4096 {
		chunk := raw[i:min(i+4096, len(raw))]
		if compress != nil {
			chunk = compress(chunk)
		}
		out = append(out, chunk)
	}
	return out
}

// PalmDOC compresses with literals only (valid PalmDOC without
// back-references).
func PalmDOC(raw []byte) []byte {
	plain := func(c byte) bool { return c == 0 || (c >= 0x09 && c <= 0x7F) }
	var out []byte
	for i := 0; i < len(raw); {
		if plain(raw[i]) {
			out = append(out, raw[i])
			i++
			continue
		}
		n := 0
		for i+n < len(raw) && n < 8 && !plain(raw[i+n]) {
			n++
		}
		out = append(out, byte(n))
		out = append(out, raw[i:i+n]...)
		i += n
	}
	return out
}

func u16(v int) []byte { b := make([]byte, 2); binary.BigEndian.PutUint16(b, uint16(v)); return b }
func u32(v int) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, uint32(v)); return b }

// header builds record 0 of a MOBI section (PalmDOC + MOBI + EXTH + name).
func (m MOBI) header(huffOffset, exth121 int) []byte {
	r := make([]byte, 248)
	copy(r[0:], u16(m.Compression))
	copy(r[4:], u32(m.TextLength))
	copy(r[8:], u16(len(m.Records)))
	copy(r[10:], u16(4096))
	copy(r[12:], u16(m.Encryption))
	copy(r[16:], "MOBI")
	copy(r[20:], u32(232))
	copy(r[24:], u32(2))
	copy(r[28:], u32(m.Encoding))
	copy(r[36:], u32(m.Version))
	copy(r[80:], u32(len(m.Records)+1))
	if len(m.Huff) > 0 {
		copy(r[112:], u32(huffOffset))
		copy(r[116:], u32(len(m.Huff)))
	}
	copy(r[128:], u32(0x40))
	if m.Trailing {
		copy(r[242:], u16(3))
	}
	var recs []byte
	count := 0
	add := func(typ int, data []byte) {
		recs = append(recs, u32(typ)...)
		recs = append(recs, u32(8+len(data))...)
		recs = append(recs, data...)
		count++
	}
	if m.Author != "" {
		add(100, []byte(m.Author))
	}
	if m.Title != "" {
		add(503, []byte(m.Title))
	}
	if m.Lang != "" {
		add(524, []byte(m.Lang))
	}
	if exth121 >= 0 {
		add(121, u32(exth121))
	}
	r = append(r, "EXTH"...)
	r = append(r, u32(12+len(recs))...)
	r = append(r, u32(count)...)
	r = append(r, recs...)
	nameOff := len(r)
	r = append(r, m.Title...)
	r = append(r, 0, 0)
	copy(r[84:], u32(nameOff))
	copy(r[88:], u32(len(m.Title)))
	return r
}

func (m MOBI) text() [][]byte {
	if !m.Trailing {
		return m.Records
	}
	var out [][]byte
	for _, r := range m.Records {
		// multibyte flag byte (removes itself), then a 3-byte TBS entry
		out = append(out, append(append([]byte{}, r...), 0x00, 0xAA, 0xBB, 0x83))
	}
	return out
}

// Build writes the PalmDB file.
func (m MOBI) Build() []byte {
	var recs [][]byte
	section := func(s MOBI, exth121 int) {
		huffOffset := 0
		if len(s.Huff) > 0 {
			huffOffset = 1 + len(s.Records) // relative to the section header
		}
		recs = append(recs, s.header(huffOffset, exth121))
		recs = append(recs, s.text()...)
		recs = append(recs, s.Huff...)
	}
	if m.KF8 != nil {
		kf8Header := 1 + len(m.Records) + len(m.Huff) + 1
		section(m, kf8Header)
		recs = append(recs, []byte("BOUNDARY"))
		section(*m.KF8, -1)
	} else {
		section(m, -1)
	}
	n := len(recs)
	out := make([]byte, 78+8*n+2)
	copy(out, "w5f-test-book")
	copy(out[60:], "BOOKMOBI")
	copy(out[76:], u16(n))
	off := len(out)
	for i, r := range recs {
		copy(out[78+8*i:], u32(off))
		copy(out[78+8*i+4:], u32(2*i))
		off += len(r)
	}
	for _, r := range recs {
		out = append(out, r...)
	}
	return out
}
