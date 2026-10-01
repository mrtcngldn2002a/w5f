package suwayomi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadServerConf(t *testing.T) {
	p := filepath.Join(t.TempDir(), "server.conf")
	os.WriteFile(p, []byte("# comment\r\nserver.ip = \"0.0.0.0\" # default: \"0.0.0.0\"\r\nserver.maxLogFileSize = \"10mb\"\r\n"+
		"server.electronPath = \"C:\\\\a\\\\b.exe\"\r\nserver.note = \"has # inside\" # a note\r\nserver.interval = 23.0\r\n"+
		"server.mode = BROWSER\r\nserver.list = [\r\n    \"a\",\r\n    \"b\" # b\r\n] # default: []\r\nserver.empty = []\r\nserver.obj = {}\r\n"), 0o644)
	c, err := ReadServerConf(p)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"ip": "0.0.0.0", "maxLogFileSize": "10mb", "electronPath": `C:\a\b.exe`, "note": "has # inside",
		"interval": "23.0", "mode": "BROWSER", "list": "a, b", "empty": "", "obj": "{}"} {
		if c[k] != want {
			t.Errorf("%s = %q, want %q", k, c[k], want)
		}
	}
	if !SameValue(Setting{Type: "Int"}, "23.0", "23") || SameValue(Setting{Type: "Int"}, "23.5", "23") ||
		!SameValue(Setting{Kind: "OBJECT", List: true}, "{}", "[]") {
		t.Error("SameValue")
	}
	for _, pair := range [][2]string{{"0s", "PT0S"}, {"5m", "PT5M"}, {"60d", "PT1440H"}, {"60d", "P60D"}, {"true", "on"}} {
		typ := "String"
		if pair[0] == "true" {
			typ = "Boolean"
		}
		if !SameValue(Setting{Type: typ}, pair[0], pair[1]) {
			t.Errorf("%s = %s", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]string{{"5m", "PT6M"}, {"10mb", "PT10M"}, {"abc", "PT0S"}} {
		if SameValue(Setting{Type: "String"}, pair[0], pair[1]) {
			t.Errorf("%s ≠ %s", pair[0], pair[1])
		}
	}
}
