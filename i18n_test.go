package golagram

import (
	"context"
	"testing"
)

func demoTranslator() *I18n {
	t := NewI18n("en")
	t.Add("en", map[string]string{
		"greet":        "Hello, %s!",
		"plain":        "Just text",
		"only_english": "English only",
		"apples.one":   "You have %d apple",
		"apples.other": "You have %d apples",
	})
	t.Add("ru", map[string]string{
		"greet":       "Привет, %s!",
		"apples.one":  "У вас %d яблоко",
		"apples.few":  "У вас %d яблока",
		"apples.many": "У вас %d яблок",
	})
	t.Add("uz", map[string]string{
		"greet": "Salom, %s!",
	})
	return t
}

func TestI18n_TranslateFallbackChain(t *testing.T) {
	tr := demoTranslator()
	cases := []struct {
		locale, key, want string
		args              []any
	}{
		{"ru", "greet", "Привет, Aziz!", []any{"Aziz"}},
		{"en", "greet", "Hello, Aziz!", []any{"Aziz"}},
		{"ru-RU", "greet", "Привет, Aziz!", []any{"Aziz"}}, // base-language fallback
		{"uz_Cyrl", "greet", "Salom, Aziz!", []any{"Aziz"}},
		{"ru", "only_english", "English only", nil},    // per-key fallback to default locale
		{"fr", "greet", "Hello, Aziz!", []any{"Aziz"}}, // unknown locale -> default
		{"", "plain", "Just text", nil},
		{"en", "no.such.key", "no.such.key", nil}, // missing key stays visible
	}
	for _, tc := range cases {
		if got := tr.Translate(tc.locale, tc.key, tc.args...); got != tc.want {
			t.Errorf("Translate(%q, %q) = %q, want %q", tc.locale, tc.key, got, tc.want)
		}
	}
}

func TestI18n_Plurals(t *testing.T) {
	tr := demoTranslator()
	cases := []struct {
		locale string
		n      int
		want   string
	}{
		{"en", 1, "You have 1 apple"},
		{"en", 2, "You have 2 apples"},
		{"en", 0, "You have 0 apples"},
		// Russian one/few/many, including the 11-14 exceptions
		{"ru", 1, "У вас 1 яблоко"},
		{"ru", 21, "У вас 21 яблоко"},
		{"ru", 2, "У вас 2 яблока"},
		{"ru", 24, "У вас 24 яблока"},
		{"ru", 5, "У вас 5 яблок"},
		{"ru", 11, "У вас 11 яблок"},
		{"ru", 12, "У вас 12 яблок"},
		{"ru", 14, "У вас 14 яблок"},
		{"ru", 111, "У вас 111 яблок"},
		{"ru", 101, "У вас 101 яблоко"},
		{"ru", 0, "У вас 0 яблок"},
		// uz has no apples.* entries -> per-key fallback to English forms
		{"uz", 1, "You have 1 apple"},
		{"uz", 3, "You have 3 apples"},
	}
	for _, tc := range cases {
		if got := tr.TranslateN(tc.locale, "apples", tc.n, tc.n); got != tc.want {
			t.Errorf("TranslateN(%q, apples, %d) = %q, want %q", tc.locale, tc.n, got, tc.want)
		}
	}
}

func TestI18n_TranslateN_FallsBackToPlainKey(t *testing.T) {
	tr := NewI18n("en").Add("en", map[string]string{"items": "items: %d"})
	if got := tr.TranslateN("en", "items", 5, 5); got != "items: 5" {
		t.Errorf("got %q, want plain-key fallback", got)
	}
	if got := tr.TranslateN("en", "missing", 5); got != "missing" {
		t.Errorf("got %q, want the key itself", got)
	}
}

func TestI18n_SetPluralRule(t *testing.T) {
	tr := NewI18n("en").
		Add("fr", map[string]string{"n.one": "un", "n.other": "plusieurs"}).
		SetPluralRule("fr", func(n int) PluralCategory {
			if n == 0 || n == 1 {
				return PluralOne
			}
			return PluralOther
		})
	if got := tr.TranslateN("fr", "n", 0); got != "un" {
		t.Errorf("custom rule ignored: got %q", got)
	}
}

