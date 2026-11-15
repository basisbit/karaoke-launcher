# Bundles: the launcher with your game

This page is for communities that ship the launcher. A **bundle** is the launcher together with your
`launcher.json`, your logo and other files, and usually a karaoke game: your own, or one that `karaoke-bundle` knows
(see [Games](#games)). The tool `karaoke-bundle` turns it into what players download, one download per system with
the game inside:

- **Windows:** a setup program and a portable zip.
- **Linux:** a Flatpak (one `.flatpak` file, a `.flatpakref` file and a repository that Flatpak updates from).
- **macOS:** a zip with the app (Apple Silicon only); on a Mac also a `.dmg`.
- **The site:** a folder for your web server with all downloads and the signed `app.json` that keeps the launcher and
  your game up to date (see [updates.md](updates.md)).

Publishing the site is described in [publishing.md](publishing.md).

`karaoke-bundle` is a single program without dependencies. It is attached to every karaoke-launcher release as
`karaoke-bundle-<system>-<architecture>`. Check it against `SHA256SUMS` of the same release.

## Files

Put `bundle.json` next to your `launcher.json`. Everything `bundle.json` names is relative to its folder.

```
my-bundle/
  bundle.json
  launcher.json
  icon.png
  assets/logo.svg
  game/game-windows-amd64.zip
  flatpak/game.yml
```

The folder [examples/bundle](../examples/bundle) is a complete bundle with a dummy game.
[examples/usdx](../examples/usdx) is a bundle with UltraStar Deluxe: its files only hold what is the community's
own.

## `bundle.json`

```json
{
  "bundle": 1,
  "version": "1.0.0",
  "icon": "icon.png",
  "summary": "Sing along with friends",
  "description": "A longer text.\n\nA blank line starts a new paragraph.",
  "license": "MIT",
  "homepage": "https://example.org/",
  "developer": "Example Community",
  "site": "https://example.org/karaoke/",
  "launcher": { "version": "v0.2.0", "repo": "basisbit/karaoke-launcher" },
  "files": ["assets", "docs/readme.txt"],
  "components": {
    "game": {
      "version": "2.1.0",
      "windows-amd64": { "path": "game/game-windows-amd64.zip", "sha256": "sha256:…", "strip": 1 },
      "linux-amd64": { "url": "https://example.org/game/game-2.1.0-linux.tar.gz", "sha256": "sha256:…", "strip": 1 },
      "darwin-arm64": { "path": "game/game-macos.zip", "sha256": "sha256:…", "strip": 0 }
    }
  },
  "flatpak": {
    "runtime": "org.freedesktop.Platform",
    "runtime_version": "26.08",
    "sdk": "org.freedesktop.Sdk",
    "finish_args": ["--share=network", "--socket=wayland", "--socket=fallback-x11", "--share=ipc", "--device=dri"],
    "modules": ["flatpak/game.yml"]
  }
}
```

Unknown fields are an error, so a typo doesn't go unnoticed.

- `bundle`: always `1`.
- `game` (optional): a game that `karaoke-bundle` knows, for example `"ultrastar-deluxe"`, or your own definition as
  `{"path": "<folder with game.json>"}`. See [Games](#games).
- `version`: the version of the bundle, for example `1.2.0`: 1 to 64 letters, digits, `.`, `_`, `+` or `-`. It is
  the version of the launcher component in `app.json`, so raise it with every release. File names contain it.
  macOS only gets the leading numbers (`1.2.0` of `1.2.0-beta`).
- `icon`: a square PNG file. Use at least 512 × 512 pixels: the packages need sizes up to 512.
- `summary`: one line that says what the bundle is. It is the comment of the Linux menu entry and the summary in
  software centres.
- `description`: a few sentences for software centres (Linux). A blank line starts a new paragraph.
- `license`: the license of the bundle as an [SPDX](https://spdx.org/licenses/) expression, for example `MIT` or
  `GPL-2.0-or-later`.
- `homepage`: your website (`https://`).
- `developer`: who makes the bundle, for example the name of your community.
- `site`: the public address where you put the site folder; it ends with `/`. `updates.url` in `launcher.json`
  must be this address plus `app.json`. `https://` only (`http://` only for `127.0.0.1` or `localhost`, for testing).
- `launcher`: the launcher release you ship. `version` is its tag, `repo` the GitHub repository. The tool downloads
  the launcher programs from there and checks them against the release's `SHA256SUMS`.
- `files`: files and folders that go into every package next to the launcher, for example a readme. Links are not
  allowed. `launcher.json` and the files its `look` names (logo, background, font) are always included; you don't
  need to list them. Names starting with `karaoke-launcher`, `.component-` or `.update-` are reserved for the
  launcher.
- `components`: the programs the launcher installs besides itself, usually your game. Each name must also be in
  `components` of `launcher.json` (see [updates.md](updates.md)).
  - `version`: the version of the program.
  - one entry per system, written `<system>-<architecture>` as Go names them: `windows-amd64`, `linux-amd64`,
    `darwin-arm64`. Each has the archive (`.zip`, `.tar.gz` or `.tgz`) either as `path` (a file next to
    `bundle.json`; the site publishes it) or as `url` (it stays where it is; the tool downloads it once to check it).
    `sha256` is `sha256:` and the 64 hex characters of the archive's SHA-256. `strip` removes that many leading
    folders from the paths in the archive (like `tar --strip-components`).
  - Every system the component has an archive for needs a `dir` in `launcher.json`. A component in the launcher's
    folder (`{appdir}/<folder>`) is put into the Windows packages, so it works right after installing. A component
    elsewhere (`{userdata}/<folder>`, `{home}/<folder>`) is left out; the launcher downloads and installs it at its
    first start. On macOS a component must be elsewhere, for example `{userdata}/game` (see `macos` below).
- `flatpak` (only for the `flatpak` command): how the Flatpak is built.
  - `runtime`, `runtime_version`, `sdk`: the Flatpak runtime, for example `org.freedesktop.Platform`, `26.08` and
    `org.freedesktop.Sdk`. Pick a version that Flathub still supports.
  - `finish_args`: the permissions of the app, one option per entry. The launcher needs at least
    `--share=network`, a display (`--socket=wayland`, `--socket=fallback-x11`, `--share=ipc`) and `--device=dri`.
    Add what your game needs, for example `--socket=pulseaudio` for sound. If your game has its own Flatpak, copy
    its permissions: Flathub's UltraStar Deluxe, for example, also has `--device=all` (all devices) and
    `--persist=.ultrastardx` (its settings folder). A game definition adds them for you.
  - `modules`: your [flatpak-builder modules](https://docs.flatpak.org/en/latest/manifests.html#modules), one file
    each (`.yml` or `.json`). They build or copy your game into the Flatpak. Paths of their sources are relative to
    the module file. The launcher's folder in the Flatpak is `/app/lib/<id>` (`<id>` from `launcher.json`), so
    install your game to where `launcher.json` starts it, for example `/app/lib/<id>/game/`.
  - `sdk_extensions` (optional): SDK extensions the modules need to build, for example a compiler like
    `org.freedesktop.Sdk.Extension.freepascal` (the manifest's `sdk-extensions`).
  - `build_options` (optional): the manifest's
    [`build-options`](https://docs.flatpak.org/en/latest/flatpak-builder-command-reference.html) for all modules, as
    an object, for example `{"append-path": "/usr/lib/sdk/freepascal/bin"}`.
  - `cleanup` (optional): files removed after all modules are built (the manifest's `cleanup`), for example
    `["/include", "/lib/pkgconfig"]`.
- `macos` (optional, only for the `macos` command): `apps`, a list of apps that go into the launcher's app, each
  `{"url": "<a .dmg file>", "sha256": "sha256:…", "app": "<the app's folder in the .dmg, like Game.app>"}`. They go
  to `Contents/Helpers/` of the launcher's app. Only macOS can open a `.dmg`, so then the `macos` command must run on
  a Mac.

`launcher.json` needs `app_id` for the Flatpak and the macOS app (it is their name, for example
`org.example.Karaoke`) and `updates` for the site.

## Games

`karaoke-bundle` knows these games (their definitions are in [games/](../games) of this repository):

- `ultrastar-deluxe`: [UltraStar Deluxe](https://usdx.eu/) 2026.11.0.

With `"game": "ultrastar-deluxe"` in `bundle.json` you don't write the game's archives, start command, folders or
Flatpak modules yourself: the tool adds them to `bundle.json` and `launcher.json`, and the packages get the merged
`launcher.json`. `karaoke-bundle check --launcher` prints it. What you write yourself wins:

- Objects are merged key by key. A file (an object with `url` or `path`, like an archive) replaces the game's as a
  whole.
- Lists inside `flatpak` (`finish_args`, `sdk_extensions`, `cleanup`) are joined: the game's first, each entry once.
- Any other value you write replaces the game's. For example `"start": {"windows": [...]}` in your `launcher.json`
  replaces the start command on Windows and keeps the game's on Linux and macOS.

What UltraStar Deluxe gets on each system (component `ultrastar-deluxe`):

- **Windows:** the game's portable zip from its GitHub release, in `game/` of the launcher's folder, so it is in the
  setup program and the portable zip. The launcher keeps it up to date through `app.json`. The game writes its
  settings, scores (`Ultrastar.db`), playlists and screenshots into that folder. Updates and uninstalling remove only
  files that were installed, so these stay, and installing again uses them.
- **Linux:** the game is built from source inside your Flatpak (modules taken from Flathub's UltraStar Deluxe). This
  needs the Free Pascal SDK extension and takes long the first time (it builds OpenCV too). Its settings are in
  `~/.ultrastardx` inside the Flatpak.
- **macOS:** the game's own app from its `.dmg`, inside the launcher's app (`Contents/Helpers/`). It is updated
  together with the launcher's app, by a new download. The `macos` command must run on a Mac.
- **All:** the start command is the game with `-SongPath {library}`; the game searches that folder and its subfolders,
  so it finds the songs of every feed. Theme packs (pack kind `theme`) go to the game's themes folder.

UltraStar Deluxe is under the GPL (version 2 or later), which asks whoever passes on its programs to also offer its
source code. Every package gets `licenses/ultrastar-deluxe.txt` with the address of the source code of exactly the
shipped version, and `check` prints it. Link it on your download page, too. (This is the least the licence asks for;
whether it is enough for you is your decision.)

### Your own game definition

A game definition is a folder with `game.json` and the files it names:

```json
{
  "game": 1,
  "id": "my-game",
  "name": "My Game",
  "version": "1.0",
  "license": "MIT",
  "homepage": "https://example.org/",
  "source": "https://example.org/my-game/source/",
  "bundle": { "components": {}, "flatpak": { "modules": ["flatpak/my-game.yml"] } },
  "launcher": { "start": {}, "components": {}, "content": {} }
}
```

`bundle` and `launcher` hold parts of `bundle.json` and `launcher.json`, written as there. `flatpak.modules` are
relative to the definition's folder. Use it with `"game": {"path": "my-game"}`, or offer it to this repository.

## Commands

```
karaoke-bundle check   [--bundle bundle.json] [--launcher]
karaoke-bundle keygen  <file>
karaoke-bundle windows [--bundle bundle.json] [--out dist] [--launcher-dir <folder>]
karaoke-bundle macos   [--bundle bundle.json] [--out dist] [--launcher-dir <folder>]
karaoke-bundle flatpak [--build] [--bundle bundle.json] [--out dist] [--launcher-dir <folder>]
karaoke-bundle site    --key <file or -> [--bundle bundle.json] [--out dist]
```

- `--bundle`: the `bundle.json` to use (default: `bundle.json` in the current folder).
- `--out`: the folder for the results (default: `dist`). `dist/cache` keeps downloads between runs.
- `--launcher-dir`: take the launcher programs from this folder instead of downloading the release. They must be
  named as in a release, for example `karaoke-launcher-windows-amd64.exe`. For testing your own launcher builds.

### `check`

Reads `bundle.json` and `launcher.json` and prints every problem at once. All other commands do the same check
first. With a game it prints the game and the address of its source code; `--launcher` prints `launcher.json` as the
packages get it.

### `keygen`

Makes the key that signs `app.json` and prints its public key. Put the public key into `updates.key` of
`launcher.json`. Keep the key file secret and make a backup: whoever has it can send programs to all your players,
and without it you can't publish updates that existing installations accept.

### `windows`

Writes `dist/windows/`:

- `<id>-<version>-windows-setup.exe`: the setup program. It installs for the current user only, so it needs no
  administrator rights: into `%LOCALAPPDATA%\Programs\<id>`, with an entry in the Start menu and in the list of
  installed apps. Uninstalling removes only what the setup program and the launcher's updates installed (the launcher
  lists those files); the songs, the settings and whatever the game wrote itself (scores, its settings) stay, and
  installing again uses them. The setup program is only made when `makensis`
  ([NSIS](https://nsis.sourceforge.io/), version 3) is installed; otherwise run `makensis installer.nsi` in
  `dist/windows` later.
- `<id>-<version>-windows-portable.zip`: the same files to unpack anywhere.
- `launcher-<version>-windows-amd64.zip`: the launcher for `app.json` (the site publishes it).
- `installer.nsi`, `installer-files/`, `karaoke-launcher.ico`: what the setup program is made from.

Both downloads already contain your game (the `windows-amd64` archive of every component), so players can play
right after installing.

### `macos`

Writes `dist/macos/<id>-<version>-macos-arm64.zip` with `<Name>.app` inside, for Macs with Apple Silicon. On a Mac
(where `hdiutil` exists) it also writes a `.dmg`. Run it on Linux or macOS: Windows loses the executable bit of the
programs, and the app would not start. With `macos.apps` (UltraStar Deluxe has one) it must run on a Mac.

- The app contains the launcher: the program in `Contents/MacOS`, and `launcher.json` with your files in
  `Contents/Resources`, which is the launcher's folder (`{appdir}`) on macOS. The apps of `macos.apps` go to
  `Contents/Helpers/`; `start.macos` reaches them from the launcher's folder, like
  `["../Helpers/Game.app/Contents/MacOS/game", …]`.
- The app never changes itself. The launcher does not update itself on macOS: when `app.json` lists a newer version,
  it shows "A new version is available" with a button that opens `updates.page`. A component must be outside the app
  (`dir.macos` like `{userdata}/game`, and `start.macos` like `["{userdata}/game/game", …]`); the launcher installs
  it at its first start from `app.json` and keeps it up to date. A game inside the app (`macos.apps`) is only updated
  by a new download of the app. So the app's signature stays valid, and it doesn't matter that macOS may start a
  downloaded app from a read-only copy.
- The app is signed ad hoc (a signature without a certificate) when `codesign` (on a Mac) or
  [`rcodesign`](https://gregoryszorc.com/docs/apple-codesign/main/) (on Linux) is installed; otherwise the command
  says that it is not signed. The apps in `Contents/Helpers/` are signed first, then the launcher's app (Apple's
  [TN2206](https://developer.apple.com/library/archive/technotes/tn2206/_index.html): inside out). Ad hoc is not
  the same as a signature by Apple: macOS still warns. See [publishing.md](publishing.md) for what players see.
- The bundle identifier of the app is `app_id`; `_` becomes `-`, because macOS doesn't allow `_` there.

### `flatpak`

Writes `dist/flatpak/<app_id>.json` (the manifest for flatpak-builder) and `dist/flatpak/files/` (the launcher, its
files, the menu entry, the software centre description and the icons). The manifest first builds the game's modules
(copied to `dist/flatpak/games/<id>/`), then your `modules`, then adds the launcher.

The `launcher.json` in the Flatpak has no `updates` and no `components`: Flatpak updates the app itself, the
launcher and your game together.

With `--build` it also builds the Flatpak. That needs `flatpak` and `flatpak-builder`; the runtime is downloaded
from [Flathub](https://flathub.org). It writes:

- `dist/flatpak/repo/`: the repository players update from. Keep it between releases: each build adds the new
  version, and players then download only what changed.
- `dist/flatpak/<app_id>.flatpak`: one file with the app. Installing it also adds your repository for updates.
- `dist/flatpak/<app_id>.flatpakref`: a small file that installs the app from your repository.

The repository is not signed with GPG. Flatpak accepts that; the `https://` connection protects the download.

### `site`

Writes `dist/site/`, the folder for your web server at `site`:

- `app.json` and `sig/<hash>.sig`: the list of programs, signed with the key. `--key -` reads the key from standard
  input, for example from a secret in a build service.
- `files/`: the launcher archives the other commands made and the component archives given by `path`.
- `download/`: the setup program, the zips, the `.flatpak` and the `.flatpakref`.
- `flatpak/repo/`: the Flatpak repository, when it was built.

Run `windows`, `macos` and `flatpak --build` first; `site` takes what they left in `dist/`. It stops when the key
doesn't match `updates.key` in `launcher.json`, because the launcher would refuse `app.json`.

## Walk-through with the example bundle

On Linux, with Go installed (for the dummy game), in a copy of [examples/bundle](../examples/bundle):

1. Build the dummy game: `./make-game.sh 1.0.0`. It writes `game/game-*.zip` and their hashes into `bundle.json`.
2. Make a key: `karaoke-bundle keygen ~/example.key`. Put the public key it prints into `updates.key` of
   `launcher.json`.
3. Check: `karaoke-bundle check`.
4. Build: `karaoke-bundle windows`, `karaoke-bundle macos` (signed if rcodesign is installed) and, if
   flatpak-builder is installed,
   `karaoke-bundle flatpak --build`. Without a launcher release that has the version in `bundle.json`, build the
   launcher yourself and add `--launcher-dir <folder>`.
5. Make the site: `karaoke-bundle site --key ~/example.key`.
6. Serve it where `site` says (`http://127.0.0.1:8090/`): `python3 -m http.server 8090 --bind 127.0.0.1 -d dist/site`.

Then install the portable zip or the `.flatpak`, raise `version` in `bundle.json` (and the game's version with
`./make-game.sh 1.1.0`), and run steps 4 to 6 again. The launcher you installed finds the new version at its next
start.
