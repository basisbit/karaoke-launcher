# Program updates

This page is for communities that ship the launcher. It explains how the launcher keeps itself and your game up to
date, and what you have to publish for that.

The short version: you publish a small file `app.json` on your website and sign it with your own key. The launcher
checks it at every start. When it lists a newer version of the launcher or the game, the launcher downloads it,
checks it and installs it. Players don't have to do anything.

## What you put into `launcher.json`

```json
"app_id": "org.example.Karaoke",
"updates": {
  "url": "https://example.org/karaoke/app.json",
  "key": "ed25519:…",
  "page": "https://example.org/karaoke/"
},
"components": {
  "game": {
    "dir": { "windows": "{appdir}/game" },
    "keep": ["config.ini", "*.db"]
  }
}
```

- `app_id` (optional): the name of your app in reverse-DNS form, for example `org.example.Karaoke`. At least three
  parts joined by `.`; letters, digits, `_` and `-`; at most 255 characters. The launcher and the packages for
  Linux and macOS use it.
- `updates.url`: where `app.json` is. It must start with `https://` (`http://` only for `127.0.0.1` or `localhost`,
  for testing).
- `updates.key`: the public key that signs `app.json`: `ed25519:` and 43 characters. Keep the private key safe.
  Whoever has it can send programs to all your players.
- `updates.page` (optional): a web page where players can download a new version by hand. It is shown when the
  launcher can't update itself (see below).
- `components`: the programs the launcher installs besides itself. Usually that is your game. The name `launcher`
  is taken: it is the launcher itself, and its folder is always the launcher's own folder.
  - `dir`: the folder per system (`windows`, `linux`, `macos`). It must start with `{appdir}` (the launcher's
    folder), `{userdata}` (the launcher's per-user folder) or `{home}` (the user's home folder), and must not
    contain `..`. A system without a folder gets no updates of this component.
  - `keep`: files that are never overwritten and never deleted once they exist, for example settings and scores.
    Each entry is a pattern like `config.ini`, `*.db` or `data/*.cfg` (`*` and `?` work within one folder level).
    A pattern matches the whole path inside the component's folder or just the file name.

## The file `app.json`

```json
{
  "app": 1,
  "id": "example-karaoke",
  "sequence": 1794650400,
  "components": {
    "launcher": {
      "version": "1.3.0",
      "files": {
        "windows-amd64": { "url": "files/launcher-1.3.0-windows-amd64.zip", "sha256": "sha256:<hex>", "size": 12345678 }
      }
    },
    "game": {
      "version": "2.0.0",
      "files": {
        "windows-amd64": { "url": "https://example.org/game-2.0.0.zip", "sha256": "sha256:<hex>", "size": 98765432, "strip": 1 }
      }
    }
  }
}
```

- `app` is always `1`. `id` must be the `id` of your `launcher.json`.
- `sequence` must grow with every new `app.json`. The Unix time of publishing works well. The launcher remembers the
  highest number it has seen and ignores an `app.json` with a lower one, so an old file can't be sent again to
  install an old version.
- `version` is a text. The launcher doesn't compare versions: any version that differs from the installed one is
  installed. To go back to an older version, list it with a higher `sequence`.
- `files`: one archive per system. The key is the Go name of the system and processor: `windows-amd64`,
  `linux-amd64`, `darwin-arm64` and so on.
  - `url`: a full `https://` address, or an address relative to `app.json`.
  - `sha256` and `size` of the archive. The launcher refuses an archive that doesn't match.
  - Archives must be `.zip`, `.tar.gz` or `.tgz`.
  - `strip` (optional): how many folder levels to remove from every path in the archive. Use `1` when everything is
    inside one top folder such as `game-2.0.0/`.

### The signature

The launcher downloads `sig/<hash>.sig` from the folder of `app.json`. `<hash>` is the SHA-256 of the exact bytes of
`app.json`, written as 64 lower-case hex characters. The file holds an Ed25519 signature of those bytes in base64url
without padding. This is the same kind of key and signature that signed karaoke feeds use, so a key made with
`kfeed keygen` works. After every change to `app.json` you need a new signature.

