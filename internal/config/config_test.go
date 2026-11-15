package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// write stores a config built from base plus changes (nil value: delete the key) and returns its path.
func write(t *testing.T, change map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "assets", "logo.png"), []byte("png"), 0o644)
	c := map[string]any{
		"config": 1,
		"id":     "example-karaoke",
		"name":   "Example Karaoke",
		"look": map[string]any{
			"logo": "assets/logo.png", "dark": true,
			"colors": map[string]any{"primary": "#3b82f6", "background": "#111827", "text": "#F9FAFB"},
		},
		"links": []any{map[string]any{"label": map[string]any{"en": "Website"}, "url": "https://example.org/"}},
		"news":  map[string]any{"url": "https://example.org/news.txt", "more": "https://example.org/news"},
		"feeds": []any{map[string]any{"id": "main", "address": "https://example.org/feed/", "key": "ed25519:abc",
			"starter": []any{"starter-pack"}}},
		"allow_other_feeds": true,
		"start": map[string]any{
			"windows": []any{"{appdir}/game.exe", "-songpath", "{library}/songs"},
			"linux":   []any{"game", "-songpath", "{library}/songs"},
			"macos":   []any{"open", "-a", "Game", "--args", "-songpath", "{library}/songs"},
		},
	}
	for k, v := range change {
		if v == nil {
			delete(c, k)
		} else {
			c[k] = v
		}
	}
	data, _ := json.Marshal(c)
	path := filepath.Join(dir, "launcher.json")
	os.WriteFile(path, data, 0o644)
	return path
}

func look(field string, value any) map[string]any {
	l := map[string]any{"logo": "assets/logo.png"}
	l[field] = value
	return map[string]any{"look": l}
}

func feeds(f ...map[string]any) map[string]any {
	list := []any{}
	for _, x := range f {
		list = append(list, x)
	}
	return map[string]any{"feeds": list}
}

func feed(id, address string) map[string]any { return map[string]any{"id": id, "address": address} }

