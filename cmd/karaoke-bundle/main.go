// Command karaoke-bundle turns a community's launcher.json and bundle.json into the files it publishes. See
// docs/bundles.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	kfeed "github.com/basisbit/karaoke-feeds/tool"
	"github.com/basisbit/karaoke-launcher/internal/bundle"
)

// version is set by the release build (-ldflags "-X main.version=…").
var version = "dev"

const usage = `karaoke-bundle %s: makes the downloads of a karaoke bundle.

Usage:
  karaoke-bundle check   [--bundle bundle.json]   check bundle.json and launcher.json
  karaoke-bundle keygen  <file>                   make a key for signing app.json
  karaoke-bundle windows [--bundle bundle.json] [--out dist] [--launcher-dir <folder>]
                                                  make the Windows downloads in <out>/windows
  karaoke-bundle flatpak [--build] [--bundle bundle.json] [--out dist] [--launcher-dir <folder>]
                                                  write <out>/flatpak: the manifest and files for flatpak-builder;
                                                  --build also builds the repository, the .flatpak and .flatpakref
  karaoke-bundle site    --key <file or -> [--bundle bundle.json] [--out dist]
                                                  make <out>/site, the folder for the web server:
                                                  app.json signed with the key (- reads it from stdin)

Put the public key that keygen prints into "updates.key" of launcher.json. Keep the key file secret.
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, usage, version)
		return 2
	}
	switch args[0] {
	case "check":
		return check(args[1:], stdout, stderr)
	case "windows":
		return windows(args[1:], stdout, stderr)
	case "flatpak":
		return flatpak(args[1:], stdout, stderr)
	case "site":
		return site(args[1:], stdout, stderr)
	case "keygen":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: karaoke-bundle keygen <file>")
			return 2
		}
		return kfeed.Run([]string{"kfeed", "keygen", args[1]}, version)
	case "help", "-h", "--help":
		fmt.Fprintf(stdout, usage, version)
		return 0
	}
	fmt.Fprintf(stderr, "unknown command %q\n\n"+usage, args[0], version)
	return 2
}

// common are the flags of every command that reads a bundle.
type common struct {
	fs                *flag.FlagSet
	bundle, out, from *string
}

func newFlags(name string, stderr io.Writer) common {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return common{fs: fs, bundle: fs.String("bundle", "bundle.json", "the bundle.json to use"),
		out:  fs.String("out", "dist", "the folder for the results"),
		from: fs.String("launcher-dir", "", "take the launcher programs from this folder instead of downloading the release")}
}

// load reads the bundle and prints every problem.
func (c common) load(stderr io.Writer) *bundle.Bundle {
	b, err := bundle.Load(*c.bundle)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return nil
	}
	return b
}

func check(args []string, stdout, stderr io.Writer) int {
	c := newFlags("check", stderr)
	if c.fs.Parse(args) != nil || c.fs.NArg() != 0 {
		return 2
	}
	b := c.load(stderr)
	if b == nil {
		return 1
	}
	fmt.Fprintf(stdout, "%s %s is fine: %d files to ship, %d components.\n", b.Config.ID, b.Version, len(b.Shipped), len(b.Components))
	return 0
}

func windows(args []string, stdout, stderr io.Writer) int {
	c := newFlags("windows", stderr)
	if c.fs.Parse(args) != nil || c.fs.NArg() != 0 {
		return 2
	}
	b := c.load(stderr)
	if b == nil {
		return 1
	}
	files, err := b.Windows(context.Background(), bundle.Options{Out: *c.out, LauncherDir: *c.from})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, f := range files {
		fmt.Fprintln(stdout, f)
	}
	if len(files) == 3 {
		fmt.Fprintln(stdout, "makensis was not found: build the setup program later with makensis installer.nsi")
	}
	return 0
}

func site(args []string, stdout, stderr io.Writer) int {
	c := newFlags("site", stderr)
	key := c.fs.String("key", "", "the key file that signs app.json; - reads it from standard input")
	if c.fs.Parse(args) != nil || c.fs.NArg() != 0 || *key == "" {
		fmt.Fprintln(stderr, "usage: karaoke-bundle site --key <file or -> [--bundle bundle.json] [--out dist]")
		return 2
	}
	b := c.load(stderr)
	if b == nil {
		return 1
	}
	notes, err := b.BuildSite(context.Background(), bundle.Options{Out: *c.out}, *key)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, n := range notes {
		fmt.Fprintln(stdout, n)
	}
	fmt.Fprintf(stdout, "%s: copy this folder to %s\n", filepath.Join(*c.out, "site"), b.Site)
	return 0
}

func flatpak(args []string, stdout, stderr io.Writer) int {
	c := newFlags("flatpak", stderr)
	build := c.fs.Bool("build", false, "also run flatpak-builder and make the repository, the .flatpak and the .flatpakref")
	if c.fs.Parse(args) != nil || c.fs.NArg() != 0 {
		return 2
	}
	b := c.load(stderr)
	if b == nil {
		return 1
	}
	o := bundle.Options{Out: *c.out, LauncherDir: *c.from}
	files := []string{}
	var err error
	if *build {
		files, err = b.BuildFlatpak(context.Background(), o, stderr)
	} else {
		var manifest string
		manifest, err = b.FlatpakFiles(context.Background(), o)
		files = append(files, manifest)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, f := range files {
		fmt.Fprintln(stdout, f)
	}
	return 0
}
