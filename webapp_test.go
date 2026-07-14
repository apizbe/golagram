package golagram

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Fixed vectors generated with an independent reference implementation
// (Python hmac/hashlib) of the documented algorithms — not with this
// package's own code, so these tests pin the algorithm, not just the
// wiring.
const (
	vectorToken = "7000000001:AAFakeTokenForHMACVectors_0123456789ab"

	vectorInitData = "query_id=AAHdF6IQAAAAAN0XohDhrOrc&user=%7B%22id%22%3A123456789%2C%22first_name%22%3A%22Aziz%22%2C%22last_name%22%3A%22Q%22%2C%22username%22%3A%22azizbek%22%2C%22language_code%22%3A%22en%22%2C%22is_premium%22%3Atrue%2C%22allows_write_to_pm%22%3Atrue%7D&auth_date=1751700000&start_param=ref_abc-123&chat_type=private&chat_instance=-3788475317572404878&hash=cb58a7dd22314c9b294bc7c481d37628df950219cbec60c658baef52eb319363"

	vectorLoginHash = "e1c6723b3368df1d46bca681ecdadd685ef0bfc234808e9a2f8673d4279b839a"
)

func vectorLoginValues() url.Values {
	return url.Values{
		"id":         {"123456789"},
		"first_name": {"Aziz"},
		"username":   {"azizbek"},
		"photo_url":  {"https://t.me/i/userpic/320/azizbek.jpg"},
		"auth_date":  {"1751700000"},
		"hash":       {vectorLoginHash},
	}
}

func TestValidateWebAppInitData_Vector(t *testing.T) {
	d, err := ValidateWebAppInitData(vectorInitData, vectorToken, 0)
	if err != nil {
		t.Fatalf("valid init data rejected: %v", err)
	}
	if d.QueryID != "AAHdF6IQAAAAAN0XohDhrOrc" {
		t.Errorf("QueryID = %q", d.QueryID)
	}
	if d.User == nil || d.User.ID != 123456789 || d.User.FirstName != "Aziz" || !d.User.IsPremium || !d.User.AllowsWriteToPM {
		t.Errorf("User = %+v", d.User)
	}
	if d.StartParam != "ref_abc-123" || d.ChatType != "private" {
		t.Errorf("StartParam/ChatType = %q/%q", d.StartParam, d.ChatType)
	}
	if d.AuthDate.Unix() != 1751700000 {
		t.Errorf("AuthDate = %v", d.AuthDate)
	}
}

func TestValidateWebAppInitData_Rejections(t *testing.T) {
	cases := []struct {
		name     string
		initData string
		token    string
	}{
		{"tampered field", strings.Replace(vectorInitData, "ref_abc-123", "ref_abc-124", 1), vectorToken},
		{"wrong token", vectorInitData, vectorToken + "x"},
		{"missing hash", strings.Split(vectorInitData, "&hash=")[0], vectorToken},
		{"smuggled extra field", vectorInitData + "&admin=1", vectorToken},
		{"empty", "", vectorToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ValidateWebAppInitData(tc.initData, tc.token, 0); err == nil {
				t.Error("expected rejection")
			}
		})
	}
}

func TestValidateWebAppInitData_MaxAge(t *testing.T) {
	// The vector was signed 2025-07-05 — expired against any sane maxAge.
	if _, err := ValidateWebAppInitData(vectorInitData, vectorToken, time.Hour); err == nil {
		t.Error("year-old init data passed a 1h maxAge")
	}
	// A freshly-signed payload passes: sign one now with the documented
	// algorithm (freshness logic is under test here; the algorithm itself
	// is pinned by the Python vector above).
	fresh := signInitData(t, url.Values{
		"query_id":  {"Q1"},
		"auth_date": {fmt.Sprint(time.Now().Unix())},
	}, vectorToken)
	if _, err := ValidateWebAppInitData(fresh, vectorToken, time.Hour); err != nil {
		t.Errorf("fresh init data rejected: %v", err)
	}
	// maxAge with no auth_date at all must reject.
	noDate := signInitData(t, url.Values{"query_id": {"Q2"}}, vectorToken)
	if _, err := ValidateWebAppInitData(noDate, vectorToken, time.Hour); err == nil {
		t.Error("init data without auth_date passed a freshness check")
	}
}

