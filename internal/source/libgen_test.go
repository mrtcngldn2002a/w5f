package source

import "testing"

func TestLibgenShortcuts(t *testing.T) {
	for in, want := range map[string]string{"libgen": "w5f:books/libgen", "lg": "w5f:books/libgen", "libgen history": "w5f:books/libgen?q=history", "lg title:Dracula": "w5f:books/libgen?q=title%3ADracula", "libgen-status": "w5f:books/libgen/status", "libgen-md5 aaaa": "w5f:books/libgen/item?md5=aaaa", "libgen-link aaaa": "w5f:books/libgen/link?md5=aaaa"} {
		if got := Resolve(in); got != want {
			t.Errorf("%q = %q, want %q", in, got, want)
		}
	}
}
