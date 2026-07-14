package golagram

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Translator is what [I18nMiddleware] carries and [Ctx.T] / [Ctx.TN] call.
// [I18n] is the built-in map-based implementation; a gettext/go-i18n-backed
// one can be plugged in from outside, as a separate module, without this
// package depending on it.
type Translator interface {
	// Translate renders the message for key in the given locale,
	// formatting the template with args (fmt.Sprintf) when any are passed.
	Translate(locale, key string, args ...any) string
	// TranslateN renders the plural form of key selected by n. n picks
	// the form only — templates still format from args, so pass n again
	// there if the text shows it: TranslateN("en", "apples", 3, 3).
	TranslateN(locale, key string, n int, args ...any) string
	// DefaultLocale is the locale used when an update carries none.
	DefaultLocale() string
}

// PluralCategory is a CLDR plural class a PluralRule maps a number to.
type PluralCategory string

const (
	PluralOne   PluralCategory = "one"
	PluralFew   PluralCategory = "few"
	PluralMany  PluralCategory = "many"
	PluralOther PluralCategory = "other"
)

// PluralRule picks the plural category for a count (integer counts only —
// fractional CLDR rules are out of scope for the built-in translator).
type PluralRule func(n int) PluralCategory

// I18n is the built-in Translator: locale → key → template maps with
// per-key fallback (exact locale → base language → default locale → the
// key itself, so a missing translation stays visible instead of blank)
// and CLDR-style plural selection.
//
// Configure it fully before the bot starts — [I18n.Add] / [I18n.SetPluralRule]
// are not safe to call concurrently with dispatch.
type I18n struct {
	defaultLocale string
	messages      map[string]map[string]string
	pluralRules   map[string]PluralRule
}

// NewI18n creates a translator that falls back to defaultLocale for
// updates whose locale is unknown or untranslated.
func NewI18n(defaultLocale string) *I18n {
	return &I18n{
		defaultLocale: defaultLocale,
		messages:      make(map[string]map[string]string),
		pluralRules:   make(map[string]PluralRule),
	}
}

// Add registers (or extends) a locale's messages. Plural variants use key
// suffixes: "apples.one", "apples.few", "apples.many", "apples.other".
func (t *I18n) Add(locale string, messages map[string]string) *I18n {
	m := t.messages[locale]
	if m == nil {
		m = make(map[string]string, len(messages))
		t.messages[locale] = m
	}
	for k, v := range messages {
		m[k] = v
	}
	return t
}

// AddJSON is Add for a JSON object ({"key": "template", ...}) — the shape
// a per-locale file naturally holds.
func (t *I18n) AddJSON(locale string, data []byte) error {
	var messages map[string]string
	if err := json.Unmarshal(data, &messages); err != nil {
		return fmt.Errorf("i18n: locale %q: %w", locale, err)
	}
	t.Add(locale, messages)
	return nil
}

// SetPluralRule overrides the plural rule for a base language. Built-in
// rules cover the Germanic/Turkic one/other split (the default: n==1 →
// one) and the Slavic one/few/many system (ru, uk, be, sr, hr, bs) —
// anything else needs a rule set here.
func (t *I18n) SetPluralRule(locale string, rule PluralRule) *I18n {
	t.pluralRules[baseLang(locale)] = rule
	return t
}

// DefaultLocale implements Translator.
func (t *I18n) DefaultLocale() string { return t.defaultLocale }

// Translate implements Translator.
func (t *I18n) Translate(locale, key string, args ...any) string {
	template, ok := t.lookup(locale, key)
	if !ok {
		return key
	}
	if len(args) == 0 {
		return template
	}
	return fmt.Sprintf(template, args...)
}

// TranslateN implements Translator: looks up "key.<category>" per the
// locale's plural rule, falling back to "key.other", then plain key.
func (t *I18n) TranslateN(locale, key string, n int, args ...any) string {
	category := t.pluralRule(locale)(n)
	for _, k := range []string{key + "." + string(category), key + "." + string(PluralOther), key} {
		if template, ok := t.lookup(locale, k); ok {
			if len(args) == 0 {
				return template
			}
			return fmt.Sprintf(template, args...)
		}
	}
	return key
}

// lookup resolves key per-key through the fallback chain: exact locale →
// base language ("en-US" → "en") → default locale.
func (t *I18n) lookup(locale, key string) (string, bool) {
	for _, loc := range []string{locale, baseLang(locale), t.defaultLocale} {
		if loc == "" {
			continue
		}
		if template, ok := t.messages[loc][key]; ok {
			return template, true
		}
	}
	return "", false
}

func (t *I18n) pluralRule(locale string) PluralRule {
	base := baseLang(locale)
	if rule, ok := t.pluralRules[base]; ok {
		return rule
	}
	switch base {
	case "ru", "uk", "be", "sr", "hr", "bs":
		return slavicPlural
	default:
		return defaultPlural
	}
}

// baseLang strips a region/script subtag: "en-US" and "uz_Cyrl" → "en"/"uz".
func baseLang(locale string) string {
	if i := strings.IndexAny(locale, "-_"); i >= 0 {
		return locale[:i]
	}
	return locale
}