// signInitData builds initData signed per the Mini Apps spec.
func signInitData(t *testing.T, values url.Values, token string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte("WebAppData"))
	mac.Write([]byte(token))
	secret := mac.Sum(nil)
	mac = hmac.New(sha256.New, secret)
	mac.Write([]byte(dataCheckString(values)))
	values.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return values.Encode()
}

func TestWebAppEd25519Keys_AreValidAndDistinct(t *testing.T) {
	if len(webAppEd25519ProdKey) != ed25519.PublicKeySize {
		t.Errorf("prod key length = %d, want %d", len(webAppEd25519ProdKey), ed25519.PublicKeySize)
	}
	if len(webAppEd25519TestKey) != ed25519.PublicKeySize {
		t.Errorf("test key length = %d, want %d", len(webAppEd25519TestKey), ed25519.PublicKeySize)
	}
	if webAppEd25519ProdKey.Equal(webAppEd25519TestKey) {
		t.Error("prod and test keys must differ")
	}
}

// signInitDataEd25519 builds initData signed the way
// ValidateWebAppInitDataThirdParty verifies it — used with a locally
// generated keypair (swapped into the package's key var for the test's
// duration) since only Telegram holds the private half of the real
// embedded public keys.
func signInitDataEd25519(t *testing.T, values url.Values, botID int64, priv ed25519.PrivateKey) string {
	t.Helper()
	message := fmt.Sprint(botID) + ":WebAppData\n" + dataCheckString(values, "hash", "signature")
	sig := ed25519.Sign(priv, []byte(message))
	values.Set("signature", base64.RawURLEncoding.EncodeToString(sig))
	return values.Encode()
}

// withTestEd25519Key swaps webAppEd25519ProdKey for pub for the calling
// test's duration, restoring it on cleanup.
func withTestEd25519Key(t *testing.T, pub ed25519.PublicKey) {
	t.Helper()
	orig := webAppEd25519ProdKey
	webAppEd25519ProdKey = pub
	t.Cleanup(func() { webAppEd25519ProdKey = orig })
}

func TestValidateWebAppInitDataThirdParty_Vector(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	withTestEd25519Key(t, pub)

	const botID = int64(7000000001)
	initData := signInitDataEd25519(t, url.Values{
		"query_id":    {"AAG"},
		"user":        {`{"id":123456789,"first_name":"Aziz","is_premium":true}`},
		"auth_date":   {"1751700000"},
		"start_param": {"ref_abc-123"},
	}, botID, priv)

	d, err := ValidateWebAppInitDataThirdParty(initData, botID, WebAppProd, 0)
	if err != nil {
		t.Fatalf("valid init data rejected: %v", err)
	}
	if d.User == nil || d.User.ID != 123456789 || d.User.FirstName != "Aziz" || !d.User.IsPremium {
		t.Errorf("User = %+v", d.User)
	}
	if d.StartParam != "ref_abc-123" {
		t.Errorf("StartParam = %q", d.StartParam)
	}
	if d.AuthDate.Unix() != 1751700000 {
		t.Errorf("AuthDate = %v", d.AuthDate)
	}
}

func TestValidateWebAppInitDataThirdParty_Rejections(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	withTestEd25519Key(t, pub)

	const botID = int64(7000000001)
	base := func() url.Values {
		return url.Values{"query_id": {"AAG"}, "auth_date": {"1751700000"}}
	}

	valid := signInitDataEd25519(t, base(), botID, priv)
	tampered := signInitDataEd25519(t, base(), botID, priv)
	tampered = strings.Replace(tampered, "query_id=AAG", "query_id=AAH", 1)

	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrongKey := signInitDataEd25519(t, base(), botID, otherPriv)

	wrongBotID := signInitDataEd25519(t, base(), botID+1, priv)
	noSig := strings.Split(valid, "&signature=")[0]
	badBase64 := strings.Replace(valid, "signature=", "signature=not-valid-base64!!!&x=", 1)

	cases := []struct {
		name     string
		initData string
		botID    int64
		env      WebAppEnvironment
	}{
		{"tampered field", tampered, botID, WebAppProd},
		{"signed with wrong key", wrongKey, botID, WebAppProd},
		{"wrong botID", wrongBotID, botID, WebAppProd},
		{"wrong environment (test key expected)", valid, botID, WebAppTest},
		{"no signature field", noSig, botID, WebAppProd},
		{"malformed base64 signature", badBase64, botID, WebAppProd},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ValidateWebAppInitDataThirdParty(tc.initData, tc.botID, tc.env, 0); err == nil {
				t.Error("expected rejection")
			}
		})
	}

	// Sanity: the untampered vector against the right key/botID/env passes.
	if _, err := ValidateWebAppInitDataThirdParty(valid, botID, WebAppProd, 0); err != nil {
		t.Errorf("valid init data rejected: %v", err)
	}
}

