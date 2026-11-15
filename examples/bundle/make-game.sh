#!/usr/bin/env bash
# Builds the dummy game of the example bundle for Windows and Linux (amd64), packs each build into
# game/game-<system>.zip and writes the version and the SHA-256 of the archives into bundle.json.
# Usage: ./make-game.sh 1.0.0   (needs Go and Python 3)
set -euo pipefail
cd "$(dirname "$0")"
version=${1:?usage: make-game.sh <version>}
for platform in windows-amd64 linux-amd64; do
	goos=${platform%-*}
	goarch=${platform#*-}
	exe=game
	if [ "$goos" = windows ]; then exe=game.exe; fi
	tmp=$(mktemp -d)
	CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -trimpath -ldflags "-X main.version=$version" -o "$tmp/$exe" ./game/main.go # one file: works also outside this repository
	python3 - "$tmp/$exe" "game/game-$platform.zip" <<'PY'
import os, sys, zipfile
src, out = sys.argv[1], sys.argv[2]
info = zipfile.ZipInfo(os.path.basename(src), date_time=(2026, 1, 1, 0, 0, 0))
info.external_attr = 0o100755 << 16  # a program: the executable bit is kept on Linux and macOS
info.compress_type = zipfile.ZIP_DEFLATED
with zipfile.ZipFile(out, "w") as z, open(src, "rb") as f:
    z.writestr(info, f.read())
PY
	rm -r "$tmp"
done
python3 - "$version" <<'PY'
import hashlib, json, sys
with open("bundle.json", encoding="utf-8") as f:
    b = json.load(f)
game = b["components"]["game"]
game["version"] = sys.argv[1]
for platform, entry in game.items():
    if isinstance(entry, dict):
        with open(entry["path"], "rb") as f:
            entry["sha256"] = "sha256:" + hashlib.sha256(f.read()).hexdigest()
with open("bundle.json", "w", encoding="utf-8") as f:
    f.write(json.dumps(b, indent=2, ensure_ascii=False) + "\n")
PY
echo "game $version: game/game-windows-amd64.zip, game/game-linux-amd64.zip; bundle.json updated"
