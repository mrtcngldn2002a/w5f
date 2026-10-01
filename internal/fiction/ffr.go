package fiction

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"w5f/internal/doc"
	"w5f/internal/sysdeps"
)

// ffrCommand runs FanFicFare (a variable so tests can use a stand-in).
var ffrCommand = []string{"fanficfare"}

// ffrTimeout bounds one FanFicFare run.
const ffrTimeout = 10 * time.Minute

func init() { extraRoutes["fiction/ffr"] = ffrRoute }

// ffrRoute saves a story as an EPUB into the Library with FanFicFare, if
// it is installed. W5F passes no logins or adult-content settings: those
// stay in FanFicFare's own personal.ini.
func ffrRoute(ctx context.Context, q url.Values, env Env) (*doc.Document, error) {
	u := strings.TrimSpace(q.Get("u"))
	exe, err := exec.LookPath(ffrCommand[0])
	if err != nil {
		return ffrInstallDoc(), nil
	}
	if u == "" {
		d := &doc.Document{Title: "FanFicFare", URL: "w5f:fiction/ffr", Origin: "local", Lang: "en"}
		d.Blocks = []doc.Block{para(plain("FanFicFare is installed ("+exe+"). Save a story from any site it supports: g → ffr <address of the story>. The EPUB goes into your Library.", 0))}
		return d, nil
	}
	tmp, err := os.MkdirTemp("", "w5f-ffr-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	cctx, cancel := context.WithTimeout(ctx, ffrTimeout)
	defer cancel()
	args := append(append([]string{}, ffrCommand[1:]...), "--non-interactive", "--format=epub", u)
	cmd := exec.CommandContext(cctx, exe, args...)
	cmd.Dir = tmp
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()
	epub := newestEPUB(tmp)
	if runErr != nil || epub == "" {
		why := "FanFicFare did not produce an EPUB"
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			why = "FanFicFare took longer than 10 minutes and was stopped"
		} else if runErr != nil {
			why = "FanFicFare failed: " + runErr.Error()
		}
		return ffrFailDoc(u, why, out.String()), nil
	}
	// Copied into the Library (the temporary folder may be on another disk).
	id, err := libraryPut(env, epub, "ffr:"+u)
	if err != nil {
		return ffrFailDoc(u, err.Error(), out.String()), nil
	}
	return openBook(ctx, env, id)
}

func newestEPUB(dir string) string {
	ms, _ := filepath.Glob(filepath.Join(dir, "*.epub"))
	best, bestT := "", time.Time{}
	for _, m := range ms {
		if st, err := os.Stat(m); err == nil && st.ModTime().After(bestT) {
			best, bestT = m, st.ModTime()
		}
	}
	return best
}

func ffrInstallDoc() *doc.Document {
	d := &doc.Document{Title: "FanFicFare is not installed", URL: "w5f:fiction/ffr", Origin: "local", Lang: "en"}
	d.Blocks = []doc.Block{
		para(plain("FanFicFare saves stories from more than a hundred fiction sites as EPUB books. W5F uses it when the fanficfare command is installed; it does not install it for you.", 0)),
		doc.Pre{Text: ffrInstall()},
		para(plain("Then: g → ffr <address of a story>. Sites that need a login or an adult-content choice are configured in FanFicFare's own personal.ini.", doc.Italic)),
	}
	return d
}

func ffrFailDoc(u, why, output string) *doc.Document {
	d := &doc.Document{Title: "FanFicFare could not save the story", URL: "w5f:fiction/ffr", Origin: "local", Lang: "en"}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(output, "\r", "")), "\n")
	if len(lines) > 15 {
		lines = lines[len(lines)-15:]
	}
	d.Blocks = []doc.Block{doc.Notice{Kind: "warn", Text: why}, para(plain("Story: ", doc.Italic), doc.Span{Text: u, Link: link(d, u, "story")})}
	if len(lines) > 0 && lines[0] != "" {
		d.Blocks = append(d.Blocks, doc.Heading{Level: 3, Text: doc.Inline{{Text: "FanFicFare said"}}}, doc.Pre{Text: strings.Join(lines, "\n")})
	}
	return d
}

// ffrInstall is how FanFicFare is installed here: with pipx where the
// system has it as a package (Debian's Python refuses pip installs), else
// with pip.
func ffrInstall() string {
	if h := sysdeps.Hint(sysdeps.Pipx); h != "" && sysdeps.Manager() != "winget" {
		return h + "\npipx install FanFicFare"
	}
	return "pip install FanFicFare"
}
