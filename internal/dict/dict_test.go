package dict

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// buildZip makes a tiny StarDict package in memory, including a synonym
// ("ran" → "run") and a compressed .dict.dz.
func buildZip(t *testing.T, path string) {
	t.Helper()
	words := map[string]string{
		"Run":   "<b>run</b> koşmak",
		"run":   "koşu; işletmek",
		"rune":  "run harfi",
		"apple": "elma",
	}
	keys := make([]string, 0, len(words))
	for k := range words {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return cmp(keys[i], keys[j]) < 0 })
	var dictB, idxB bytes.Buffer
	pos := map[string]int{}
	for i, k := range keys {
		pos[k] = i
		off := dictB.Len()
		dictB.WriteString(words[k])
		idxB.WriteString(k)
		idxB.WriteByte(0)
		binary.Write(&idxB, binary.BigEndian, uint32(off))
		binary.Write(&idxB, binary.BigEndian, uint32(len(words[k])))
	}
	var synB bytes.Buffer
	synB.WriteString("ran")
	synB.WriteByte(0)
	binary.Write(&synB, binary.BigEndian, uint32(pos["run"]))
	var dz bytes.Buffer
	gz := gzip.NewWriter(&dz)
	gz.Write(dictB.Bytes())
	gz.Close()

	f, _ := os.Create(path)
	zw := zip.NewWriter(f)
	add := func(name string, b []byte) {
		w, _ := zw.Create("test/" + name)
		w.Write(b)
	}
	add("t.ifo", []byte("StarDict's dict ifo file\nversion=2.4.2\nwordcount=4\nbookname=Test EN-TR\nsametypesequence=h\n"))
	add("t.idx", idxB.Bytes())
	add("t.syn", synB.Bytes())
	add("t.dict.dz", dz.Bytes())
	zw.Close()
	f.Close()
}

func TestInstallLookupSuggest(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "d.zip")
	buildZip(t, zipPath)
	dir := filepath.Join(tmp, "dict", "en-tr")
	title, err := Install(context.Background(), zipPath, dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(title, "Test EN-TR") {
		t.Errorf("title = %q", title)
	}
	d, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := d.Lookup("RUN"); len(got) != 2 {
		t.Errorf("case-insensitive lookup found %d entries", len(got))
	}
	got := d.Lookup("ran")
	if len(got) != 1 || !strings.Contains(got[0].HTML, "koşu") {
		t.Errorf("synonym lookup = %+v", got)
	}
	if s := d.Suggest("ru", 10); len(s) != 3 || s[2] != "rune" {
		t.Errorf("suggest = %v", s)
	}
	if s := d.Suggest("ra", 10); len(s) != 1 || s[0] != "run" {
		t.Errorf("suggest via synonym = %v", s)
	}
	if len(d.Lookup("zzz")) != 0 || len(d.Suggest("zz", 5)) != 0 {
		t.Error("unknown word should find nothing")
	}
}
