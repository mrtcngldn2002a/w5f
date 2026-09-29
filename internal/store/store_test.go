package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestItemsLifecycle(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	for i, title := range []string{"old", "new"} {
		isNew, err := db.UpsertItem(Item{FeedID: "f", GUID: title, Title: title, Published: now.Add(time.Duration(i) * time.Hour), Content: "short"})
		if err != nil || !isNew {
			t.Fatalf("insert %s: %v %v", title, isNew, err)
		}
	}
	// Re-sync: not new, longer content wins, read state kept.
	items, _ := db.Items(Query{Feeds: []string{"f"}})
	if len(items) != 2 || items[0].Title != "new" {
		t.Fatalf("order: %+v", items)
	}
	if err := db.SetRead(items[0].ID, true); err != nil {
		t.Fatal(err)
	}
	isNew, _ := db.UpsertItem(Item{FeedID: "f", GUID: "new", Title: "new!", Content: "a much longer body"})
	if isNew {
		t.Error("existing item reported as new")
	}
	it, _ := db.Item(items[0].ID)
	if !it.Read || it.Title != "new!" || it.Content != "a much longer body" {
		t.Errorf("after resync: %+v", it)
	}
	unread, _ := db.Items(Query{Unread: true})
	if len(unread) != 1 || unread[0].Title != "old" {
		t.Errorf("unread = %+v", unread)
	}
	if s, _ := db.ToggleStar(it.ID); !s {
		t.Error("star toggle")
	}
	c, _ := db.Counts()
	if c["f"] != [2]int{1, 2} || db.StarredCount() != 1 {
		t.Errorf("counts = %v starred=%d", c, db.StarredCount())
	}
	if err := db.MarkAllRead([]string{"f"}); err != nil {
		t.Fatal(err)
	}
	if u, _ := db.Items(Query{Unread: true}); len(u) != 0 {
		t.Error("mark all read failed")
	}
	db.SetFeed(FeedState{ID: "f", URL: "https://x/feed", LastSync: now, LastOK: now})
	db.SetFeed(FeedState{ID: "f", URL: "https://x/feed", LastSync: now.Add(time.Minute), Error: "boom"})
	fs := db.Feed("f")
	if fs.Error != "boom" || fs.LastOK.IsZero() {
		t.Errorf("feed state %+v", fs)
	}
}
