package golagram

import (
	"math"
	"testing"
)

// fuzzCBPayload exercises every field kind CallbackData supports (buyItem in
// callback_data_test.go only covers int64/int/bool) so the fuzz targets
// below drive string, float64, and uint parsing too.
type fuzzCBPayload struct {
	Name  string
	Qty   int
	Price float64
	Code  uint
	Gift  bool
}

// payloadsEqual is == for fuzzCBPayload except on Price: strconv.ParseFloat
// happily parses "NaN", and IEEE 754 says NaN != NaN even reflexively, so a
// plain != would report a spurious round-trip mismatch for a value that
// round-tripped perfectly correctly.
func payloadsEqual(a, b fuzzCBPayload) bool {
	if a.Name != b.Name || a.Qty != b.Qty || a.Code != b.Code || a.Gift != b.Gift {
		return false
	}
	if math.IsNaN(a.Price) || math.IsNaN(b.Price) {
		return math.IsNaN(a.Price) && math.IsNaN(b.Price)
	}
	return a.Price == b.Price
}

func FuzzCallbackDataUnpack(f *testing.F) {
	cd := NewCallbackData[fuzzCBPayload]("fz")

	for _, seed := range []string{
		"fz:hello:1:2.5:3:true",
		"fz::0:0:0:false",
		"fz:a:b:c:d:e",
		"fz",
		"",
		"other:1:2:3:4",
		"fz:has:a:colon:in:it:too:many:fields",
		"fz:-9223372036854775808:1e400:18446744073709551615:notabool",
	} {
		f.Add(seed)
	}
	if packed, err := cd.Pack(fuzzCBPayload{Name: "widget", Qty: 3, Price: 9.99, Code: 42, Gift: true}); err == nil {
		f.Add(packed)
	}

	f.Fuzz(func(t *testing.T, data string) {
		v, err := cd.Unpack(data)
		if err != nil {
			return
		}
		// A successful decode must be stable under re-encoding: packing it
		// back and unpacking that must reproduce exactly the same value,
		// even if the original fuzzed string used different formatting
		// (e.g. "+3" or leading zeros) that Pack would never itself emit.
		repacked, err := cd.Pack(v)
		if err != nil {
			t.Fatalf("Unpack(%q) = %+v, nil but re-Pack failed: %v", data, v, err)
		}
		v2, err := cd.Unpack(repacked)
		if err != nil {
			t.Fatalf("Unpack(%q) round-trip: Unpack(re-Pack(v)) failed: %v", data, err)
		}
		if !payloadsEqual(v, v2) {
			t.Fatalf("Unpack(%q) round-trip mismatch: v=%+v, v2=%+v", data, v, v2)
		}
	})
}

func FuzzCallbackDataUnpackHMAC(f *testing.F) {
	cd := NewCallbackData[fuzzCBPayload]("fz").WithHMAC([]byte("fuzz-test-secret"))

	for _, seed := range []string{
		"fz:hello:1:2.5:3:true:abcdefghijk",
		"",
		"fz:a:b:c:d:e:not-base64-!!!",
		"fz:a:b:c:d:e",
	} {
		f.Add(seed)
	}
	if packed, err := cd.Pack(fuzzCBPayload{Name: "widget", Qty: 3, Price: 9.99, Code: 42, Gift: true}); err == nil {
		f.Add(packed)
	}

	f.Fuzz(func(t *testing.T, data string) {
		v, err := cd.Unpack(data)
		if err != nil {
			return
		}
		repacked, err := cd.Pack(v)
		if err != nil {
			t.Fatalf("Unpack(%q) = %+v, nil but re-Pack failed: %v", data, v, err)
		}
		v2, err := cd.Unpack(repacked)
		if err != nil {
			t.Fatalf("Unpack(%q) round-trip: Unpack(re-Pack(v)) failed: %v", data, err)
		}
		if !payloadsEqual(v, v2) {
			t.Fatalf("Unpack(%q) round-trip mismatch: v=%+v, v2=%+v", data, v, v2)
		}
	})
}
