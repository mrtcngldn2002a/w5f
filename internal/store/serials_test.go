package store

import "testing"

func TestSerialsMergeFollowAndProgress(t *testing.T) {
	db := tmpDB(t)
	id, err := db.UpsertSerial(Serial{Kind: "royalroad", URL: "https://rr/f/1", Title: "Mother", Author: "nobody103"})
	if err != nil {
		t.Fatal(err)
	}
	chs := []SerialChapter{{Title: "One", URL: "u1"}, {Title: "Two", URL: "u2"}}
	if n, err := db.MergeChapters(id, chs); err != nil || n != 2 {
		t.Fatalf("first merge: %d %v", n, err)
	}
	db.SetFollowed(id, true)
	db.SaveSerialProgress(id, 1, 0.5)
	db.MarkSeen(id)
	// Refresh: metadata changes, follow and progress stay.
	if id2, _ := db.UpsertSerial(Serial{Kind: "royalroad", URL: "https://rr/f/1", Title: "Mother of Learning"}); id2 != id {
		t.Fatalf("upsert changed id: %d", id2)
	}
	// u1 removed on the site, u2 renamed, u3 new.
	if n, err := db.MergeChapters(id, []SerialChapter{{Title: "Two (edited)", URL: "u2"}, {Title: "Three", URL: "u3"}}); err != nil || n != 1 {
		t.Fatalf("second merge: %d %v", n, err)
	}
	got, _ := db.SerialChapters(id)
	if len(got) != 3 || got[0].URL != "u1" || got[1].Title != "Two (edited)" || got[2].URL != "u3" || got[2].N != 2 {
		t.Fatalf("chapters: %+v", got)
	}
	if _, err := db.MergeChapters(id, nil); err == nil {
		t.Error("an empty chapter list must be refused")
	}
	s, err := db.Serial(id)
	if err != nil || s.Title != "Mother of Learning" || !s.Followed || s.Chapter != 1 || s.Pos != 0.5 || s.Chapters != 3 || s.Seen != 2 {
		t.Fatalf("serial: %v %+v", err, s)
	}
	other, _ := db.UpsertSerial(Serial{Kind: "ao3", URL: "https://ao3/w/2", Title: "Unfollowed"})
	db.MergeChapters(other, chs)
	if n, _ := db.NewChapterTotal(); n != 1 {
		t.Errorf("new chapters = %d, want 1 (only followed serials)", n)
	}
	if fs, _ := db.Serials(true); len(fs) != 1 || fs[0].ID != id {
		t.Errorf("followed: %+v", fs)
	}
	if s, ok := db.SerialByURL("https://ao3/w/2"); !ok || s.ID != other {
		t.Error("SerialByURL")
	}
	db.SetChecked(id, "Royal Road: verification")
	if s, _ := db.Serial(id); s.CheckError == "" || s.Checked.IsZero() {
		t.Errorf("checked: %+v", s)
	}
}