func TestValidateWebAppInitDataThirdParty_MaxAge(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	withTestEd25519Key(t, pub)
	const botID = int64(7000000001)

	stale := signInitDataEd25519(t, url.Values{"query_id": {"Q1"}, "auth_date": {"1751700000"}}, botID, priv)
	if _, err := ValidateWebAppInitDataThirdParty(stale, botID, WebAppProd, time.Hour); err == nil {
		t.Error("year-old init data passed a 1h maxAge")
	}

	fresh := signInitDataEd25519(t, url.Values{"query_id": {"Q2"}, "auth_date": {fmt.Sprint(time.Now().Unix())}}, botID, priv)
	if _, err := ValidateWebAppInitDataThirdParty(fresh, botID, WebAppProd, time.Hour); err != nil {
		t.Errorf("fresh init data rejected: %v", err)
	}

	noDate := signInitDataEd25519(t, url.Values{"query_id": {"Q3"}}, botID, priv)
	if _, err := ValidateWebAppInitDataThirdParty(noDate, botID, WebAppProd, time.Hour); err == nil {
		t.Error("init data without auth_date passed a freshness check")
	}
}

func TestValidateLoginWidgetData_Vector(t *testing.T) {
	d, err := ValidateLoginWidgetData(vectorLoginValues(), vectorToken, 0)
	if err != nil {
		t.Fatalf("valid login data rejected: %v", err)
	}
	if d.ID != 123456789 || d.FirstName != "Aziz" || d.Username != "azizbek" {
		t.Errorf("parsed = %+v", d)
	}
	if d.AuthDate.Unix() != 1751700000 {
		t.Errorf("AuthDate = %v", d.AuthDate)
	}
}

func TestValidateLoginWidgetData_Rejections(t *testing.T) {
	tampered := vectorLoginValues()
	tampered.Set("id", "123456780")

	wrongKeyDerivation := vectorLoginValues()
	// Sign with the WebApp derivation instead of SHA256(token): must fail,
	// proving the two schemes aren't interchangeable.
	mac := hmac.New(sha256.New, []byte("WebAppData"))
	mac.Write([]byte(vectorToken))
	secret := mac.Sum(nil)
	mac = hmac.New(sha256.New, secret)
	mac.Write([]byte(dataCheckString(wrongKeyDerivation)))
	wrongKeyDerivation.Set("hash", hex.EncodeToString(mac.Sum(nil)))

	noHash := vectorLoginValues()
	noHash.Del("hash")

	cases := []struct {
		name   string
		values url.Values
		token  string
	}{
		{"tampered id", tampered, vectorToken},
		{"wrong token", vectorLoginValues(), vectorToken + "x"},
		{"webapp key derivation", wrongKeyDerivation, vectorToken},
		{"missing hash", noHash, vectorToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ValidateLoginWidgetData(tc.values, tc.token, 0); err == nil {
				t.Error("expected rejection")
			}
		})
	}
}

func TestValidateLoginWidgetData_MaxAge(t *testing.T) {
	if _, err := ValidateLoginWidgetData(vectorLoginValues(), vectorToken, time.Hour); err == nil {
		t.Error("year-old login data passed a 1h maxAge")
	}
	if _, err := ValidateLoginWidgetData(vectorLoginValues(), vectorToken, 0); err != nil {
		t.Errorf("maxAge 0 must skip the freshness check: %v", err)
	}
}
