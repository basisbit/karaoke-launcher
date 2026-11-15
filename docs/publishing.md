# Publishing a bundle

This page is for communities that ship the launcher. It explains how to put a new release online. How to describe
the bundle and what each command makes is in [bundles.md](bundles.md).

Publishing means: copy the folder `dist/site/` that `karaoke-bundle site` makes to a web server, at the address
`site` in `bundle.json`. Any server that serves static files over `https://` works. No server program is needed.

## By hand

For every release:

1. Raise `version` in `bundle.json` (and the versions of components that changed, with their new archives and
   hashes).
2. Build: `karaoke-bundle windows`, `karaoke-bundle macos` (on a Mac when the bundle has `macos.apps`, as with
   UltraStar Deluxe), `karaoke-bundle flatpak --build` (on Linux, with `flatpak` and `flatpak-builder` installed).
   Keep `dist/flatpak/repo/` from the last release, so players download only what changed.
3. Make the site: `karaoke-bundle site --key <your key file>`.
4. Copy `dist/site/` to the server, for example `rsync -a --delete dist/site/ user@example.org:/var/www/karaoke/`.
   Copy the folders `files/`, `download/` and `flatpak/` first and `app.json` with `sig/` last: the launcher may
   read `app.json` while you copy, and it must not list files that are not there yet.
5. Link the files in `download/` on your website. With a game under the GPL (UltraStar Deluxe), also link the
   source code of the shipped version: `karaoke-bundle check` prints its address.

Every `app.json` gets a sequence number (the time it was made). The launcher refuses an `app.json` that is older
than one it has already seen, so nobody can send players back to an old version. Always publish the newest site.

Keep the key file out of your repository and out of the site. If you lose it, existing installations can't be
updated any more; if someone else gets it, they can send programs to all your players.

## With GitHub Actions and GitHub Pages

[examples/bundle/.github/workflows/release.yml](../examples/bundle/.github/workflows/release.yml) is a template for
a community repository that holds the bundle. When you push a version tag, it builds everything, attaches the
downloads to the GitHub release of the tag and publishes the site on GitHub Pages. The comments at its top say what
to set up once (the pinned `karaoke-bundle` version and hashes, the secret `APP_KEY`, Pages). The macOS app is made
on a Mac runner (`macos-latest`), which has `codesign` and `hdiutil`.

This template has not been run yet. Check the first release carefully.

- The site address is `https://<owner>.github.io/<repository>/`. Use it as `site` in `bundle.json` and add
  `app.json` for `updates.url` in `launcher.json`.
- The Flatpak repository starts new with every run (there is no earlier one in the build service). Players still get
  updates, but download the whole app each time.
- GitHub Pages sites may be at most 1 GB, with a soft limit of 100 GB of traffic per month
  ([limits](https://docs.github.com/en/pages/getting-started-with-github-pages/github-pages-limits)). The site holds
  your game several times (setup program, zips, Flatpak). With a big game, put the component archives elsewhere
  (`url` instead of `path`) or use your own server.

## Warnings players see

The setup program and the macOS app are not signed with a certificate. Signing costs money every year (a
code-signing certificate for Windows, a membership in the Apple Developer Program for macOS); this project doesn't
use it. Tell your players what to expect:

- **Windows:** Microsoft Defender SmartScreen warns about downloaded programs that it doesn't know yet, and by default
  lets the player run them anyway
  ([SmartScreen](https://learn.microsoft.com/en-us/windows/security/operating-system-security/virus-and-threat-protection/microsoft-defender-smartscreen/)).
  The player has to confirm in that warning that they want to run the program.
- **macOS:** macOS blocks the app because Apple has not checked it. According to Apple's
  [Safely open apps on your Mac](https://support.apple.com/en-us/102445), the player opens System Settings, clicks
  Privacy & Security, scrolls down and clicks "Open Anyway", then "Open" in the warning that comes again. After that
  the app opens normally. The same page says that an app that "has been modified or damaged" can't be opened at all,
  and an app without a valid signature may count as that. So sign the app ad hoc (see `macos` in
  [bundles.md](bundles.md)). Neither case has been tried on a Mac yet.
- **Linux:** nothing to sign. The Flatpak repository has no GPG signature, which Flatpak accepts; the `https://`
  connection protects the download.
