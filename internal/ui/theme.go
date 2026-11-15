// Package ui is the launcher's window: theme, screens and translations.
package ui

import (
	"image/color"
	"log"
	"math"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/basisbit/karaoke-launcher/internal/config"
)

// Theme is a Fyne theme built from the "look" of launcher.json. Colours that the config does not set come from
// Fyne's default theme; some are derived so that everything stays readable with any config colours.
type Theme struct {
	variant fyne.ThemeVariant
	colors  map[fyne.ThemeColorName]color.Color
	font    fyne.Resource
}

// NewTheme builds the theme for a config. A font that can't be loaded is reported in the log; the default is used.
func NewTheme(c *config.Config) *Theme {
	t := &Theme{variant: theme.VariantLight, colors: map[fyne.ThemeColorName]color.Color{}}
	if c.Look.Dark {
		t.variant = theme.VariantDark
	}
	def := theme.DefaultTheme()
	set := func(name fyne.ThemeColorName, hex string) color.Color {
		if col, ok := parseColor(hex); ok {
			t.colors[name] = col
			return col
		}
		return def.Color(name, t.variant)
	}
	primary := set(theme.ColorNamePrimary, c.Look.Colors.Primary)
	bg := set(theme.ColorNameBackground, c.Look.Colors.Background)
	text := set(theme.ColorNameForeground, c.Look.Colors.Text)
	// the outline of an unticked checkbox and of input fields: as soft as possible, but at least 3:1 (WCAG)
	border := text
	for w := 0.5; w < 1; w += 0.05 {
		if m := mix(text, bg, w); contrast(m, bg) >= 3 {
			border = m
			break
		}
	}
	t.colors[theme.ColorNameInputBorder] = border
	// text on the primary colour: buttons, the progress bar
	t.colors[theme.ColorNameForegroundOnPrimary] = onColor(primary)

	if p := c.Asset(c.Look.Font); p != "" {
		if r, err := fyne.LoadResourceFromPath(p); err == nil {
			t.font = r
		} else {
			log.Printf("font %s: %v (using the default font)", p, err)
		}
	}
	return t
}

// Color implements fyne.Theme. The variant always comes from the config, not from the system.
func (t *Theme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	if c, ok := t.colors[n]; ok {
		return c
	}
	return theme.DefaultTheme().Color(n, t.variant)
}

// Font implements fyne.Theme. The config font is used for normal text; monospace and symbols stay Fyne's.
func (t *Theme) Font(s fyne.TextStyle) fyne.Resource {
	if t.font != nil && !s.Monospace && !s.Symbol {
		return t.font
	}
	return theme.DefaultTheme().Font(s)
}

// Icon implements fyne.Theme.
func (t *Theme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

// Size implements fyne.Theme.
func (t *Theme) Size(n fyne.ThemeSizeName) float32 { return theme.DefaultTheme().Size(n) }

// Variant returns the variant from the config.
func (t *Theme) Variant() fyne.ThemeVariant { return t.variant }

func parseColor(s string) (color.Color, bool) {
	if len(s) != 7 || s[0] != '#' {
		return nil, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return nil, false
	}
	return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xff}, true
}

// mix returns a*w + b*(1-w).
func mix(a, b color.Color, w float64) color.Color {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	m := func(x, y uint32) uint8 { return uint8((float64(x)*w + float64(y)*(1-w)) / 257) }
	return color.NRGBA{m(ar, br), m(ag, bg), m(ab, bb), 0xff}
}

// luminance is the relative luminance of WCAG 2.
func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	ch := func(v uint32) float64 {
		s := float64(v) / 0xffff
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(r) + 0.7152*ch(g) + 0.0722*ch(b)
}

// contrast is the WCAG 2 contrast ratio of two colours (1 to 21).
func contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	return (math.Max(la, lb) + 0.05) / (math.Min(la, lb) + 0.05)
}

// onColor returns black or white, whichever is easier to read on bg.
func onColor(bg color.Color) color.Color {
	if contrast(color.White, bg) >= contrast(color.Black, bg) {
		return color.White
	}
	return color.Black
}
