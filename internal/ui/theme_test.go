package ui

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/basisbit/karaoke-launcher/internal/config"
)

func look(dark bool, primary, bg, text string) *config.Config {
	c := &config.Config{}
	c.Look.Dark = dark
	c.Look.Colors = config.Colors{Primary: primary, Background: bg, Text: text}
	return c
}

func TestThemeReadable(t *testing.T) {
	uiWait(func() { test.NewTempApp(t) })
	uiWait(func() { themeReadable(t) })
}

func themeReadable(t *testing.T) {
	for _, c := range []*config.Config{
		look(true, "#3b82f6", "#111827", "#f9fafb"),
		look(false, "#ea580c", "#fff7ed", "#431407"),
		look(false, "#facc15", "#ffffff", "#000000"), // light primary: needs dark text on it
		look(true, "", "", ""),                       // nothing set: Fyne's defaults
		look(false, "", "", ""),
	} {
		th := NewTheme(c)
		v := th.Variant()
		bg := th.Color(theme.ColorNameBackground, v)
		if r := contrast(th.Color(theme.ColorNameInputBorder, v), bg); r < 3 {
			t.Errorf("%+v: checkbox outline contrast %.2f < 3", c.Look.Colors, r)
		}
		if r := contrast(th.Color(theme.ColorNameForegroundOnPrimary, v), th.Color(theme.ColorNamePrimary, v)); r < 4.5 {
			t.Errorf("%+v: text on primary contrast %.2f < 4.5", c.Look.Colors, r)
		}
		if r := contrast(th.Color(theme.ColorNameForeground, v), bg); r < 4.5 {
			t.Errorf("%+v: text contrast %.2f < 4.5", c.Look.Colors, r)
		}
	}
}

func TestThemeVariantAndColors(t *testing.T) {
	uiWait(func() { test.NewTempApp(t) })
	uiWait(func() { themeVariantAndColors(t) })
}

func themeVariantAndColors(t *testing.T) {
	th := NewTheme(look(false, "#ea580c", "", ""))
	if th.Variant() != theme.VariantLight {
		t.Error("variant")
	}
	// the system's dark variant must not win over the config
	if got := th.Color(theme.ColorNameBackground, theme.VariantDark); got != theme.DefaultTheme().Color(theme.ColorNameBackground, theme.VariantLight) {
		t.Errorf("background follows the system variant: %v", got)
	}
	if got := th.Color(theme.ColorNamePrimary, theme.VariantLight); got != (color.NRGBA{0xea, 0x58, 0x0c, 0xff}) {
		t.Errorf("primary %v", got)
	}
	if _, ok := parseColor("#12345"); ok {
		t.Error("short colour accepted")
	}
}
