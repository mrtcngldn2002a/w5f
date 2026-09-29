package fiction

import (
	"context"
	"strings"
	"testing"
)

func TestSyncFollowedAndHome(t *testing.T) {
	fa := &fakeAdapter{chapters: 2}
	env := testEnv(t, fa)
	ctx := context.Background()
	for _, u := range []string{"https://fake.test/story/1", "https://fake.test/story/2"} {
		if _, err := OpenURL(ctx, env, mustURL(u)); err != nil {
			t.Fatal(err)
		}
		s, _ := env.DB.SerialByURL(u)
		ToggleFollow(env.DB, s.ID)
	}
	fa.chapters, fa.failURL = 4, "https://fake.test/story/2"
	rep := SyncFollowed(ctx, env, nil)
	if rep.Checked != 2 || rep.New != 2 || rep.Errors != 1 {
		t.Fatalf("report: %+v", rep)
	}
	if n, _ := env.DB.NewChapterTotal(); n != 2 {
		t.Errorf("new chapters: %d", n)
	}
	broken, _ := env.DB.SerialByURL("https://fake.test/story/2")
	if broken.CheckError == "" || broken.Chapters != 2 {
		t.Errorf("failing serial: %+v", broken)
	}
	home, err := Route(ctx, "w5f:fiction", env)
	if err != nil {
		t.Fatal(err)
	}
	txt := flat(home)
	if !strings.Contains(txt, "Following (2 new)") || !strings.Contains(txt, "2 new") || strings.Contains(txt, "Questionable Questing") {
		t.Errorf("home:\n%s", txt)
	}
	env.Mature = true
	home, _ = Route(ctx, "w5f:fiction", env)
	if !strings.Contains(flat(home), "Questionable Questing") {
		t.Error("mature forums must show when enabled")
	}
	fol, _ := Route(ctx, "w5f:following", env)
	if !strings.Contains(flat(fol), "✗") {
		t.Errorf("following page must show the failed check:\n%s", flat(fol))
	}
	// Opening the serial page marks its chapters seen.
	ok, _ := env.DB.SerialByURL("https://fake.test/story/1")
	Route(ctx, serialHref(ok.ID, ""), env)
	if n, _ := env.DB.NewChapterTotal(); n != 0 {
		t.Errorf("after opening: %d new", n)
	}
}
