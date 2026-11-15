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
		{"app_id two parts", map[string]any{"app_id": "example.Karaoke"}, `"app_id"`},
		{"app_id space", map[string]any{"app_id": "org.example.My Karaoke"}, `"app_id"`},
		{"app_id empty part", map[string]any{"app_id": "org..example.Karaoke"}, `"app_id"`},
		{"app_id slash", map[string]any{"app_id": "org.example/../x.y"}, `"app_id"`},
		{"app_id too long", map[string]any{"app_id": "org.example." + strings.Repeat("a", 244)}, `"app_id"`},
		{"updates http", updates("http://example.org/app.json", goodKey, ""), "updates.url"},
		{"updates url missing", updates("", goodKey, ""), "updates.url"},
		{"updates page file", updates("https://example.org/app.json", goodKey, "file:///etc"), "updates.page"},
		{"updates key short", updates("https://example.org/app.json", "ed25519:abc", ""), "updates.key"},
		{"updates key missing", updates("https://example.org/app.json", "", ""), "updates.key"},
		{"updates key rsa", updates("https://example.org/app.json", "rsa:"+goodKey[8:], ""), "updates.key"},
		{"component launcher", component("launcher", map[string]any{"windows": "{appdir}/x"}, nil), "is the launcher itself"},
		{"component id bad", component("Game", map[string]any{"windows": "{appdir}/x"}, nil), "components:"},
		{"component dir relative", component("game", map[string]any{"linux": "game"}, nil), "must start with"},
		{"component dir absolute", component("game", map[string]any{"linux": "/usr/lib/game"}, nil), "must start with"},
		{"component dir {library}", component("game", map[string]any{"linux": "{home}/{library}"}, nil), "unknown placeholder {library}"},
		{"component dir ..", component("game", map[string]any{"windows": `{appdir}\..\x`}, nil), `must not contain ".."`},
		{"keep ..", component("game", map[string]any{"windows": "{appdir}/game"}, []any{"../config.ini"}), "keep"},
		{"keep absolute", component("game", map[string]any{"windows": "{appdir}/game"}, []any{"/etc/x"}), "keep"},
		{"keep backslash", component("game", map[string]any{"windows": "{appdir}/game"}, []any{`data\x`}), "keep"},
		{"keep bad pattern", component("game", map[string]any{"windows": "{appdir}/game"}, []any{"[x"}), "keep"},
		{"keep empty", component("game", map[string]any{"windows": "{appdir}/game"}, []any{""}), "keep"},
		{"content songs", content("songs", themeLabel, map[string]any{"linux": "{home}/t"}, []any{".ini"}), `other than "songs"`},
		{"content kind upper", content("Theme", themeLabel, map[string]any{"linux": "{home}/t"}, []any{".ini"}), "content:"},
		{"content kind long", content(strings.Repeat("a", 33), themeLabel, map[string]any{"linux": "{home}/t"}, []any{".ini"}), "content:"},
		{"content no label", content("theme", nil, map[string]any{"linux": "{home}/t"}, []any{".ini"}), "label is missing"},
		{"content empty label", content("theme", map[string]any{"de": " "}, map[string]any{"linux": "{home}/t"}, []any{".ini"}), `label for "de" is empty`},
		{"content dir relative", content("theme", themeLabel, map[string]any{"linux": "themes"}, []any{".ini"}), "must start with"},
		{"content dir absolute", content("theme", themeLabel, map[string]any{"windows": "C:/themes"}, []any{".ini"}), "must start with"},
		{"content dir ..", content("theme", themeLabel, map[string]any{"linux": "{home}/../etc"}, []any{".ini"}), `must not contain ".."`},
		{"content dir unknown placeholder", content("theme", themeLabel, map[string]any{"linux": "{home}/{x}"}, []any{".ini"}), "unknown placeholder {x}"},
		{"content no extensions", content("theme", themeLabel, map[string]any{"linux": "{home}/t"}, []any{}), "extensions is empty"},
		{"content dir library", content("theme", themeLabel, map[string]any{"linux": "{library}/"}, []any{".ini"}), "must not be the song folder itself"},
		{"content dir added feed", content("theme", themeLabel, map[string]any{"windows": `{library}\added-0a1b2c3d\t`}, []any{".ini"}), "folder of a feed"},
		{"content two kinds one folder", map[string]any{"content": map[string]any{
			"theme": map[string]any{"label": themeLabel, "dir": map[string]any{"linux": "{home}/x"}, "extensions": []any{".ini"}},
			"skin":  map[string]any{"label": themeLabel, "dir": map[string]any{"linux": "{home}/x/"}, "extensions": []any{".ini"}},
		}}, "every kind needs its own folder"},
		{"content extension without dot", content("theme", themeLabel, map[string]any{"linux": "{home}/t"}, []any{"ini"}), "extensions"},
		{"content extension upper", content("theme", themeLabel, map[string]any{"linux": "{home}/t"}, []any{".PNG"}), "extensions"},
		{"content extension long", content("theme", themeLabel, map[string]any{"linux": "{home}/t"}, []any{".abcdefghi"}), "extensions"},
		{"content extension exe", content("theme", themeLabel, map[string]any{"linux": "{home}/t"}, []any{".ini", ".exe"}), "program or script"},
		{"content extension sh", content("theme", themeLabel, map[string]any{"linux": "{home}/t"}, []any{".sh"}), "program or script"},
		{"content extension desktop", content("theme", themeLabel, map[string]any{"linux": "{home}/t"}, []any{".desktop"}), "program or script"},
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

const goodKey = "ed25519:ZFpXtOewICQ0aV3Z5HkkPT0PuA1yNQhIHMIDjAOvxc8"

