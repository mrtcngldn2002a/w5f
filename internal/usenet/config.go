package usenet

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is usenet.toml in the data folder: the server, the groups you
// read and the kill file. W5F writes it when you subscribe or kill; you may
// edit it too.
type Config struct {
	Server string   `toml:"server"`
	Groups []string `toml:"groups"`
	Kill   Kill     `toml:"kill"`
}

// DefaultServer lets everyone read (chosen with the owner, 2026-09-30).
const DefaultServer = "freenews.netfront.net:119"

// defaultGroups are the owner's starting sets: books and SF, esoterica and
// the paranormal, role-playing games and the old internet.
var defaultGroups = []string{
	"rec.arts.sf.written", "rec.arts.sf.fandom", "rec.arts.books", "rec.arts.books.tolkien",
	"alt.magick", "alt.pagan", "alt.religion.gnostic", "alt.mythology", "alt.paranormal",
	"rec.games.frp.dnd", "rec.arts.int-fiction", "alt.folklore.computers", "rec.arts.poems",
}

const configHeader = `# W5F Usenet: the news server, the groups you read and your kill file.
# Subscribe and unsubscribe from the Usenet page; "kill" hides posts whose
# subject or poster contains one of these words (case does not matter).
`

// LoadConfig reads the file, creating it with the defaults on first use.
func LoadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		c := Config{Server: DefaultServer, Groups: append([]string{}, defaultGroups...), Kill: Kill{Subjects: []string{"[spam]"}, From: []string{}}}
		return c, c.Save(path)
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := toml.Unmarshal(b, &c); err != nil {
		return Config{}, errors.New(filepath.Base(path) + ": " + err.Error())
	}
	if c.Server == "" {
		c.Server = DefaultServer
	}
	if !strings.Contains(c.Server, ":") {
		c.Server += ":119"
	}
	var groups []string
	for _, g := range c.Groups {
		if g = strings.ToLower(strings.TrimSpace(g)); ValidGroup(g) {
			groups = append(groups, g)
		}
	}
	c.Groups = groups
	return c, nil
}

// Save writes the file.
func (c Config) Save(path string) error {
	var b bytes.Buffer
	b.WriteString(configHeader)
	if err := toml.NewEncoder(&b).Encode(c); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func (c Config) subscribed(g string) bool {
	for _, x := range c.Groups {
		if x == g {
			return true
		}
	}
	return false
}
