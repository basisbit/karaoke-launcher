package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	kfeed "github.com/basisbit/karaoke-feeds/tool"
	"github.com/basisbit/karaoke-launcher/internal/folders"
)

const platform = "test-amd64"

// site is a community's update site: app.json, its signature and the archives.
type site struct {
	t   *testing.T
	dir string
	srv *httptest.Server
	key string // private key file
	pub string
}

func newSite(t *testing.T) *site {
	s := &site{t: t, dir: t.TempDir()}
	s.key, s.pub = newKey(t)
	s.srv = httptest.NewServer(http.FileServer(http.Dir(s.dir)))
	t.Cleanup(s.srv.Close)
	return s
}

func newKey(t *testing.T) (file, pub string) {
	t.Helper()
	file = filepath.Join(t.TempDir(), "app.key")
	if kfeed.Run([]string{"kfeed", "keygen", file}, "") != 0 {
		t.Fatal("keygen failed")
	}
	_, pub, err := kfeed.Sign(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	return file, pub
}

// publish writes app.json (any JSON value) and signs it with key ("" = the site's key).
func (s *site) publish(m any, key string) {
	s.t.Helper()
	data, _ := json.Marshal(m)
	os.WriteFile(filepath.Join(s.dir, "app.json"), data, 0o644)
	if key == "" {
		key = s.key
	}
	sig, _, err := kfeed.Sign(key, data)
	if err != nil {
		s.t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	os.MkdirAll(filepath.Join(s.dir, "sig"), 0o755)
	os.WriteFile(filepath.Join(s.dir, "sig", hex.EncodeToString(sum[:])+".sig"), []byte(sig), 0o644)
}

// archive stores a zip under files/ and returns its entry for app.json (relative address).
func (s *site) archive(name string, ms ...member) File {
	s.t.Helper()
	p := zipFile(s.t, s.t.TempDir(), ms...)
	data, _ := os.ReadFile(p)
	os.MkdirAll(filepath.Join(s.dir, "files"), 0o755)
	os.WriteFile(filepath.Join(s.dir, "files", name), data, 0o644)
	sum := sha256.Sum256(data)
	return File{URL: "files/" + name, SHA256: "sha256:" + hex.EncodeToString(sum[:]), Size: int64(len(data))}
}

func (s *site) updater(targets ...Target) *Updater {
	return &Updater{URL: s.srv.URL + "/app.json", Key: s.pub, ID: "example", UserData: s.t.TempDir(), Targets: targets, Platform: platform}
}

func manifest(seq int64, comps map[string]Component) Manifest {
	return Manifest{App: 1, ID: "example", Sequence: seq, Components: comps}
}

func comp(version string, f File) Component {
	return Component{Version: version, Files: map[string]File{platform: f}}
}

func ids(r *Result) string {
	var l []string
	for _, u := range r.Updates {
		l = append(l, u.ID+"@"+u.Version)
	}
	return strings.Join(l, ",")
}

func TestCheckStageApply(t *testing.T) {
	s := newSite(t)
	base := t.TempDir()
	app, game, other := filepath.Join(base, "app"), filepath.Join(base, "app", "game"), filepath.Join(base, "other")
	os.MkdirAll(app, 0o755)
	WriteState(app, State{Component: "launcher", Version: "1.0", Files: []string{"launcher"}})
	os.MkdirAll(other, 0o755)
	os.WriteFile(filepath.Join(other, "foreign.exe"), []byte("not ours"), 0o644)
	gameZip := s.archive("game-2.0.zip", member{name: "game-2.0/game", body: "game 2.0", mode: 0o755}, member{name: "game-2.0/config.ini", body: "default"})
	gameZip.Strip = 1
	s.publish(manifest(100, map[string]Component{
		"launcher": comp("2.0", s.archive("launcher-2.0.zip", member{name: "launcher", body: "launcher 2.0", mode: 0o755})),
		"game":     comp("2.0", gameZip),
		"other":    comp("2.0", s.archive("other.zip", member{name: "x", body: "x"})),
		"nofile":   {Version: "1.0", Files: map[string]File{"other-arch": gameZip}},
	}), "")
	u := s.updater(Target{ID: "launcher", Dir: app}, Target{ID: "game", Dir: game, Keep: []string{"config.ini"}},
		Target{ID: "other", Dir: other}, Target{ID: "nofile", Dir: filepath.Join(base, "nofile")}, Target{ID: "absent", Dir: base})
	ctx := context.Background()
	res, err := u.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(res); got != "launcher@2.0,game@2.0" || res.Notice {
		t.Fatalf("updates %q, notice %v (want launcher and game; a folder with foreign files is not touched)", got, res.Notice)
	}
	if res.Updates[1].File.URL != s.srv.URL+"/files/game-2.0.zip" {
		t.Fatalf("address not resolved: %s", res.Updates[1].File.URL)
	}
	for _, up := range res.Updates {
		calls := 0
		if err := u.Stage(ctx, up, func(done, total int64) { calls++ }); err != nil {
			t.Fatal(err)
		}
		if !Pending(up.Dir, up.ID) || calls == 0 {
			t.Fatalf("%s: not staged, %d progress calls", up.ID, calls)
		}
		if err := u.Stage(ctx, up, nil); err != nil {
			t.Fatal("second Stage:", err)
		}
		apply(t, up.Dir, up.ID, up.Keep)
	}
	if got := tree(t, game); got["game"] != "game 2.0" || got["config.ini"] != "default" {
		t.Fatalf("game folder: %v", got)
	}
	if st := state(t, app, "launcher"); st.Version != "2.0" {
		t.Fatalf("launcher state: %+v", st)
	}
	if got := tree(t, other); len(got) != 1 {
		t.Fatalf("foreign folder changed: %v", got)
	}
	if entries, _ := os.ReadDir(filepath.Join(u.UserData, "updates")); len(entries) != 0 {
		t.Fatalf("downloads left: %v", entries)
	}
	res, err = u.Check(ctx)
	if err != nil || len(res.Updates) != 0 {
		t.Fatalf("second check: %q %v", ids(res), err)
	}
}

func TestCheckRefused(t *testing.T) {
	ctx := context.Background()
	newGame := func(s *site) (*Updater, Manifest) {
		m := manifest(100, map[string]Component{"game": comp("2.0", s.archive("g.zip", member{name: "g", body: "g"}))})
		return s.updater(Target{ID: "game", Dir: filepath.Join(t.TempDir(), "game")}), m
	}
	other, _ := newKey(t)
	cases := []struct {
		name string
		prep func(s *site, m Manifest)
		want string
	}{
		{"missing signature", func(s *site, m Manifest) { s.publish(m, ""); os.RemoveAll(filepath.Join(s.dir, "sig")) }, "signature"},
		{"wrong key", func(s *site, m Manifest) { s.publish(m, other) }, "not signed"},
		{"wrong id", func(s *site, m Manifest) { m.ID = "someone-else"; s.publish(m, "") }, "is for"},
		{"app 2", func(s *site, m Manifest) { m.App = 2; s.publish(m, "") }, `"app" is 2`},
		{"2 MiB", func(s *site, m Manifest) {
			s.publish(map[string]any{"app": 1, "id": "example", "sequence": 100, "pad": strings.Repeat("x", 2<<20)}, "")
		}, "larger than"},
		{"changed after signing", func(s *site, m Manifest) {
			s.publish(m, "")
			data, _ := os.ReadFile(filepath.Join(s.dir, "app.json"))
			sum := sha256.Sum256(data)
			sig, _ := os.ReadFile(filepath.Join(s.dir, "sig", hex.EncodeToString(sum[:])+".sig"))
			data = []byte(strings.Replace(string(data), `"sequence":100`, `"sequence":101`, 1))
			sum = sha256.Sum256(data)
			os.WriteFile(filepath.Join(s.dir, "app.json"), data, 0o644)
			os.WriteFile(filepath.Join(s.dir, "sig", hex.EncodeToString(sum[:])+".sig"), sig, 0o644)
		}, "not signed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newSite(t)
			u, m := newGame(s)
			tc.prep(s, m)
			if res, err := u.Check(ctx); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v %v, want an error with %q", res, err, tc.want)
			}
			if _, err := os.Stat(filepath.Join(u.UserData, "update.json")); err == nil {
				t.Fatal("sequence stored for a refused app.json")
			}
		})
	}
	t.Run("lower sequence", func(t *testing.T) {
		s := newSite(t)
		u, m := newGame(s)
		s.publish(m, "")
		if _, err := u.Check(ctx); err != nil {
			t.Fatal(err)
		}
		m.Sequence = 50
		s.publish(m, "")
		if _, err := u.Check(ctx); err == nil || !strings.Contains(err.Error(), "older") {
			t.Fatalf("older app.json: %v", err)
		}
		data, _ := os.ReadFile(filepath.Join(u.UserData, "update.json"))
		if !strings.Contains(string(data), "100") {
			t.Fatalf("stored sequence changed: %s", data)
		}
	})
	t.Run("wrong archive hash", func(t *testing.T) {
		s := newSite(t)
		u, m := newGame(s)
		f := m.Components["game"].Files[platform]
		f.SHA256 = "sha256:" + strings.Repeat("0", 64)
		m.Components["game"] = comp("2.0", f)
		s.publish(m, "")
		res, err := u.Check(ctx)
		if err != nil || len(res.Updates) != 1 {
			t.Fatalf("check: %v %v", res, err)
		}
		if err := u.Stage(ctx, res.Updates[0], nil); err == nil || !strings.Contains(err.Error(), "hash") {
			t.Fatalf("Stage with a wrong hash: %v", err)
		}
		if Pending(res.Updates[0].Dir, "game") {
			t.Fatal("staged despite the wrong hash")
		}
	})
	t.Run("bad file entries", func(t *testing.T) {
		s := newSite(t)
		u, m := newGame(s)
		good := m.Components["game"].Files[platform]
		for _, f := range []File{
			{URL: "files/g.exe", SHA256: good.SHA256, Size: good.Size},
			{URL: "http://example.org/g.zip", SHA256: good.SHA256, Size: good.Size},
			{URL: "file:///etc/g.zip", SHA256: good.SHA256, Size: good.Size},
			{URL: good.URL, SHA256: "abc", Size: good.Size},
			{URL: good.URL, SHA256: good.SHA256, Size: 0},
			{URL: good.URL, SHA256: good.SHA256, Size: good.Size, Strip: -1},
		} {
			m.Components["game"] = comp("2.0", f)
			m.Sequence++
			s.publish(m, "")
			if res, err := u.Check(ctx); err != nil || len(res.Updates) != 0 {
				t.Errorf("%+v: %q %v, want no update", f, ids(res), err)
			}
		}
	})
}

