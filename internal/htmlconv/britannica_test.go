package htmlconv

import (
	"net/url"
	"strings"
	"testing"
)

// This structure matches Britannica's generated Top Questions module; the
// article and answer text are synthetic to keep the fixture small.
const britannicaAIModule = `<div class="ai-qna-module ai-top-questions-module module-spacing">
<div>Top Questions</div><ul>
<li class="ai-qna-item ai-tq-item"><div class="ai-qna-question">Generated question one?</div>
<div class="ai-qna-answer"><div class="answer-content"><p>Generated answer one.</p></div>
<div class="disclaimer"><span>This answer is created from Britannica articles <a href="/about-britannica-ai">using AI</a>. AI can make mistakes, so verify using Britannica articles.</span></div></div></li>
<li class="ai-qna-item ai-tq-item"><div class="ai-qna-question">Generated question two?</div>
<div class="ai-qna-answer"><p>Generated answer two.</p><div class="disclaimer">This answer is created from Britannica articles using AI. AI can make mistakes, so verify using Britannica articles.</div></div></li>
</ul><button>Show more</button></div>`

func TestArticleBritannicaOmitsAIQuestions(t *testing.T) {
	for _, short := range []bool{false, true} {
		name := "long article"
		prose := strings.Repeat("Editorial discussion of professional responsibility, its history, and its principles. ", 12)
		if short {
			name = "short article fallback"
			prose = "Short editorial article."
		}
		t.Run(name, func(t *testing.T) {
			body := []byte(`<html lang="en"><head><title>Legal ethics</title></head><body><main><article><h1>Legal ethics</h1>` + britannicaAIModule +
				`<p>` + prose + `</p><section class="top-questions"><h2>Editorial question?</h2><p>Human editorial answer.</p></section></article></main></body></html>`)
			d, err := Article(body, "https://www.britannica.com/topic/legal-ethics")
			if err != nil {
				t.Fatal(err)
			}
			text := allText(d.Blocks)
			for _, unwanted := range []string{"Generated question", "Generated answer", "This answer is created", "AI can make mistakes", "Top Questions", "Show more"} {
				if strings.Contains(text, unwanted) {
					t.Errorf("AI module leaked: %q", unwanted)
				}
			}
			for _, want := range []string{prose, "Editorial question?", "Human editorial answer."} {
				if !strings.Contains(text, want) {
					t.Errorf("editorial content lost: %q; got %s", want, text)
				}
			}
			if d.Title != "Legal ethics" || d.Lang != "en" {
				t.Errorf("metadata changed: title=%q lang=%q", d.Title, d.Lang)
			}
			for _, link := range d.Links {
				if strings.Contains(link.Href, "about-britannica-ai") {
					t.Error("AI source link leaked")
				}
			}
		})
	}
}

func TestBritannicaAIFilterScope(t *testing.T) {
	for _, host := range []string{"britannica.com", "WWW.BRITANNICA.COM", "example.com", "britannica.com.example.com"} {
		t.Run(host, func(t *testing.T) {
			u, _ := url.Parse("https://" + host + "/topic/example")
			body := []byte("<html><body>" + britannicaAIModule + "<p>Editorial text.</p></body></html>")
			got := string(stripBritannicaAI(body, u))
			if host == "britannica.com" || host == "WWW.BRITANNICA.COM" {
				if strings.Contains(got, "Generated answer") || !strings.Contains(got, "Editorial text.") {
					t.Fatal("expected only AI module to be removed")
				}
			} else if got != string(body) {
				t.Fatal("unrelated host was modified")
			}
		})
	}
	u, _ := url.Parse("https://www.britannica.com/topic/example")
	plain := []byte("<p>Ordinary editorial page without AI.</p>")
	if string(stripBritannicaAI(plain, u)) != string(plain) {
		t.Fatal("page without AI was modified")
	}
}
