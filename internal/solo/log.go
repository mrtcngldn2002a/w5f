package solo

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Entry is one throw, answer or spark, as the log keeps it (JSON lines,
// one file a day in the W5F data folder): what was asked, what came out
// and where it came from, so a session can be followed back.
type Entry struct {
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"` // roll, ask, spark
	Input  string    `json:"input"`
	Result string    `json:"result"`
	Detail string    `json:"detail,omitempty"`
	Source string    `json:"source,omitempty"`
	Link   string    `json:"link,omitempty"`
	Manual bool      `json:"manual,omitempty"`
}

func (e Env) logFile(day time.Time) string {
	return filepath.Join(e.LogDir, day.Format("2006-01-02")+".jsonl")
}

func (e Env) record(en Entry) {
	if e.LogDir == "" {
		return
	}
	if en.Time.IsZero() {
		en.Time = time.Now()
	}
	if os.MkdirAll(e.LogDir, 0o755) != nil {
		return
	}
	f, err := os.OpenFile(e.logFile(en.Time), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(en)
	f.Write(append(b, '\n'))
}

// Recent is the log, newest first, from the latest days (at most n).
func (e Env) Recent(n int) []Entry {
	files, _ := filepath.Glob(filepath.Join(e.LogDir, "*.jsonl"))
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	var out []Entry
	for _, f := range files {
		var day []Entry
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var en Entry
			if json.Unmarshal(sc.Bytes(), &en) == nil {
				day = append(day, en)
			}
		}
		fh.Close()
		for i := len(day) - 1; i >= 0; i-- {
			out = append(out, day[i])
			if len(out) == n {
				return out
			}
		}
	}
	return out
}