func TestCheckLauncher(t *testing.T) {
	ctx := context.Background()
	s := newSite(t)
	app := t.TempDir()
	u := s.updater(Target{ID: "launcher", Dir: app})
	l := s.archive("l.zip", member{name: "launcher", body: "2.0"})
	s.publish(manifest(1, map[string]Component{"launcher": comp("2.0", l)}), "")
	if res, err := u.Check(ctx); err != nil || len(res.Updates) != 0 || res.Notice {
		t.Fatalf("launcher without state (developer run): %q %v %v", ids(res), res.Notice, err)
	}
	WriteState(app, State{Component: "launcher", Version: "1.0"})
	s.publish(manifest(2, map[string]Component{"launcher": {Version: "2.0", Files: map[string]File{"other-arch": l}}}), "")
	if res, err := u.Check(ctx); err != nil || len(res.Updates) != 0 || !res.Notice {
		t.Fatalf("no file for this system: %q notice %v %v, want a notice", ids(res), res.Notice, err)
	}
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Log("read-only folder not tested on Windows or as root")
		return
	}
	s.publish(manifest(3, map[string]Component{"launcher": comp("2.0", l)}), "")
	os.Chmod(app, 0o555)
	defer os.Chmod(app, 0o755)
	if res, err := u.Check(ctx); err != nil || len(res.Updates) != 0 || !res.Notice {
		t.Fatalf("read-only folder: %q notice %v %v, want a notice", ids(res), res.Notice, err)
	}
}