func TestI18n_AddJSON(t *testing.T) {
	tr := NewI18n("en")
	if err := tr.AddJSON("en", []byte(`{"hi": "Hi, %s!"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := tr.Translate("en", "hi", "there"); got != "Hi, there!" {
		t.Errorf("got %q", got)
	}
	if err := tr.AddJSON("en", []byte(`not json`)); err == nil {
		t.Error("expected an error for invalid JSON")
	}
}

// i18nCtx builds a dispatch-shaped Ctx over shared FSM storage, the way
// middleware sees one.
func i18nCtx(storage FSMStorage, langCode string) *Ctx {
	u := &Update{Message: &Message{
		MessageID: 1,
		Chat:      &Chat{ID: 10, Type: "private"},
		From:      &User{ID: 20, LanguageCode: langCode},
	}}
	return newCtx(context.Background(), u, nil, nil, storage, "test_bot")
}

func TestI18nMiddleware_ResolvesFromLanguageCode(t *testing.T) {
	tr := demoTranslator()
	storage := NewMemoryStorage()
	var gotGreet, gotLocale string
	handler := I18nMiddleware(tr)(func(c *Ctx) error {
		gotGreet = c.T("greet", "Aziz")
		gotLocale = c.Locale()
		return nil
	})

	if err := handler(i18nCtx(storage, "ru")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotGreet != "Привет, Aziz!" || gotLocale != "ru" {
		t.Errorf("greet/locale = %q/%q, want Russian via LanguageCode", gotGreet, gotLocale)
	}

	// No language code at all -> translator default.
	if err := handler(i18nCtx(storage, "")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotLocale != "en" {
		t.Errorf("locale = %q, want default en", gotLocale)
	}
}

func TestI18nMiddleware_SetLocaleOverridesAndPersists(t *testing.T) {
	tr := demoTranslator()
	storage := NewMemoryStorage()
	mw := I18nMiddleware(tr)

	// First update: Telegram says "ru", user switches to "uz".
	first := mw(func(c *Ctx) error {
		if err := c.SetLocale("uz"); err != nil {
			return err
		}
		// The override applies immediately, mid-handler.
		if got := c.T("greet", "Aziz"); got != "Salom, Aziz!" {
			t.Errorf("post-SetLocale greet = %q, want Uzbek", got)
		}
		return nil
	})
	if err := first(i18nCtx(storage, "ru")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Next update from the same chat/user: the saved override outranks
	// the "ru" Telegram still reports.
	var gotGreet, gotLocale string
	second := mw(func(c *Ctx) error {
		gotGreet = c.T("greet", "Aziz")
		gotLocale = c.Locale()
		return nil
	})
	if err := second(i18nCtx(storage, "ru")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotGreet != "Salom, Aziz!" || gotLocale != "uz" {
		t.Errorf("greet/locale = %q/%q, want persisted Uzbek override", gotGreet, gotLocale)
	}

	// A different user is unaffected by the override.
	var otherLocale string
	third := mw(func(c *Ctx) error {
		otherLocale = c.Locale()
		return nil
	})
	other := &Update{Message: &Message{
		MessageID: 2,
		Chat:      &Chat{ID: 11, Type: "private"},
		From:      &User{ID: 21, LanguageCode: "ru"},
	}}
	if err := third(newCtx(context.Background(), other, nil, nil, storage, "test_bot")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if otherLocale != "ru" {
		t.Errorf("other user's locale = %q, want ru", otherLocale)
	}
}

// TestI18nOuter_TranslatedFilterMatches is the ordering bug I18nOuter
// fixes: a filter that matches a translated reply-keyboard label needs
// c.T to resolve *before* filters run. With plain I18nMiddleware (r.Use,
// inner) this filter would never see a translator and would always
// compare against the untranslated key.
func TestI18nOuter_TranslatedFilterMatches(t *testing.T) {
	tr := demoTranslator()
	storage := NewMemoryStorage()

	r := NewRouter()
	r.UseOuter(I18nOuter(tr))

	isMenuButton := func(c *Ctx) bool {
		return c.Text() == c.T("greet", "Aziz")
	}
	var ran bool
	r.Message(isMenuButton).Handle(func(c *Ctx) error {
		ran = true
		return nil
	})

	u := &Update{Message: &Message{
		MessageID: 1,
		Text:      "Привет, Aziz!",
		Chat:      &Chat{ID: 10, Type: "private"},
		From:      &User{ID: 20, LanguageCode: "ru"},
	}}
	matched, err := r.dispatch(newCtx(context.Background(), u, nil, nil, storage, "test_bot"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched || !ran {
		t.Errorf("expected filter to see the resolved translator before matching, matched=%v ran=%v", matched, ran)
	}
}

func TestCtx_T_WithoutMiddleware(t *testing.T) {
	c := i18nCtx(NewMemoryStorage(), "en")
	if got := c.T("greet", "x"); got != "greet" {
		t.Errorf("T without middleware = %q, want the key itself", got)
	}
	if got := c.TN("apples", 3); got != "apples" {
		t.Errorf("TN without middleware = %q, want the key itself", got)
	}
	if got := c.Locale(); got != "" {
		t.Errorf("Locale without middleware = %q, want empty", got)
	}
}

func TestBaseLang(t *testing.T) {
	for in, want := range map[string]string{
		"en": "en", "en-US": "en", "uz_Cyrl": "uz", "pt-BR": "pt", "": "",
	} {
		if got := baseLang(in); got != want {
			t.Errorf("baseLang(%q) = %q, want %q", in, got, want)
		}
	}
}
