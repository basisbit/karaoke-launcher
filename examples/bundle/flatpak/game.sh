#!/bin/sh
# Dummy game of the example bundle (Flatpak): writes its arguments into game-run.txt in its data folder and exits.
{
	echo "version flatpak"
	for a in "$@"; do echo "$a"; done
} >"${XDG_DATA_HOME:-$HOME}/game-run.txt"
