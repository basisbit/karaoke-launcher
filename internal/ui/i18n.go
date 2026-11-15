package ui

import (
	"embed"
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strings"

	"fyne.io/fyne/v2/lang"
)

// Translations: one file per language in translations/<code>.json, a flat object "key": "text". Placeholders are
// written {name}. To add a language, copy en.json, translate the texts and keep the keys and placeholders.
// A text that is missing in a language is shown in English.
//
//go:embed translations/*.json
var translationFiles embed.FS

var placeholderRe = regexp.MustCompile(`\{[a-z]+\}`)

const fallback = "en"

var translations = loadTranslations()

func loadTranslations() map[string]map[string]string {
	all := map[string]map[string]string{}
	files, _ := translationFiles.ReadDir("translations")
	for _, f := range files {
		data, err := translationFiles.ReadFile("translations/" + f.Name())
		if err != nil {
			panic(err)
		}
		texts := map[string]string{}
		if err := json.Unmarshal(data, &texts); err != nil {
			panic("translations/" + f.Name() + ": " + err.Error()) // caught by the tests
		}
		all[strings.TrimSuffix(f.Name(), path.Ext(f.Name()))] = texts
	}
	return all
}

// Languages returns the codes of all languages, sorted.
func Languages() []string {
	var codes []string
	for c := range translations {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	return codes
}

// LanguageName returns the name of a language in that language ("Deutsch").
func LanguageName(code string) string { return translations[code]["language.name"] }

// Translator looks up the texts of one language.
type Translator struct {
	code  string
	texts map[string]string
}

// NewTranslator returns the translator for a language code ("de", "pt-BR"). "" means the system language.
// Languages without a translation get English.
func NewTranslator(code string) *Translator {
	if code == "" {
		code = lang.SystemLocale().LanguageString()
	}
	code = strings.ToLower(code)
	base, _, _ := strings.Cut(strings.ReplaceAll(code, "_", "-"), "-")
	for _, c := range []string{code, base} {
		if texts, ok := translations[c]; ok {
			return &Translator{code: c, texts: texts}
		}
	}
	return &Translator{code: fallback, texts: translations[fallback]}
}

// Code returns the language code in use.
func (t *Translator) Code() string { return t.code }

// T returns the text for key with the placeholders filled in: T("songs.needs", "size", "2 GB", "free", "18 GB").
func (t *Translator) T(key string, pairs ...string) string {
	s, ok := t.texts[key]
	if !ok {
		if s, ok = translations[fallback][key]; !ok {
			return key
		}
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		s = strings.ReplaceAll(s, "{"+pairs[i]+"}", pairs[i+1])
	}
	return s
}
