package doc

import "testing"

func para(s string) Block { return Paragraph{Text: Inline{{Text: s}}} }

func TestTidyGutenberg(t *testing.T) {
	d := &Document{Title: "The Yellow Wallpaper | Project Gutenberg", Blocks: []Block{
		para("The Project Gutenberg eBook of The Yellow Wallpaper"),
		para("*** START OF THE PROJECT GUTENBERG EBOOK 1952 *** [Illustration]"),
		Image{Alt: "[Illustration]"},
		para("It is very seldom that mere ordinary people"),
		para("[Illustration]"),
		Image{Alt: "[Illustration]", Caption: "The paper"},
		para("Why, there’s John at the door!"),
		para("*** END OF THE PROJECT GUTENBERG EBOOK 1952 ***"),
		para("Section 1. General Terms of Use"),
	}}
	TidyGutenberg(d)
	if d.Title != "The Yellow Wallpaper" {
		t.Errorf("title %q", d.Title)
	}
	if len(d.Blocks) != 3 {
		t.Fatalf("kept %d blocks, want the story's 3: %#v", len(d.Blocks), d.Blocks)
	}
	if blockText(d.Blocks[0]) != "It is very seldom that mere ordinary people" {
		t.Errorf("first block %q", blockText(d.Blocks[0]))
	}
	if img, ok := d.Blocks[1].(Image); !ok || img.Caption != "The paper" {
		t.Errorf("a captioned picture should stay, got %#v", d.Blocks[1])
	}
}

func TestTidyGutenbergLeavesOtherPages(t *testing.T) {
	d := &Document{Title: "A page", Blocks: []Block{para("one"), para("[Illustration]")}}
	TidyGutenberg(d)
	if len(d.Blocks) != 2 {
		t.Errorf("a page without Gutenberg's markers changed: %#v", d.Blocks)
	}
}
