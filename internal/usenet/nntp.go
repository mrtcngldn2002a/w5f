// Package usenet reads text newsgroups over NNTP (read only): the groups
// you subscribe to, their threads and articles, what you have read (kept
// like a .newsrc) and a kill file for spam and posters you skip.
package usenet

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// conn is one NNTP session.
type conn struct {
	c net.Conn
	r *bufio.Reader
}

var (
	// reGroup is what a newsgroup name may be; anything else never reaches
	// the server (a line break would smuggle in another command).
	reGroup = regexp.MustCompile(`^[a-z0-9][a-z0-9+_-]*(\.[a-z0-9+_-]+)*$`)
	// reWildmat is a group search pattern.
	reWildmat = regexp.MustCompile(`^[a-z0-9.*+_-]{1,60}$`)
)

// ValidGroup reports a newsgroup name W5F will send to a server.
func ValidGroup(g string) bool { return len(g) <= 120 && reGroup.MatchString(g) }

// Dial opens a session and reads the server's greeting.
func dial(addr string, timeout time.Duration) (*conn, error) {
	c, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("news server %s: %v", addr, err)
	}
	c.SetDeadline(time.Now().Add(90 * time.Second))
	s := &conn{c: c, r: bufio.NewReaderSize(c, 64<<10)}
	code, msg, err := s.status()
	if err != nil {
		c.Close()
		return nil, err
	}
	if code != 200 && code != 201 {
		c.Close()
		return nil, fmt.Errorf("news server %s: %d %s", addr, code, msg)
	}
	return s, nil
}

func (s *conn) Close() error {
	fmt.Fprint(s.c, "QUIT\r\n")
	return s.c.Close()
}

func (s *conn) status() (int, string, error) {
	line, err := s.r.ReadString('\n')
	if err != nil {
		return 0, "", fmt.Errorf("news server: %v", err)
	}
	line = strings.TrimRight(line, "\r\n")
	code, err := strconv.Atoi(strings.SplitN(line, " ", 2)[0])
	if err != nil || len(line) < 3 {
		return 0, "", fmt.Errorf("news server: unexpected answer %q", line)
	}
	return code, strings.TrimSpace(line[3:]), nil
}

// cmd sends a command and checks for the expected status.
func (s *conn) cmd(want int, format string, args ...any) (string, error) {
	line := fmt.Sprintf(format, args...)
	if strings.ContainsAny(line, "\r\n") {
		return "", errors.New("bad NNTP command")
	}
	if _, err := fmt.Fprintf(s.c, "%s\r\n", line); err != nil {
		return "", err
	}
	code, msg, err := s.status()
	if err != nil {
		return "", err
	}
	if code != want {
		switch code {
		case 411:
			return "", fmt.Errorf("the server does not carry that group (%s)", msg)
		case 423, 430:
			return "", fmt.Errorf("that article is gone from the server (%s)", msg)
		case 480:
			return "", errors.New("this server wants a login to read; W5F reads servers that let everyone read")
		}
		return "", fmt.Errorf("news server: %d %s", code, msg)
	}
	return msg, nil
}

// lines reads a multi-line answer up to the lone dot, undoing dot-stuffing.
func (s *conn) lines(max int) ([]string, error) {
	var out []string
	size := 0
	for {
		l, err := s.r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return out, err
		}
		l = strings.TrimRight(l, "\r\n")
		if l == "." {
			return out, nil
		}
		l = strings.TrimPrefix(l, ".")
		size += len(l)
		if size <= max {
			out = append(out, l)
		}
	}
}

// Group is a newsgroup's article range on the server.
type Group struct {
	Name          string
	Count, Lo, Hi int
}

func (s *conn) group(name string) (Group, error) {
	if !ValidGroup(name) {
		return Group{}, fmt.Errorf("not a newsgroup name: %q", name)
	}
	if _, err := fmt.Fprintf(s.c, "GROUP %s\r\n", name); err != nil {
		return Group{}, err
	}
	code, msg, err := s.status()
	if err != nil {
		return Group{}, err
	}
	return groupAnswer(name, code, msg)
}

