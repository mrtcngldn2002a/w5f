package fixtures

import (
	"archive/zip"
	"bytes"

	"golang.org/x/text/encoding/charmap"
)

// FB2 wraps an FB2 document (everything inside <FictionBook>) with the XML
// declaration; encoding "windows-1251" encodes the text accordingly.
func FB2(inner, encoding string) []byte {
	doc := `<?xml version="1.0" encoding="` + encoding + `"?>` + "\n" +
		`<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">` +
		inner + `</FictionBook>`
	if encoding == "windows-1251" {
		b, err := charmap.Windows1251.NewEncoder().Bytes([]byte(doc))
		if err != nil {
			panic(err)
		}
		return b
	}
	return []byte(doc)
}

// Zip puts one file into a zip archive.
func Zip(name string, data []byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(name)
	w.Write(data)
	zw.Close()
	return buf.Bytes()
}
