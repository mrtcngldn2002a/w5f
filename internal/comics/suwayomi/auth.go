package suwayomi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Auth is how W5F signs in to a server whose Authentication is on: the
// mode the server uses and the account set for it.
type Auth struct {
	Mode     string `json:"mode"` // NONE, BASIC_AUTH, SIMPLE_LOGIN, UI_LOGIN
	Username string `json:"username"`
	Password string `json:"password"`
}

// On reports an Auth that signs in.
func (a Auth) On() bool { return a.Mode != "" && a.Mode != "NONE" }

// ErrUnauthorized means the server wants W5F to sign in, and W5F has no
// (right) account for it.
var ErrUnauthorized = errors.New("the Suwayomi server asks W5F to sign in (Authentication is on)")

// authFile keeps the account W5F signs in with, readable by the user alone.
func (s Server) authFile() string { return filepath.Join(s.Dir, "w5f-auth.json") }

// Auth is the account W5F signs in with (none: Authentication off).
func (s Server) Auth() Auth {
	var a Auth
	if b, err := os.ReadFile(s.authFile()); err == nil {
		_ = json.Unmarshal(b, &a)
	}
	return a
}

// SaveAuth keeps the account W5F signs in with; Mode NONE forgets it.
func (s Server) SaveAuth(a Auth) error {
	if !a.On() {
		err := os.Remove(s.authFile())
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(a, "", "  ")
	tmp := s.authFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.authFile())
}

// Client is a client for the server that signs in with its saved account.
func (s Server) Client() *Client {
	c := New(s.Addr())
	c.Auth = s.Auth()
	return c
}

// session is what signing in gave: SIMPLE_LOGIN's cookie (in the jar) or
// UI_LOGIN's token.
type session struct {
	mu     sync.Mutex
	token  string
	jar    http.CookieJar
	signed bool
}

// authorize adds what the server's mode wants to a request.
func (c *Client) authorize(ctx context.Context, req *http.Request) error {
	switch c.Auth.Mode {
	case "", "NONE":
		return nil
	case "BASIC_AUTH":
		req.SetBasicAuth(c.Auth.Username, c.Auth.Password)
		return nil
	case "SIMPLE_LOGIN", "UI_LOGIN":
		c.sess.mu.Lock()
		defer c.sess.mu.Unlock()
		if !c.sess.signed {
			if err := c.signIn(ctx); err != nil {
				return err
			}
		}
		if c.Auth.Mode == "UI_LOGIN" {
			req.Header.Set("Authorization", "Bearer "+c.sess.token)
		}
		return nil
	}
	return fmt.Errorf("unknown Authentication mode %q", c.Auth.Mode)
}

// signIn opens a session: SIMPLE_LOGIN posts the login form (its cookie
// stays in the jar), UI_LOGIN asks the login mutation for a token.
func (c *Client) signIn(ctx context.Context) error {
	if c.sess.jar == nil {
		c.sess.jar, _ = cookiejar.New(nil)
	}
	h := *c.HTTP
	h.Jar = c.sess.jar
	switch c.Auth.Mode {
	case "SIMPLE_LOGIN":
		form := url.Values{"user": {c.Auth.Username}, "pass": {c.Auth.Password}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/login.html", strings.NewReader(form.Encode()))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		resp, err := h.Do(req)
		if err != nil {
			return fmt.Errorf("%w (%v)", ErrNotRunning, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusFound {
			return fmt.Errorf("%w: the username or password W5F has is not accepted", ErrUnauthorized)
		}
	case "UI_LOGIN":
		body, _ := json.Marshal(map[string]any{
			"query":     `mutation($u: String!, $p: String!) { login(input: {username: $u, password: $p}) { accessToken } }`,
			"variables": map[string]any{"u": c.Auth.Username, "p": c.Auth.Password},
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/api/graphql", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := h.Do(req)
		if err != nil {
			return fmt.Errorf("%w (%v)", ErrNotRunning, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		var r struct {
			Data struct {
				Login struct {
					AccessToken string `json:"accessToken"`
				} `json:"login"`
			} `json:"data"`
		}
		if json.Unmarshal(b, &r) != nil || r.Data.Login.AccessToken == "" {
			return fmt.Errorf("%w: the username or password W5F has is not accepted", ErrUnauthorized)
		}
		c.sess.token = r.Data.Login.AccessToken
	}
	c.sess.signed = true
	return nil
}

// signOut forgets the session (it expired): the next request signs in again.
func (c *Client) signOut() {
	c.sess.mu.Lock()
	c.sess.signed, c.sess.token = false, ""
	c.sess.mu.Unlock()
}

// httpClient is the client with the session's cookies.
func (c *Client) httpClient() *http.Client {
	if c.sess.jar == nil {
		return c.HTTP
	}
	h := *c.HTTP
	h.Jar = c.sess.jar
	return &h
}

// unauthorized reports an answer that means "sign in": HTTP 401, or a
// GraphQL error saying so.
func unauthorized(status int, msgs []string) bool {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return true
	}
	for _, m := range msgs {
		l := strings.ToLower(m)
		if strings.Contains(l, "unauthorized") || strings.Contains(l, "forbidden") {
			return true
		}
	}
	return false
}
