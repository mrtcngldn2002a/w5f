package fixtures

// HuffEntry is one CDIC dictionary phrase: literal text, or HUFF-encoded
// symbols (a recursive phrase) when Literal is false.
type HuffEntry struct {
	Data    []byte
	Literal bool
}

// Huff builds a HUFF record and one CDIC record for up to 256 phrases.
// Every code is 8 bits long: byte b decodes to phrase 255-b.
func Huff(entries []HuffEntry) [][]byte {
	huff := []byte("HUFF\x00\x00\x00\x18")
	huff = append(huff, u32(24)...)
	huff = append(huff, u32(24+1024)...)
	for len(huff) < 24 {
		huff = append(huff, 0)
	}
	for b := 0; b < 256; b++ {
		huff = append(huff, u32(255<<8|0x80|8)...) // maxcode 255, terminal, length 8
	}
	huff = append(huff, make([]byte, 64*4)...)
	cdic := []byte("CDIC\x00\x00\x00\x10")
	cdic = append(cdic, u32(len(entries))...)
	cdic = append(cdic, u32(8)...)
	var table, body []byte
	base := 2 * len(entries)
	for _, e := range entries {
		table = append(table, u16(base+len(body))...)
		l := len(e.Data)
		if e.Literal {
			l |= 0x8000
		}
		body = append(body, u16(l)...)
		body = append(body, e.Data...)
	}
	cdic = append(cdic, table...)
	cdic = append(cdic, body...)
	return [][]byte{huff, cdic}
}

// HuffEncode encodes phrase numbers with the codes Huff builds.
func HuffEncode(symbols ...int) []byte {
	out := make([]byte, len(symbols))
	for i, s := range symbols {
		out[i] = byte(255 - s)
	}
	return out
}
