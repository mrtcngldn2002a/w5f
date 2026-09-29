package store

import "testing"

func TestHistory(t *testing.T) {
	db := tmpDB(t)
	db.Visit("https://a", "A", "web", "WEB·A·home")
	db.Visit("https://b", "B", "scp", "FIC·SCP·173")
	db.Visit("https://a", "A again", "web", "WEB·A·home")
	vs, err := db.History(0, 0)
	if err != nil || len(vs) != 2 || vs[0].Target != "https://a" || vs[0].Opens != 2 || vs[0].Title != "A again" {
		t.Fatalf("history: %v %+v", err, vs)
	}
	db.SavePos("https://a", 0.4)
	db.SavePos("https://b", 0.99)
	db.Visit("w5f:book/1/ch/2", "Book", "book", "BK·LOC·1")
	db.SavePos("w5f:book/1/ch/2", 0.5)
	un, _ := db.Unfinished(5)
	if len(un) != 1 || un[0].Target != "https://a" || un[0].Pos != 0.4 {
		t.Errorf("unfinished (books are listed from the library instead): %+v", un)
	}
	if vs, _ := db.History(1, 1); len(vs) != 1 || vs[0].Target != "https://a" {
		t.Errorf("paging: %+v", vs)
	}
}
