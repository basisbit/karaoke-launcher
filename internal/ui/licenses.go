package ui

import (
	_ "embed"
	"regexp"
	"strings"
)

//go:generate go run gen_licenses.go

// licenseText holds the licence texts of Go and of every library and font in the program (see gen_licenses.go).
//
//go:embed licenses.txt
var licenseText string

// licenceSection is the licence text of one part of the program.
type licenceSection struct{ title, text string }

// licenceSections splits licenseText at the "=====" lines that gen_licenses.go writes before every part.
func licenceSections() []licenceSection {
	var out []licenceSection
	parts := strings.Split(licenseText, "\n"+strings.Repeat("=", 60)+"\n")
	for _, p := range parts[1:] { // parts[0] is the heading
		title, text, _ := strings.Cut(p, "\n")
		out = append(out, licenceSection{title: title, text: strings.TrimSpace(text)})
	}
	return out
}

// licenceFile is one licence file of a part: its name and its text.
type licenceFile struct{ name, text string }

var fileHeadRe = regexp.MustCompile(`(?m)^-{60}\n--- (.+) ---\n`)

// files splits a part into its licence files.
func (s licenceSection) files() []licenceFile {
	idx := fileHeadRe.FindAllStringSubmatchIndex(s.text, -1)
	var out []licenceFile
	for i, m := range idx {
		end := len(s.text)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		out = append(out, licenceFile{name: s.text[m[2]:m[3]], text: strings.TrimSpace(s.text[m[1]:end])})
	}
	return out
}

var listItemRe = regexp.MustCompile(`^\s*([*•-]|\d+[.)]|\([a-z0-9]+\)|[a-z][.)])\s`)

// reflow joins the hard-wrapped lines of a licence text into paragraphs, so that it wraps to any window width.
// Blank lines and list items start new lines.
func reflow(text string) string {
	var out []string
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		var lines []string
		for _, l := range strings.Split(para, "\n") {
			t := strings.TrimSpace(l)
			if t == "" {
				continue
			}
			if len(lines) > 0 && !listItemRe.MatchString(l) {
				lines[len(lines)-1] += " " + t
			} else {
				lines = append(lines, t)
			}
		}
		if len(lines) > 0 {
			out = append(out, strings.Join(lines, "\n"))
		}
	}
	return strings.Join(out, "\n\n")
}
