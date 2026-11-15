// Package games holds the game definitions that karaoke-bundle knows: games/<id>/game.json and the files it refers
// to. A bundle picks one with "game" in bundle.json. See docs/bundles.md.
package games

import "embed"

// FS has one folder per game, named by its id.
//
//go:embed */game.json */flatpak
var FS embed.FS
