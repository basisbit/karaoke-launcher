# karaoke-launcher

A desktop launcher for karaoke communities. Players use it to pick the song packs they want, download them, keep
them up to date and start their karaoke game, without admin rights and without a huge all-or-nothing download.

**Status: early.** Nothing here is ready for players yet. The file formats (`launcher.json`, `bundle.json`,
`game.json`, `app.json`) will change without notice before 1.0.

## Who it is for

- **Players and party hosts:** choose your songs, get a small first download so you can play quickly, prepare at home
  and play offline. Interrupted downloads continue where they stopped. Press Play and the launcher starts the game
  and closes (after a running download has finished).
- **Communities:** ship the launcher with your own `launcher.json`, which sets your name, logo, colours, links, news,
  your song feeds and how to start your game. You don't need to change any code.

The launcher can also keep itself and the community's game up to date from a file the community signs with its own
key. See [docs/updates.md](docs/updates.md).

For communities there is also `karaoke-bundle`, a tool that turns your `launcher.json`, your files and your game into
a Windows setup program, a Flatpak for Linux, a macOS app and a folder for your web server. See
[docs/bundles.md](docs/bundles.md) and [docs/publishing.md](docs/publishing.md).

The launcher works with any game that reads songs from a folder. It contains no code for one particular game;
`karaoke-bundle` knows how to ship UltraStar Deluxe (see [Games](docs/bundles.md#games)).
Songs come from feeds in the [karaoke-feeds](https://github.com/basisbit/karaoke-feeds) format. Each song has a
checksum, and a feed can be signed.

## Building

You need Go and a C compiler plus the OpenGL and X11 development files that [Fyne](https://fyne.io) needs. On Linux,
see the package list in Fyne's [Getting started](https://docs.fyne.io/started/) guide.

```
go build -o karaoke-launcher ./cmd/karaoke-launcher
go test ./...
```

The bundle tool is plain Go and builds for any system without a C compiler:

```
CGO_ENABLED=0 go build -o karaoke-bundle ./cmd/karaoke-bundle
```

## Trying it

```
./karaoke-launcher --config examples/<name>/launcher.json
```

The `examples/` folder has sample configurations with self-made assets. One of them uses a feed on your own computer,
served with `kfeed serve` from karaoke-feeds.

## Known limits

- Fyne draws with OpenGL. On a PC with a broken graphics driver the window may not open.
- The folder picker is Fyne's own, not the system's file dialog.

## License

MIT, see [LICENSE](LICENSE).
