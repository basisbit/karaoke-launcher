package ui

import (
	"strings"
	"testing"
)

func TestLicenceSections(t *testing.T) {
	s := licenceSections()
	if len(s) < 20 || s[0].title != "Go (standard library and runtime)" || !strings.Contains(s[0].text, "The Go Authors") {
		t.Fatalf("%d sections, first %q", len(s), s[0].title)
	}
	for _, x := range s {
		if x.title == "" || len(x.text) < 100 {
			t.Errorf("section %q has %d bytes of text", x.title, len(x.text))
		}
		if strings.HasPrefix(x.title, "fyne.io/fyne/v2") && !strings.Contains(x.text, "theme/font/LICENSE_Inter.txt") {
			t.Error("Fyne's font licences are missing")
		}
	}
}

func TestReflow(t *testing.T) {
	in := "Copyright 2009 The Go Authors.\n\nRedistribution and use in source\nand binary forms are permitted:\n\n   * one\n     more\n   * two\n\n1. first\n2. second"
	want := "Copyright 2009 The Go Authors.\n\nRedistribution and use in source and binary forms are permitted:\n\n* one more\n* two\n\n1. first\n2. second"
	if got := reflow(in); got != want {
		t.Fatalf("%q\nwant %q", got, want)
	}
	files := licenceSections()[0].files()
	if len(files) < 2 || files[0].name != "LICENSE" || files[1].name != "PATENTS" || !strings.HasPrefix(files[0].text, "Copyright") {
		t.Fatalf("Go's licence files: %+v", files)
	}
}
