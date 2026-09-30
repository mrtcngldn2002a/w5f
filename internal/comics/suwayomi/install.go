package suwayomi

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ReleaseAPI is where the latest Suwayomi-Server release is looked up.
var ReleaseAPI = "https://api.github.com/repos/Suwayomi/Suwayomi-Server/releases/latest"

type release struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

func latestRelease(ctx context.Context) (release, error) {
	var rel release
	if err := getJSON(ctx, ReleaseAPI, &rel); err != nil {
		return rel, fmt.Errorf("finding the latest Suwayomi release: %w", err)
	}
	return rel, nil
}

// Install downloads the latest Suwayomi-Server jar from its official GitHub
// release into dir, checks it against the release's Checksums.sha256 and
// removes older jars. It returns the jar's name.
func Install(ctx context.Context, dir string, progress func(done, total int64)) (string, error) {
	rel, err := latestRelease(ctx)
	if err != nil {
		return "", err
	}
	var jarURL, sumURL, jarName string
	var size int64
	for _, a := range rel.Assets {
		switch {
		case strings.HasPrefix(a.Name, "Suwayomi-Server-") && strings.HasSuffix(a.Name, ".jar"):
			jarURL, jarName, size = a.URL, a.Name, a.Size
		case a.Name == "Checksums.sha256":
			sumURL = a.URL
		}
	}
	if jarURL == "" || sumURL == "" {
		return "", fmt.Errorf("release %s has no jar or no checksums", rel.Tag)
	}
	sums, err := get(ctx, sumURL, 1<<16)
	if err != nil {
		return "", err
	}
	want := ""
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == jarName {
			want = strings.ToLower(f[0])
		}
	}
	if want == "" {
		return "", errors.New("the release's checksums do not list the jar")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp := filepath.Join(dir, jarName+".part")
	if err := download(ctx, jarURL, tmp, size, want, progress); err != nil {
		os.Remove(tmp)
		return "", err
	}
	old, _ := filepath.Glob(filepath.Join(dir, "Suwayomi-Server-*.jar"))
	if err := os.Rename(tmp, filepath.Join(dir, jarName)); err != nil {
		return "", err
	}
	for _, o := range old {
		if filepath.Base(o) != jarName {
			os.Remove(o)
		}
	}
	return jarName, nil
}

func request(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "W5F (Suwayomi install)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	return resp, nil
}

func get(ctx context.Context, u string, limit int64) ([]byte, error) {
	resp, err := request(ctx, u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func getJSON(ctx context.Context, u string, out any) error {
	b, err := get(ctx, u, 1<<20)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func download(ctx context.Context, u, path string, size int64, sha string, progress func(done, total int64)) error {
	resp, err := request(ctx, u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	h := sha256.New()
	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				return err
			}
			h.Write(buf[:n])
			done += int64(n)
			if progress != nil {
				progress(done, size)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sha {
		return errors.New("the downloaded jar does not match the release checksum; it was not kept")
	}
	return nil
}
