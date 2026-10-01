package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	xterm "github.com/charmbracelet/x/term"

	"w5f/internal/comics"
	"w5f/internal/comics/suwayomi"
	"w5f/internal/source"
	"w5f/internal/store"
	"w5f/internal/tui"
)

const comicsUsage = `usage:
  w5f comics list                   local series and the series Suwayomi follows
  w5f comics update                 check followed series for new chapters
  w5f comics server install         download Suwayomi-Server (official release, checksum checked)
  w5f comics server start|stop|restart|status
  w5f comics server update          install the newest release when there is one
  w5f comics settings [group]       Suwayomi's settings (the server's own; W5F's are marked)
  w5f comics set <setting> <value>  change one (on/off, a number, text, a,b,c for lists, JSON for conversions)
  w5f comics login                  the account W5F signs in with, when the server's Authentication is on`

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// registerComicsForms lets Comics pages open forms in the reader: a
// setting, the server's Authentication, W5F's own account.
func registerComicsForms() {
	open := func(href string) (*tui.Form, error) {
		env, err := source.ComicsEnv()
		if err != nil {
			return nil, err
		}
		f, err := env.Form(href)
		if err != nil {
			return nil, err
		}
		tf := &tui.Form{Title: f.Title, Intro: f.Intro, Note: f.Note, Save: f.Save}
		for _, fl := range f.Fields {
			tf.Fields = append(tf.Fields, tui.FormField{Label: fl.Label, Hint: fl.Hint, Hidden: fl.Hidden})
		}
		return tf, nil
	}
	for _, p := range []string{comics.SettingFormPrefix, comics.AuthFormPrefix, comics.LoginFormPrefix} {
		tui.RegisterForm(p, open)
	}
}