func TestCheckAddressBOMAndFirstInstall(t *testing.T) {
	s := newSite(t)
	game := filepath.Join(t.TempDir(), "game")
	// launcher-made content inside the game's folder and an interrupted first installation don't make it foreign
	os.MkdirAll(filepath.Join(game, "data", "themes", "Blue"), 0o755)
	if err := folders.Mark(filepath.Join(game, "data", "themes"), folders.Content("theme"), "# Themes"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(game, "data", "themes", "Blue", "Blue.ini"), []byte("theme"), 0o644)
	os.MkdirAll(filepath.Join(stagingDir(game, "game"), "files"), 0o755)
	os.WriteFile(filepath.Join(stagingDir(game, "game"), "files", "half"), []byte("x"), 0o644)
	// signed as published: with a byte order mark
	data, _ := json.Marshal(manifest(1, map[string]Component{"game": comp("1.0", s.archive("g.zip", member{name: "game", body: "g"}))}))
	data = append([]byte("\xef\xbb\xbf"), data...)
	os.WriteFile(filepath.Join(s.dir, "app.json"), data, 0o644)
	sig, _, err := kfeed.Sign(s.key, data)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	os.MkdirAll(filepath.Join(s.dir, "sig"), 0o755)
	os.WriteFile(filepath.Join(s.dir, "sig", hex.EncodeToString(sum[:])+".sig"), []byte(sig), 0o644)

	u := s.updater(Target{ID: "game", Dir: game})
	u.URL += "?token=a/b" // a "/" in the query is not a folder
	res, err := u.Check(context.Background())
	if err != nil || ids(res) != "game@1.0" {
		t.Fatalf("check: %v %v", res, err)
	}
	if err := u.Stage(context.Background(), res.Updates[0], nil); err != nil {
		t.Fatal(err)
	}
	apply(t, game, "game", nil)
	if got := tree(t, game); got["game"] != "g" || got["data/themes/Blue/Blue.ini"] != "theme" || got["half"] != "" {
		t.Fatalf("game folder: %v", got)
	}

	os.RemoveAll(game)
	os.MkdirAll(filepath.Join(game, "data"), 0o755)
	os.WriteFile(filepath.Join(game, "data", "own.txt"), []byte("x"), 0o644)
	if res, err := u.Check(context.Background()); err != nil || len(res.Updates) != 0 {
		t.Fatalf("folder with a foreign file: %v %v", ids(res), err)
	}
}