func TestValid(t *testing.T) {
	c, err := Load(write(t, map[string]any{"future_field": map[string]any{"x": 1}}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Name.Get("de") != "Example Karaoke" || c.Feeds[0].Starter[0] != "starter-pack" || !c.Look.Dark || c.Library.Default != "{userdata}/songs" {
		t.Fatalf("loaded: %+v", c)
	}
	if got := c.Asset(c.Look.Logo); got != filepath.Join(c.Dir, "assets", "logo.png") {
		t.Fatalf("Asset: %s", got)
	}
	// minimal: no look, links, news, start; only other feeds allowed
	if _, err := Load(write(t, map[string]any{"look": nil, "links": nil, "news": nil, "start": nil, "feeds": nil})); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(write(t, feeds(feed("dev", "http://127.0.0.1:8080/"), feed("dev2", "http://localhost/feed/")))); err != nil {
		t.Fatal(err)
	}
}

func TestInvalid(t *testing.T) {
	big := strings.Repeat(" ", 10<<20)
	cases := []struct {
		name   string
		change map[string]any
		want   string
	}{
		{"config 2", map[string]any{"config": 2}, `"config" must be 1`},
		{"config missing", map[string]any{"config": nil}, `"config" must be 1`},
		{"id upper case", map[string]any{"id": "Example"}, `"id"`},
		{"id with slash", map[string]any{"id": "../x"}, `"id"`},
		{"id too long", map[string]any{"id": strings.Repeat("a", 33)}, `"id"`},
		{"id empty", map[string]any{"id": ""}, `"id"`},
		{"name missing", map[string]any{"name": nil}, `"name" is missing`},
		{"name empty", map[string]any{"name": " "}, `"name" for "" is empty`},
		{"name empty language", map[string]any{"name": map[string]any{"en": "Karaoke", "de": ""}}, `"name" for "de" is empty`},
		{"name number", map[string]any{"name": 5}, "not valid JSON"},
		{"logo ..", look("logo", "../logo.png"), "relative path inside"},
		{"logo .. inside", look("logo", "assets/../../logo.png"), "relative path inside"},
		{"logo absolute", look("logo", "/etc/passwd"), "relative path inside"},
		{"logo windows absolute", look("logo", `C:\logo.png`), "relative path inside"},
		{"logo backslash", look("logo", `assets\..\..\x`), "relative path inside"},
		{"font missing file", look("font", "assets/font.ttf"), "look.font"},
		{"background drive", look("background", "C:/bg.png"), "relative path inside"},
		{"colour name", look("colors", map[string]any{"primary": "blue"}), "look.colors.primary"},
		{"colour short", look("colors", map[string]any{"text": "#fff"}), "look.colors.text"},
		{"link javascript", map[string]any{"links": []any{map[string]any{"label": map[string]any{"en": "x"}, "url": "javascript:alert(1)"}}}, "links[0].url"},
		{"link http", map[string]any{"links": []any{map[string]any{"label": map[string]any{"en": "x"}, "url": "http://example.org/"}}}, "must start with https://"},
		{"link file", map[string]any{"links": []any{map[string]any{"label": map[string]any{"en": "x"}, "url": "file:///etc/passwd"}}}, "links[0].url"},
		{"link no label", map[string]any{"links": []any{map[string]any{"url": "https://example.org/"}}}, "no label"},
		{"news ftp", map[string]any{"news": map[string]any{"url": "ftp://example.org/news.txt"}}, "news.url"},
		{"news more data", map[string]any{"news": map[string]any{"url": "https://example.org/n.txt", "more": "data:text/html,x"}}, "news.more"},
		{"feed user info", feeds(feed("main", "https://user:pw@example.org/")), "feeds[0].address"},
		{"feed http remote", feeds(feed("main", "http://192.168.1.2/")), "feeds[0].address"},
		{"feed http lookalike", feeds(feed("main", "http://localhost.example.org/")), "feeds[0].address"},
		{"feed id twice", feeds(feed("main", "https://a.example/"), feed("main", "https://b.example/")), "used twice"},
		{"feed id bad", feeds(feed("Main Feed", "https://a.example/")), "feeds[0].id"},
		{"feed key", feeds(map[string]any{"id": "main", "address": "https://a.example/", "key": "rsa:x"}), "ed25519:"},
		{"starter bad", feeds(map[string]any{"id": "main", "address": "https://a.example/", "starter": []any{"../x"}}), "starter"},
		{"nothing to download", map[string]any{"feeds": nil, "allow_other_feeds": false}, "nothing to download"},
		{"start unknown placeholder", map[string]any{"start": map[string]any{"linux": []any{"game", "{home}/songs"}}}, "unknown placeholder {home}"},
		{"start empty", map[string]any{"start": map[string]any{"linux": []any{}}}, "first argument"},
		{"start shell string empty program", map[string]any{"start": map[string]any{"windows": []any{""}}}, "first argument"},
		{"library uses {library}", map[string]any{"library": map[string]any{"default": "{library}/x"}}, "library.default"},
		{"10 MB file", map[string]any{"padding": big}, "larger than"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(write(t, tc.change))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error with %q", err, tc.want)
			}
		})
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "launcher.json")
	os.WriteFile(bad, []byte(`{"config": 1,`), 0o644)
	if _, err := Load(bad); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("broken JSON: %v", err)
	}
	if _, err := Load(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestStartArgs(t *testing.T) {
	c, err := Load(write(t, map[string]any{"start": map[string]any{
		"linux":   []any{"game", "--title", "a; rm -rf ~ && $(echo x) | y", "{library}/songs"},
		"windows": []any{"{appdir}/game.exe"},
		"amiga":   []any{"{nonsense}"}, // unknown systems are ignored
	}}))
	if err != nil {
		t.Fatal(err)
	}
	v := Vars{Library: "/lib", AppDir: "/app", UserData: "/u"}
	args, ok, err := c.StartArgs("linux", v)
	want := []string{"game", "--title", "a; rm -rf ~ && $(echo x) | y", "/lib/songs"}
	if !ok || err != nil || !reflect.DeepEqual(args, want) {
		t.Fatalf("linux: %q %v %v", args, ok, err) // shell characters stay one literal argument
	}
	if args, ok, _ := c.StartArgs("windows", v); !ok || args[0] != "/app/game.exe" {
		t.Fatalf("windows: %q", args)
	}
	if _, ok, _ := c.StartArgs("darwin", v); ok {
		t.Fatal("macos has no entry, but ok")
	}
	if _, ok, _ := c.StartArgs("plan9", v); ok {
		t.Fatal("unknown system, but ok")
	}
	lib, err := c.LibraryDefault(v)
	if err != nil || lib != filepath.Clean(filepath.FromSlash("/u/songs")) {
		t.Fatalf("LibraryDefault: %s %v", lib, err)
	}
}

func TestExpand(t *testing.T) {
	v := Vars{Library: "L", AppDir: "A", UserData: "U"}
	if s, err := Expand("{library}-{appdir}-{userdata}-{", v); s != "L-A-U-{" || err != nil {
		t.Fatalf("%q %v", s, err)
	}
	if _, err := Expand("{LIBRARY}", v); err == nil {
		t.Fatal("placeholders are case-sensitive")
	}
	if _, err := Expand("{library}", v, "appdir"); err == nil {
		t.Fatal("not allowed placeholder accepted")
	}
}

func TestNameTranslations(t *testing.T) {
	c, err := Load(write(t, map[string]any{"name": map[string]any{"en": "Karaoke", "ja": "カラオケ", "de": "Karaoke"}}))
	if err != nil {
		t.Fatal(err)
	}
	for code, want := range map[string]string{"ja": "カラオケ", "de": "Karaoke", "fr": "Karaoke", "en": "Karaoke"} {
		if got := c.Name.Get(code); got != want {
			t.Errorf("%s: %q, want %q", code, got, want)
		}
	}
	if (Text{"zz": "Z", "ab": "A"}).Get("fr") != "A" || (Text{}).Get("de") != "" {
		t.Error("fallback order")
	}
}