// runComics: w5f comics …
func runComics(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, comicsUsage)
		return 2
	}
	srv := source.ComicsServer()
	ctx := context.Background()
	fail := func(err error) int {
		fmt.Fprintln(os.Stderr, "w5f:", err)
		return 1
	}
	switch args[0] {
	case "server":
		action := ""
		if len(args) > 1 {
			action = args[1]
		}
		switch action {
		case "install":
			fmt.Println("Downloading Suwayomi-Server into", srv.Dir)
			name, err := suwayomi.Install(ctx, srv.Dir, func(done, total int64) {
				if total > 0 {
					fmt.Fprintf(os.Stderr, "\r%d%%", done*100/total)
				}
			})
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return fail(err)
			}
			fmt.Println("Installed", name, "(checksum OK). It needs Java 21 or newer.")
		case "start":
			fmt.Println("Starting Suwayomi (about 15 seconds)…")
			if _, err := srv.Start(ctx, 90*time.Second); err != nil {
				return fail(err)
			}
			fmt.Println("Suwayomi is running at", srv.Addr(), "(stop it with w5f comics server stop)")
		case "stop":
			if err := srv.Stop(); err != nil {
				return fail(err)
			}
			fmt.Println("Suwayomi stopped.")
		case "restart":
			fmt.Println("Restarting Suwayomi (about 15 seconds)…")
			if err := srv.Restart(ctx); err != nil {
				return fail(err)
			}
			fmt.Println("Suwayomi is running again.")
		case "update":
			fmt.Println("Installed:", firstNonEmpty(srv.Version(), "none"), "· asking GitHub for the newest release…")
			v, changed, err := srv.Update(ctx, func(done, total int64) {
				if total > 0 {
					fmt.Fprintf(os.Stderr, "\r%d%%", done*100/total)
				}
			})
			fmt.Fprintln(os.Stderr)
			if changed {
				fmt.Println("Suwayomi is now", v, "(checksum OK).")
			} else if err == nil {
				fmt.Println("Suwayomi", v, "is the newest release.")
			}
			if err != nil {
				return fail(err)
			}
		case "status":
			v, err := srv.Client().Version(ctx)
			switch {
			case err == nil:
				fmt.Println("running:", v, "at", srv.Addr())
			case srv.Jar() == "":
				fmt.Println("not installed (w5f comics server install)")
			default:
				fmt.Println("installed, not running:", srv.Jar())
			}
		default:
			fmt.Fprintln(os.Stderr, comicsUsage)
			return 2
		}
		return 0
	case "settings", "set":
		if _, err := srv.Start(ctx, 90*time.Second); err != nil {
			return fail(err)
		}
		c := srv.Client()
		st, err := c.ServerSettings(ctx)
		if err != nil {
			return fail(err)
		}
		forced := srv.Forced()
		if args[0] == "set" {
			if len(args) < 3 {
				fmt.Fprintln(os.Stderr, comicsUsage)
				return 2
			}
			s, ok := st.Get(args[1])
			if !ok {
				return fail(fmt.Errorf("this Suwayomi has no setting %q", args[1]))
			}
			if _, f := forced[s.Name]; f {
				return fail(fmt.Errorf("%s is given by W5F when the server starts", s.Name))
			}
			if s.Name == "authMode" || s.Name == "authUsername" || s.Name == "authPassword" {
				return fail(errors.New("change Authentication in the reader (Comics → Server settings → Authentication), so W5F keeps signing in"))
			}
			v, err := suwayomi.ParseValue(s, strings.Join(args[2:], " "))
			if err != nil {
				return fail(err)
			}
			if err := c.SetServerSettings(ctx, map[string]any{s.Name: v}); err != nil {
				return fail(err)
			}
			fmt.Println(s.Name, "saved.")
			return 0
		}
		want := ""
		if len(args) > 1 {
			want = args[1]
		}
		for _, g := range suwayomi.Groups {
			if want != "" && want != g.ID {
				continue
			}
			first := true
			for _, s := range st.List {
				if s.Group != g.ID {
					continue
				}
				if first {
					fmt.Printf("\n%s (%s)\n", g.Title, g.ID)
					first = false
				}
				v := suwayomi.FormatValue(s, st.Values[s.Name])
				if s.Secret && v != "" {
					v = "(set, hidden)"
				}
				note := ""
				if fv, ok := forced[s.Name]; ok {
					note = "   [W5F: " + fv + "]"
				} else if !s.Settable {
					note = "   [read only]"
				}
				fmt.Printf("  %-36s %s%s\n", s.Name, v, note)
			}
			if g.ID == "extension" {
				// Suwayomi 2.4 keeps the repositories apart from its settings.
				if _, stores, err := c.Extensions(ctx, false); err == nil {
					names := []string{}
					for _, s := range stores {
						names = append(names, s.IndexURL)
					}
					fmt.Printf("  %-36s %s\n", "extension stores", firstNonEmpty(strings.Join(names, ", "), "(none)"))
				}
			}
		}
		return 0
	case "login":
		fmt.Print("Mode the server uses (BASIC_AUTH, SIMPLE_LOGIN, UI_LOGIN): ")
		rd := bufio.NewReader(os.Stdin)
		mode, _ := rd.ReadString('\n')
		fmt.Print("Username: ")
		user, _ := rd.ReadString('\n')
		fmt.Print("Password (hidden): ")
		var pass []byte
		var err error
		if xterm.IsTerminal(os.Stdin.Fd()) {
			pass, err = xterm.ReadPassword(os.Stdin.Fd())
			fmt.Println()
		} else {
			var line string
			line, err = rd.ReadString('\n')
			pass = []byte(line)
		}
		if err != nil && len(pass) == 0 {
			return fail(err)
		}
		a := suwayomi.Auth{Mode: strings.ToUpper(strings.TrimSpace(mode)), Username: strings.TrimSpace(user), Password: strings.TrimSpace(string(pass))}
		if err := srv.SaveAuth(a); err != nil {
			return fail(err)
		}
		fmt.Println("W5F signs in to Suwayomi with this account.")
		return 0
	case "list", "update":
		db, err := store.Default()
		if err != nil {
			return fail(err)
		}
		if args[0] == "list" {
			if _, err := comics.Scan(db, comics.Root()); err != nil {
				fmt.Fprintln(os.Stderr, "w5f: comics folder:", err)
			}
			series, _ := db.ComicSeriesList()
			fmt.Printf("Local library (%s): %d series\n", comics.Root(), len(series))
			for _, s := range series {
				fmt.Printf("  %-40s %3d issues, %d unread\n", s.Name, s.Issues, s.Unread)
			}
		}
		started, err := srv.Start(ctx, 90*time.Second)
		if err != nil {
			if args[0] == "list" {
				fmt.Println("Suwayomi:", err)
				return 0
			}
			return fail(err)
		}
		if started {
			defer srv.Stop()
		}
		c := srv.Client()
		if args[0] == "update" {
			if err := c.UpdateLibrary(ctx); err != nil {
				return fail(err)
			}
			fmt.Print("Checking followed series")
			for i := 0; i < 180; i++ {
				time.Sleep(2 * time.Second)
				j, err := c.Updating(ctx)
				if err != nil || !j.IsRunning {
					break
				}
				fmt.Printf("\rChecking followed series %d/%d", j.FinishedJobs, j.TotalJobs)
			}
			fmt.Println()
		}
		lib, err := c.Library(ctx)
		if err != nil {
			return fail(err)
		}
		fmt.Printf("Following on Suwayomi: %d series\n", len(lib))
		for _, m := range lib {
			fmt.Printf("  %-40s %3d unread  (%s)\n", m.Title, m.UnreadCount, m.SourceName())
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, comicsUsage)
	return 2
}
