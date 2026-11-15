package bundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// releaseBase is where launcher releases are downloaded from (a test replaces it).
var releaseBase = "https://github.com"

const maxBinary = 512 << 20

// BinaryName is the file name of the launcher for a system ("windows-amd64"), as release.yml builds it.
func BinaryName(platform string) string {
	if strings.HasPrefix(platform, "windows-") {
		return "karaoke-launcher-" + platform + ".exe"
	}
	return "karaoke-launcher-" + platform
}

// LauncherBinary returns the launcher program for a system: from dir when it is given, otherwise downloaded from
// the release named in bundle.json into cache (once) and checked against the release's SHA256SUMS.
func (b *Bundle) LauncherBinary(ctx context.Context, platform, dir, cache string) (string, error) {
	name := BinaryName(platform)
	if dir != "" {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
			return "", fmt.Errorf("%s is not in %s", name, dir)
		}
		return p, nil
	}
	base := fmt.Sprintf("%s/%s/releases/download/%s/", releaseBase, b.Launcher.Repo, b.Launcher.Version)
	var sums strings.Builder
	if _, err := get(ctx, base+"SHA256SUMS", &sums, 1<<20); err != nil {
		return "", fmt.Errorf("SHA256SUMS of launcher %s: %v", b.Launcher.Version, err)
	}
	want := ""
	for _, line := range strings.Split(sums.String(), "\n") {
		if f := strings.Fields(line); len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			want = "sha256:" + strings.ToLower(f[0])
		}
	}
	if !hashRe.MatchString(want) {
		return "", fmt.Errorf("SHA256SUMS of launcher %s does not list %s", b.Launcher.Version, name)
	}
	dest := filepath.Join(cache, "launcher-"+b.Launcher.Version, name)
	if err := fetchChecked(ctx, base+name, want, dest, maxBinary, 0o755); err != nil {
		return "", fmt.Errorf("%s of launcher %s: %v", name, b.Launcher.Version, err)
	}
	return dest, nil
}

// fetchChecked downloads addr to dest unless dest already has the hash want ("sha256:<hex>"). The file is written
// only when its hash is right.
func fetchChecked(ctx context.Context, addr, want, dest string, max int64, perm os.FileMode) error {
	if have, _, err := hashFile(dest); err == nil && have == want {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".*.part")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	h := sha256.New()
	_, err = get(ctx, addr, io.MultiWriter(tmp, h), max)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if have := "sha256:" + hex.EncodeToString(h.Sum(nil)); have != want {
		return fmt.Errorf("the download has the hash %s, but %s was expected", have, want)
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

var client = &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 || (via[0].URL.Scheme == "https" && req.URL.Scheme != "https") {
		return errors.New("too many redirects, or redirect from https to http")
	}
	return nil
}}

// get downloads addr into w; more than max bytes is an error.
func get(ctx context.Context, addr string, w io.Writer, max int64) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%s: HTTP %d", req.URL.Redacted(), resp.StatusCode)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, max+1))
	if err == nil && n > max {
		err = fmt.Errorf("%s is larger than %d bytes", req.URL.Redacted(), max)
	}
	return n, err
}
