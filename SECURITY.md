# Security policy

## Reporting a vulnerability

Please report security problems **privately**, not in a public issue:
use **"Report a vulnerability"** on the Security tab of this repository (GitHub private vulnerability reporting).

Please include what is affected (the launcher, a part of `launcher.json`, how it starts programs or downloads),
how to reproduce it, and what an attacker could do with it. You'll get an answer within about a week. This is a
volunteer project, so fixes may take longer; we'll agree with you on when to publish details. There is no bug bounty.

## What is in scope

- the launcher, in the latest release and on `main`
- how it handles feeds: a feed is untrusted input; the launcher must never take a path, a command or HTML from it
- how it reads `launcher.json` and starts the configured program

Out of scope: the content of feeds (report it to the feed's operator; every feed names a contact), bugs in the
download engine itself (report them at [karaoke-feeds](https://github.com/basisbit/karaoke-feeds/security)), and
vulnerabilities in games, graphics drivers or operating systems.

## Supported versions

Only the latest release gets fixes.
