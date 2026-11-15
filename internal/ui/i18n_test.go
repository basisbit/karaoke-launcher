package ui

import (
	"slices"
	"testing"
)

// TestTranslationFiles checks every language against English: no unknown keys (typos), the same placeholders.
// Missing keys are allowed (English is shown) but listed.
func TestTranslationFiles(t *testing.T) {
	en := translations["en"]
	if len(en) < 50 {
		t.Fatalf("en.json has only %d texts", len(en))
	}
	for _, code := range Languages() {
		texts := translations[code]
		if texts["language.name"] == "" {
			t.Errorf("%s: no language.name", code)
		}
		for k, v := range texts {
			ev, ok := en[k]
			if !ok {
				t.Errorf("%s: key %q is not in en.json", code, k)
				continue
			}
			a, b := placeholderRe.FindAllString(v, -1), placeholderRe.FindAllString(ev, -1)
			slices.Sort(a)
			slices.Sort(b)
			if !slices.Equal(a, b) {
				t.Errorf("%s: %q has placeholders %v, English has %v", code, k, a, b)
			}
		}
		for k := range en {
			if _, ok := texts[k]; !ok {
				t.Logf("%s: %q missing, English is shown", code, k)
			}
		}
	}
	for _, want := range []string{"en", "de", "fr", "es", "it", "pt", "nl", "pl", "ru", "zh", "ja", "ko", "hi"} {
		if !slices.Contains(Languages(), want) {
			t.Errorf("language %s missing", want)
		}
	}
}

func TestTranslator(t *testing.T) {
	de := NewTranslator("de")
	if de.Code() != "de" || de.T("home.play") != "Spielen" {
		t.Fatalf("de: %s %q", de.Code(), de.T("home.play"))
	}
	if got := de.T("songs.needs", "size", "2,3 GB", "free", "18 GB"); got != "Noch herunterzuladen: 2,3 GB (18 GB frei)" {
		t.Fatalf("placeholders: %q", got)
	}
	for in, want := range map[string]string{"de-AT": "de", "pt_BR": "pt", "FR": "fr", "xx": "en", "tlh-KL": "en"} {
		if got := NewTranslator(in).Code(); got != want {
			t.Errorf("%s -> %s, want %s", in, got, want)
		}
	}
	if got := NewTranslator("xx").T("home.play"); got != "Play" {
		t.Fatalf("fallback: %q", got)
	}
	// a key missing in one language falls back to English
	translations["zz"] = map[string]string{"language.name": "Test"}
	defer delete(translations, "zz")
	if got := NewTranslator("zz").T("home.play"); got != "Play" {
		t.Fatalf("missing key: %q", got)
	}
	if got := de.T("no.such.key"); got != "no.such.key" {
		t.Fatalf("unknown key: %q", got)
	}
	if NewTranslator("").Code() == "" {
		t.Fatal("system language gave no code")
	}
	if LanguageName("de") != "Deutsch" {
		t.Fatal(LanguageName("de"))
	}
}