// defaultPlural is the one/other split most non-Slavic languages the Bot
// API meets use (English, Uzbek, Turkish, German, ...).
func defaultPlural(n int) PluralCategory {
	if n == 1 || n == -1 {
		return PluralOne
	}
	return PluralOther
}

// slavicPlural is the CLDR one/few/many rule for Russian-family languages:
// 1, 21, 31 → one; 2-4, 22-24 → few; 0, 5-20, 11-14, 25-30 → many.
func slavicPlural(n int) PluralCategory {
	if n < 0 {
		n = -n
	}
	switch mod10, mod100 := n%10, n%100; {
	case mod10 == 1 && mod100 != 11:
		return PluralOne
	case mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14):
		return PluralFew
	default:
		return PluralMany
	}
}

// Ctx value keys and the reserved FSM data key the i18n middleware uses.
const (
	ctxKeyTranslator = "golagram:i18n:translator"
	ctxKeyLocale     = "golagram:i18n:locale"
	localeFSMKey     = "_golagram.locale"
)

// I18nMiddleware attaches a [Translator] to every dispatched update and
// resolves its locale: an FSM override saved by [Ctx.SetLocale] wins, then
// the sender's User.LanguageCode, then the translator's default.
//
// [Router.Use] middleware is inner — it only wraps the handler of whichever
// registration already matched, so it runs *after* filters. That means a
// filter that itself wants translated matching (a reply-keyboard button
// whose label came from c.T — the standard menu pattern) always sees the
// untranslated key, because I18nMiddleware hasn't attached the translator
// yet at filter time. If any filter in the tree needs [Ctx.T]/[Ctx.Locale],
// register [I18nOuter] via [Router.UseOuter] on the root router instead —
// same resolution, but it runs before filters:
//
//	r.UseOuter(gg.I18nOuter(translator))
//
// Use plain I18nMiddleware only when nothing at filter time needs the
// locale (translated text lives solely in handler bodies):
//
//	r.Use(gg.I18nMiddleware(translator))
//
// The FSM override lives in conversation data under a reserved key, so it
// follows the bot's [FSMKeyStrategy] and survives restarts on persistent
// storage.
func I18nMiddleware(t Translator) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Ctx) error {
			attachI18n(c, t)
			return next(c)
		}
	}
}

// I18nOuter is [I18nMiddleware]'s outer-middleware form: identical locale
// resolution, but registered via [Router.UseOuter] so it runs before any
// filter in the router (and its included sub-routers) evaluates — the fix
// for the ordering problem described on [I18nMiddleware].
//
//	r.UseOuter(gg.I18nOuter(translator))
func I18nOuter(t Translator) OuterMiddlewareFunc {
	return func(next func(*Ctx) (bool, error)) func(*Ctx) (bool, error) {
		return func(c *Ctx) (bool, error) {
			attachI18n(c, t)
			return next(c)
		}
	}
}

// attachI18n resolves c's locale against t and stores both on c, the shared
// step behind [I18nMiddleware] and [I18nOuter].
func attachI18n(c *Ctx, t Translator) {
	locale := ""
	if saved, ok, err := FSMGet[string](c.FSM(), localeFSMKey); err == nil && ok {
		locale = saved
	}
	if locale == "" {
		if u := c.From(); u != nil {
			locale = u.LanguageCode
		}
	}
	if locale == "" {
		locale = t.DefaultLocale()
	}
	c.Set(ctxKeyTranslator, t)
	c.Set(ctxKeyLocale, locale)
}

// T translates key for this update's resolved locale — the gettext-style
// shorthand. Without [I18nMiddleware] installed it returns the key itself,
// so a missing middleware shows up as visible untranslated keys, not a
// crash.
func (c *Ctx) T(key string, args ...any) string {
	t, locale := c.i18n()
	if t == nil {
		return key
	}
	return t.Translate(locale, key, args...)
}

// TN translates the plural form of key selected by n (see
// [Translator.TranslateN] — pass n in args too if the template renders it).
func (c *Ctx) TN(key string, n int, args ...any) string {
	t, locale := c.i18n()
	if t == nil {
		return key
	}
	return t.TranslateN(locale, key, n, args...)
}

// Locale returns the locale [I18nMiddleware] resolved for this update
// ("" if the middleware isn't installed).
func (c *Ctx) Locale() string {
	_, locale := c.i18n()
	return locale
}

// SetLocale saves a locale override in conversation data — from a /lang
// command or settings button — and applies it to this Ctx immediately, so
// even the confirmation message can already be translated.
func (c *Ctx) SetLocale(locale string) error {
	if err := FSMSet(c.FSM(), localeFSMKey, locale); err != nil {
		return err
	}
	c.Set(ctxKeyLocale, locale)
	return nil
}

func (c *Ctx) i18n() (Translator, string) {
	v, ok := c.Get(ctxKeyTranslator)
	if !ok {
		return nil, ""
	}
	t, _ := v.(Translator)
	locale, _ := c.Get(ctxKeyLocale)
	s, _ := locale.(string)
	return t, s
}
