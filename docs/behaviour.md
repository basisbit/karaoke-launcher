# What the launcher does when (behaviour specification)

This is the specification of the launcher window's behaviour: for every situation, what Play shows, what the status
line says, whether the launcher closes, and what is on disk. It describes the behaviour of version 0.2 plus the M13
fixes. The rules below cover every combination of the five inputs (2 × 5 × 3 × 5 × 3 = 450 situations): each output
is decided by its own table, read top to bottom, the first matching line wins.

## Inputs

| Input | Values |
|---|---|
| Songs | none in the song folder · some |
| Download (feed sync) | idle · running · paused · failed (a problem in the last run) · offline (the feed or every failed file unreachable) |
| Game | not started · running (started by the launcher) · exited |
| Program update | none · game update downloaded · launcher update downloaded · game update failed half-way · user chose "Later" |
| Action | window closed · Play pressed · Restart pressed |

"Game update" stands for any component except the launcher. While a game update is installed (from "close the game"
until it is in place) the launcher is *installing*.

## Play button

| Situation | Play |
|---|---|
| game running | disabled |
| installing a game update | disabled |
| game update failed half-way | disabled until the next start finishes it |
| no songs | enabled, "Choose your songs first": opens the songs screen |
| otherwise | enabled, "Play" |

A running, paused, failed or offline download never disables Play: new songs show up at the game's next start.
A downloaded launcher update does not disable Play either.

## Status line (home screen)

| Situation | Text |
|---|---|
| no songs, download idle, no result and no error yet | empty |
| game running | "The game is running. The launcher closes when the download is finished." |
| download running | "Downloading songs: x of y, a of b" / "Finishing …" / "Looking for new songs …"; with several sources prefixed by the source number |
| download paused | "Download paused" |
| download offline | "No connection. Will try again later.", with Details |
| download failed | "Some songs could not be downloaded. They will be tried again later.", with Details |
| otherwise | "Everything is up to date" |

The songs screen shows the same line, without the "empty" case. Details is shown only when nothing runs.

## Program update row (home screen)

| Situation | Row |
|---|---|
| installing a game update | "Getting the game ready …" |
| game update failed half-way | "The game could not be updated completely. Restart the launcher to finish the update." |
| launcher update being downloaded | "Downloading an update …" |
| launcher update downloaded | "An update is ready." with Restart |
| `app.json` has nothing for this system, but `updates.page` is set | "A new version is available" with a link |
| otherwise | hidden |

A game update downloaded at an earlier start is installed first, before the check for new ones. Before installing,
the user is asked to close the game; "Later" leaves it downloaded for the next start and Play enabled.

## Does the launcher close?

| Situation | Closes |
|---|---|
| Play was never pressed | no (only by the window's close button) |
| still starting (feeds are checked) | not yet |
| checking for, downloading or installing program updates | not yet |
| download running | not yet |
| otherwise, after Play | yes, right away (paused, failed and offline downloads count as finished) |

So after Play the launcher closes as soon as nothing runs; when the game exits first, it stays until the download is
finished. A paused download stays paused and continues at the next start.

## Actions

| Action | Situation | Result |
|---|---|---|
| window closed | download running | asks "stop and close?": yes pauses the download and closes; it continues next time |
| window closed | otherwise | closes at once. A game update being installed is finished at the next start (it can be interrupted at any point) |
| Play pressed | no songs | the songs screen |
| Play pressed | songs | the game starts with the song folder; the launcher closes when nothing runs (table above) |
| Restart pressed | launcher update downloaded | the download is paused, the update installed, the new version started, this one closes |
| Restart pressed | installing it failed | an error; the download stays paused (Resume continues it) |

## On disk

| Situation | Song folder | Program folders |
|---|---|---|
| download running or paused | finished songs complete and visible; songs in progress have `.kfeed-part-…` files and no visible txt | unchanged |
| download failed or offline | what finished is there; the rest is downloaded next time | unchanged |
| a pack removed | its songs deleted, except files the user changed (their txt is hidden) | unchanged |
| update downloaded, not installed | unchanged | `.update-<component>/` next to the component, bound to the installed version |
| another version installed by a setup meanwhile | unchanged | the downloaded update is thrown away, never installed over it |
| update installed | unchanged | new files in place; `.component-<id>.json` lists them; kept files (settings) untouched |
| update interrupted | unchanged | old and new files mixed; Play disabled; the next start finishes it |
| second launcher start | unchanged | `show-window` in the user folder: the open window comes to the front |
