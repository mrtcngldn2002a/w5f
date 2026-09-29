package discover

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"w5f/internal/doc"
	"w5f/internal/store"
)

type fakeFamily struct {
	name string
	fail bool
}

func (f fakeFamily) Name() string { return f.name }
func (f fakeFamily) Draw(ctx context.Context, env Env) (Draw, error) {
	if f.fail {
		return Draw{}, errors.New("down")
	}
	return Draw{Target: "https://example.org/" + f.name, Family: f.name, Why: f.name + "/page"}, nil
}

func testEnv(t *testing.T, fams ...Family) Env {
	t.Helper()
	old := families
	families = fams
	t.Cleanup(func() { families = old })
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return Env{DB: db, Load: func(ctx context.Context, target string) (*doc.Document, error) {
		return &doc.Document{URL: target, Title: target, Blocks: []doc.Block{doc.Paragraph{Text: doc.Inline{{Text: "body"}}}}}, nil
	}}
}

func TestTenDrawsCoverSixFamilies(t *testing.T) {
	var fams []Family
	for _, n := range []string{"esoteric", "textfiles", "folklore", "encyclopedic", "weird", "smallweb", "fiction"} {
		fams = append(fams, fakeFamily{name: n, fail: n == "smallweb"})
	}
	env := testEnv(t, fams...)
	ctx := context.Background()
	for round := 0; round < 3; round++ {
		seen := map[string]bool{}
		last := ""
		for i := 0; i < 10; i++ {
			d, page, err := Next(ctx, env)
			if err != nil {
				t.Fatal(err)
			}
			if d.Family == last {
				t.Errorf("the same family twice in a row: %s", d.Family)
			}
			last = d.Family
			seen[d.Family] = true
			if d.Family == "smallweb" {
				t.Error("a failing family must be skipped")
			}
			if !strings.HasPrefix(page.Blocks[0].(doc.Notice).Text, "Deep random · "+d.Why) {
				t.Errorf("why line: %+v", page.Blocks[0])
			}
			if len(page.Links) != 0 {
				t.Error("the why line carries no w5f: link (the page is a web page)")
			}
		}
		if len(seen) < 6 {
			t.Errorf("round %d: ten draws covered %d families: %v", round, len(seen), seen)
		}
	}
}

func TestAllFamiliesFailing(t *testing.T) {
	env := testEnv(t, fakeFamily{name: "a", fail: true}, fakeFamily{name: "b", fail: true})
	if _, _, err := Next(context.Background(), env); err == nil {
		t.Error("no family worked: an error is expected")
	}
}