// groups asks for several groups at once (the commands are pipelined, so
// a slow line costs one round trip, not one per group).
func (s *conn) groups(names []string) ([]Group, []error) {
	gs, errs := make([]Group, len(names)), make([]error, len(names))
	var b strings.Builder
	for i, n := range names {
		if !ValidGroup(n) {
			errs[i] = fmt.Errorf("not a newsgroup name: %q", n)
			continue
		}
		fmt.Fprintf(&b, "GROUP %s\r\n", n)
	}
	if _, err := io.WriteString(s.c, b.String()); err != nil {
		for i := range errs {
			errs[i] = err
		}
		return gs, errs
	}
	for i, n := range names {
		if errs[i] != nil {
			continue
		}
		code, msg, err := s.status()
		if err != nil {
			errs[i] = err
			continue
		}
		gs[i], errs[i] = groupAnswer(n, code, msg)
	}
	return gs, errs
}

func groupAnswer(name string, code int, msg string) (Group, error) {
	switch code {
	case 211:
	case 411:
		return Group{}, fmt.Errorf("the server does not carry that group (%s)", msg)
	case 480:
		return Group{}, errors.New("this server wants a login to read; W5F reads servers that let everyone read")
	default:
		return Group{}, fmt.Errorf("news server: %d %s", code, msg)
	}
	f := strings.Fields(msg)
	if len(f) < 3 {
		return Group{}, fmt.Errorf("news server: odd GROUP answer %q", msg)
	}
	g := Group{Name: name}
	g.Count, _ = strconv.Atoi(f[0])
	g.Lo, _ = strconv.Atoi(f[1])
	g.Hi, _ = strconv.Atoi(f[2])
	return g, nil
}

// articles fetches several articles of the current group at once
// (pipelined like groups).
func (s *conn) articles(nums []int) ([][]string, []error) {
	out, errs := make([][]string, len(nums)), make([]error, len(nums))
	var b strings.Builder
	for _, n := range nums {
		fmt.Fprintf(&b, "ARTICLE %d\r\n", n)
	}
	if _, err := io.WriteString(s.c, b.String()); err != nil {
		for i := range errs {
			errs[i] = err
		}
		return out, errs
	}
	for i := range nums {
		code, msg, err := s.status()
		switch {
		case err != nil:
			errs[i] = err
		case code == 220:
			out[i], errs[i] = s.lines(2 << 20)
		case code == 423 || code == 430:
			errs[i] = fmt.Errorf("that article is gone from the server (%s)", msg)
		default:
			errs[i] = fmt.Errorf("news server: %d %s", code, msg)
		}
		if err != nil || code == 220 && errs[i] != nil { // the stream is broken
			for j := i + 1; j < len(nums); j++ {
				errs[j] = errs[i]
			}
			break
		}
	}
	return out, errs
}

// Overview is one article's summary line.
type Overview struct {
	Num        int
	Subject    string
	From       string
	Date       time.Time
	MessageID  string
	References []string
	Lines      int
}

// over lists the overview of articles lo..hi of the current group.
func (s *conn) over(lo, hi int) ([]Overview, error) {
	if lo > hi {
		return nil, nil
	}
	if _, err := s.cmd(224, "XOVER %d-%d", lo, hi); err != nil {
		return nil, err
	}
	ls, err := s.lines(32 << 20)
	if err != nil {
		return nil, err
	}
	out := make([]Overview, 0, len(ls))
	for _, l := range ls {
		f := strings.Split(l, "\t")
		if len(f) < 8 {
			continue
		}
		n, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		o := Overview{Num: n, Subject: decodeHeader(f[1]), From: decodeHeader(f[2]), MessageID: strings.TrimSpace(f[4]), References: strings.Fields(f[5])}
		o.Date = parseDate(f[3])
		o.Lines, _ = strconv.Atoi(strings.TrimSpace(f[7]))
		out = append(out, o)
	}
	return out, nil
}

// active lists the groups matching a wildmat pattern (LIST ACTIVE).
func (s *conn) active(pattern string) ([]Group, error) {
	if !reWildmat.MatchString(pattern) {
		return nil, fmt.Errorf("not a group pattern: %q (letters, digits, dots and *)", pattern)
	}
	if _, err := s.cmd(215, "LIST ACTIVE %s", pattern); err != nil {
		return nil, err
	}
	ls, err := s.lines(8 << 20)
	if err != nil {
		return nil, err
	}
	var out []Group
	for _, l := range ls {
		f := strings.Fields(l)
		if len(f) < 3 || !ValidGroup(f[0]) {
			continue
		}
		g := Group{Name: f[0]}
		g.Hi, _ = strconv.Atoi(f[1])
		g.Lo, _ = strconv.Atoi(f[2])
		if g.Hi >= g.Lo && g.Lo > 0 {
			g.Count = g.Hi - g.Lo + 1
		}
		out = append(out, g)
	}
	return out, nil
}