If the signature is missing or wrong, or anything else about `app.json` is wrong, the launcher changes nothing and
tells the player nothing. Players are never asked to decide about an update they can't judge.

## What the launcher does

- **At every start** it fetches `app.json` (at most 1 MiB, 15 seconds for the whole check) and its signature.
- **Your game** is downloaded and installed at once. While that happens, Play is disabled and the start screen says
  "Getting the game ready …". If the launcher started the game, it waits until the game has ended. Before installing,
  it asks the player to close every game window that is still open, also ones started without the launcher. On Linux
  and Windows it then checks that no program runs from the game's folder and asks again while one does; on macOS the
  player's answer counts. If the player chooses "Later", the download is kept and the question comes again at the
  next start.
- **The launcher itself** is downloaded in the background. Then the start screen says "An update is ready." with a
  "Restart now" button. If the player ignores it, the update is installed at the next start.
- **When the launcher can't update itself** (no file for this system, for example macOS, or a folder the player
  can't write to), it shows "A new version is available." with a "Get it" button that opens `updates.page`.
  Without `updates.page` it shows nothing.
- If a download or an installation fails, the player sees one message, and it is tried again at the next start.
  When the game's installation stopped half-way, it has old and new files: Play stays disabled until the next start
  has finished the installation.
- No song or theme download runs while the game is installed. A theme folder may be inside the game's folder.

A component is installed for the first time only into a folder that is missing or empty. Folders the launcher made
itself count as empty: its song and theme folders (they have its `README.md`) and an interrupted first
installation. A folder that already has other files, but was never installed by the launcher, is left alone.

### What your packages must contain

The launcher knows what it installed from the file `.component-<component>.json` in the component's folder. Your
packages (zip, installer) must include it for every program they contain, or the launcher never updates that
program:

```json
{
  "component": "launcher",
  "version": "1.2.0",
  "files": ["karaoke-launcher.exe", "launcher.json", "assets/logo.svg"]
}
```

- `component`: `launcher`, or the name from `components`. `version`: the version in the package, as in `app.json`.
- `files`: every file of the package in that folder, with `/`, without the files that match `keep`. Only these
  files are replaced or deleted by later updates.

Without `.component-launcher.json` the launcher never updates itself and shows no notice: a launcher that a developer
builds and runs is never replaced. A game folder without its file is updated only when it is empty (see above).

## How files are replaced

1. The archive is downloaded into the launcher's per-user folder. An interrupted download continues later.
2. It is unpacked into a hidden folder `.update-<component>` next to the component. The whole archive is refused if
   one entry is unsafe: an absolute path, `..`, `\` or `:` in a path, a name that Windows keeps for devices (`con`,
   `nul`, `com1`, …), a name that ends with `.` or a space, a link or special file, two names that differ only in
   upper and lower case, a name that starts with `.component-` or `.update-`, more than 100,000 entries or more than
   16 GiB of data.
3. Files of the old version that the new version doesn't have are deleted first, so that a file can become a folder
   and the other way round. Files the launcher never installed, such as scores and settings the game writes, are
   never deleted. Neither are files that match `keep`. A file that can't be deleted because it is in use is renamed
   to `<name>.old-<number>` and deleted at the next start; if even that fails, the installation stops.
4. The new files are moved into place. A file that can't be replaced because it is in use is renamed first, in the
   same way. A folder is never moved aside to make room for a file: it may hold the player's files.
5. The list of installed files is saved in `.component-<component>.json` in the component's folder.

If the computer is switched off in the middle, the next start finishes the job. Nothing is written or deleted
through a link (symbolic link, junction) in the component's folder. On Windows and macOS, `Game.exe` and `game.exe`
are the same file.

## Notes

- Archives and `app.json` can be on different servers, but only `https://` addresses are accepted.
- Flatpak updates the programs in a Flatpak package itself. That is why the bundle tool leaves `updates` and
  `components` out of the `launcher.json` it puts into a Flatpak (planned; see the bundle tool documentation).
- Windows programs from the launcher are not signed with a certificate. Windows SmartScreen may warn about them, and
  macOS blocks an unsigned app until the user allows it.