func updates(url, key, page string) map[string]any {
	return map[string]any{"updates": map[string]any{"url": url, "key": key, "page": page}}
}

func component(id string, dir map[string]any, keep []any) map[string]any {
	return map[string]any{"components": map[string]any{id: map[string]any{"dir": dir, "keep": keep}}}
}

var themeLabel = map[string]any{"en": "Themes", "de": "Designs"}

func content(kind string, label map[string]any, dir map[string]any, exts []any) map[string]any {
	c := map[string]any{"dir": dir, "extensions": exts}
	if label != nil {
		c["label"] = label
	}
	return map[string]any{"content": map[string]any{kind: c}}
}

func TestContent(t *testing.T) {
	c, err := Load(write(t, content("theme", themeLabel, map[string]any{"windows": "{appdir}/game/themes", "linux": "{home}/.game/themes",
		"macos": "{library}/../x", "amiga": "anything"}, []any{".ini", ".png", ".mp4"})))
	if err == nil {
		t.Fatalf("macos dir with .. accepted: %+v", c.Content)
	}
	c, err = Load(write(t, content("theme", themeLabel, map[string]any{"windows": "{appdir}/game/themes", "linux": "{home}/.game/themes",
		"macos": "{library}/themes", "amiga": "anything"}, []any{".ini", ".png", ".mp4"})))
	if err != nil {
		t.Fatal(err)
	}
	if c.Content["theme"].Label.Get("de") != "Designs" || len(c.Content["theme"].Extensions) != 3 {
		t.Fatalf("loaded: %+v", c.Content)
	}
	v := Vars{Library: "/lib", AppDir: "/app", UserData: "/u", Home: "/home/x"}
	for _, tc := range []struct {
		kind, goos, want string
		ok               bool
	}{
		{"theme", "windows", "/app/game/themes", true},
		{"theme", "linux", "/home/x/.game/themes", true},
		{"theme", "darwin", "/lib/themes", true},
		{"theme", "plan9", "", false},
		{"other", "linux", "", false},
	} {
		dir, ok, err := c.ContentDir(tc.kind, tc.goos, v)
		if want := filepath.FromSlash(tc.want); dir != want || ok != tc.ok || err != nil {
			t.Errorf("ContentDir(%s, %s) = %q %v %v, want %q %v", tc.kind, tc.goos, dir, ok, err, want, tc.ok)
		}
	}
	if tg := c.Targets("linux", v); len(tg) != 1 || tg["theme"].Dir != filepath.FromSlash("/home/x/.game/themes") || len(tg["theme"].Extensions) != 3 {
		t.Errorf("Targets: %+v", tg)
	}
	if tg := c.Targets("plan9", v); len(tg) != 0 {
		t.Errorf("Targets for a system without a folder: %+v", tg)
	}
}

func TestComponents(t *testing.T) {
	change := updates("https://example.org/karaoke/app.json", goodKey, "https://example.org/karaoke/")
	change["app_id"] = "org.example.Karaoke_2-x"
	change["components"] = map[string]any{
		"game":  map[string]any{"dir": map[string]any{"windows": "{appdir}/game", "linux": "{home}/.game", "amiga": "x"}, "keep": []any{"config.ini", "*.db", "data/*.cfg"}},
		"tools": map[string]any{"dir": map[string]any{"macos": "{userdata}/tools"}},
	}
	c, err := Load(write(t, change))
	if err != nil {
		t.Fatal(err)
	}
	if c.AppID != "org.example.Karaoke_2-x" || c.Updates.Key != goodKey || c.Updates.Page == "" || len(c.Components["game"].Keep) != 3 {
		t.Fatalf("loaded: %+v %+v", c, c.Updates)
	}
	v := Vars{Library: "/lib", AppDir: "/app", UserData: "/u", Home: "/home/x"}
	for _, tc := range []struct {
		id, goos, want string
		ok             bool
	}{
		{"game", "windows", "/app/game", true},
		{"game", "linux", "/home/x/.game", true},
		{"game", "darwin", "", false},
		{"tools", "darwin", "/u/tools", true},
		{"launcher", "linux", "/app", true},
		{"unknown", "linux", "", false},
		{"game", "plan9", "", false},
	} {
		dir, ok, err := c.ComponentDir(tc.id, tc.goos, v)
		if want := filepath.FromSlash(tc.want); dir != want || ok != tc.ok || err != nil {
			t.Errorf("ComponentDir(%s, %s) = %q %v %v, want %q %v", tc.id, tc.goos, dir, ok, err, want, tc.ok)
		}
	}
	// {home} is only for component folders
	if _, err := Expand("{home}/x", v, startVars...); err == nil {
		t.Error("{home} accepted in a start command")
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

func TestAppDir(t *testing.T) {
	base := t.TempDir()
	contents := filepath.Join(base, "Karaoke.app", "Contents")
	exe := filepath.Join(contents, "MacOS", "karaoke-launcher")
	res := filepath.Join(contents, "Resources")
	if got := AppDir(exe, "darwin"); got != filepath.Dir(exe) { // no launcher.json in Resources yet
		t.Fatalf("without Resources/launcher.json: %s", got)
	}
	os.MkdirAll(res, 0o755)
	os.WriteFile(filepath.Join(res, "launcher.json"), []byte("{}"), 0o644)
	if got := AppDir(exe, "darwin"); got != res {
		t.Fatalf("macOS app: %s", got)
	}
	if got := AppDir(exe, "linux"); got != filepath.Dir(exe) {
		t.Fatalf("linux: %s", got)
	}
	plain := filepath.Join(base, "karaoke", "karaoke-launcher")
	if got := AppDir(plain, "darwin"); got != filepath.Dir(plain) {
		t.Fatalf("macOS without app: %s", got)
	}
}
