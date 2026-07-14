package golagram

import (
	"errors"
	"strings"
	"testing"
)

type buyItem struct {
	ItemID int64
	Qty    int
	Gift   bool
}

func TestCallbackData_PackUnpackRoundTrip(t *testing.T) {
	cb := NewCallbackData[buyItem]("buy")

	packed, err := cb.Pack(buyItem{ItemID: 42, Qty: 3, Gift: true})
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if packed != "buy:42:3:true" {
		t.Errorf("packed = %q, want buy:42:3:true", packed)
	}

	v, err := cb.Unpack(packed)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if v != (buyItem{ItemID: 42, Qty: 3, Gift: true}) {
		t.Errorf("round trip lost data: %+v", v)
	}
}

func TestCallbackData_PackEnforces64ByteLimit(t *testing.T) {
	type payload struct{ S string }
	cb := NewCallbackData[payload]("p")

	if _, err := cb.Pack(payload{S: strings.Repeat("x", 100)}); err == nil {
		t.Fatal("expected an over-limit pack to fail")
	} else {
		var vErr *ValidationError
		if !errors.As(err, &vErr) {
			t.Errorf("expected *ValidationError, got %T", err)
		}
	}

	if _, err := cb.Pack(payload{S: "has:separator"}); err == nil {
		t.Error("expected a separator in a string field to fail")
	}
}

func TestCallbackData_UnpackRejectsForeignData(t *testing.T) {
	cb := NewCallbackData[buyItem]("buy")

	if _, err := cb.Unpack("sell:42:3:true"); err == nil {
		t.Error("expected a different prefix to fail")
	}
	if _, err := cb.Unpack("buy:42"); err == nil {
		t.Error("expected a field-count mismatch to fail")
	}
	if _, err := cb.Unpack("buy:notanumber:3:true"); err == nil {
		t.Error("expected a malformed int to fail")
	}
}

func TestCallbackData_FilterAndFromCtx(t *testing.T) {
	cb := NewCallbackData[buyItem]("buy")

	match := cbCtx(&CallbackQuery{Data: cb.MustPack(buyItem{ItemID: 7, Qty: 1})})
	if !cb.Filter()(match) {
		t.Error("Filter should match this schema's data")
	}
	if cb.Filter()(cbCtx(&CallbackQuery{Data: "buyer:7"})) {
		t.Error("Filter should not match a different prefix that shares a string prefix")
	}

	v, err := cb.FromCtx(match)
	if err != nil {
		t.Fatalf("FromCtx: %v", err)
	}
	if v.ItemID != 7 || v.Qty != 1 {
		t.Errorf("FromCtx lost data: %+v", v)
	}

	bulk := cb.FilterWhere(func(b buyItem) bool { return b.Qty > 1 })
	if bulk(match) {
		t.Error("FilterWhere predicate should reject Qty=1")
	}
	if !bulk(cbCtx(&CallbackQuery{Data: cb.MustPack(buyItem{ItemID: 7, Qty: 5})})) {
		t.Error("FilterWhere predicate should accept Qty=5")
	}
}

func TestCallbackData_Button(t *testing.T) {
	cb := NewCallbackData[buyItem]("buy")
	btn := cb.Button("Buy now", buyItem{ItemID: 42, Qty: 1})
	if btn.Text != "Buy now" || btn.CallbackData != "buy:42:1:false" {
		t.Errorf("unexpected button: %+v", btn)
	}
}

func TestCallbackData_HMAC_PackUnpackRoundTrip(t *testing.T) {
	cb := NewCallbackData[buyItem]("buy").WithHMAC([]byte("test-secret-key"))

	packed, err := cb.Pack(buyItem{ItemID: 42, Qty: 3, Gift: true})
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if !strings.HasPrefix(packed, "buy:42:3:true:") {
		t.Errorf("packed = %q, want a buy:42:3:true: prefix followed by a tag", packed)
	}
	if len(packed) <= len("buy:42:3:true") {
		t.Errorf("packed = %q, expected a tag appended beyond the plain payload", packed)
	}

	v, err := cb.Unpack(packed)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if v != (buyItem{ItemID: 42, Qty: 3, Gift: true}) {
		t.Errorf("round trip lost data: %+v", v)
	}
}

func TestCallbackData_HMAC_RejectsTamperedPayload(t *testing.T) {
	cb := NewCallbackData[buyItem]("buy").WithHMAC([]byte("test-secret-key"))
	packed := cb.MustPack(buyItem{ItemID: 42, Qty: 3, Gift: true})

	// Flip the packed quantity but keep the original (now-invalid) tag.
	idx := strings.LastIndex(packed, ":")
	tampered := "buy:42:9:true" + packed[idx:]

	if _, err := cb.Unpack(tampered); !errors.Is(err, ErrCallbackDataTampered) {
		t.Errorf("expected ErrCallbackDataTampered for a tampered payload, got %v", err)
	}
}

func TestCallbackData_HMAC_RejectsWrongSecret(t *testing.T) {
	packed := NewCallbackData[buyItem]("buy").WithHMAC([]byte("secret-a")).MustPack(buyItem{ItemID: 1, Qty: 1})

	cbB := NewCallbackData[buyItem]("buy").WithHMAC([]byte("secret-b"))
	if _, err := cbB.Unpack(packed); !errors.Is(err, ErrCallbackDataTampered) {
		t.Errorf("expected ErrCallbackDataTampered for a mismatched secret, got %v", err)
	}
}

func TestCallbackData_HMAC_RejectsMissingOrMalformedTag(t *testing.T) {
	cb := NewCallbackData[buyItem]("buy").WithHMAC([]byte("test-secret-key"))

	if _, err := cb.Unpack("buy:42:3:true"); !errors.Is(err, ErrCallbackDataTampered) {
		t.Errorf("expected ErrCallbackDataTampered for a missing tag, got %v", err)
	}
	if _, err := cb.Unpack("buy:42:3:true:not-valid-base64!!"); !errors.Is(err, ErrCallbackDataTampered) {
		t.Errorf("expected ErrCallbackDataTampered for a malformed tag, got %v", err)
	}
}

func TestCallbackData_HMAC_UnsignedSchemaIgnoresTamperedTagFormat(t *testing.T) {
	// An unsigned schema never treats the trailing segment as a tag — it's
	// just another field, so a normal field-count/type error applies
	// instead of tamper detection.
	cb := NewCallbackData[buyItem]("buy")
	if _, err := cb.Unpack("buy:42:3:true:extra"); errors.Is(err, ErrCallbackDataTampered) {
		t.Error("unsigned schema should never return ErrCallbackDataTampered")
	}
}

func TestCallbackData_HMAC_FilterWhereRejectsTamperedData(t *testing.T) {
	cb := NewCallbackData[buyItem]("buy").WithHMAC([]byte("test-secret-key"))
	packed := cb.MustPack(buyItem{ItemID: 1, Qty: 1})
	idx := strings.LastIndex(packed, ":")
	tampered := "buy:1:9:false" + packed[idx:]

	alwaysTrue := cb.FilterWhere(func(buyItem) bool { return true })
	if alwaysTrue(cbCtx(&CallbackQuery{Data: tampered})) {
		t.Error("FilterWhere should reject tampered data before the predicate ever runs")
	}
}

func TestCallbackData_WithHMAC_PanicsOnEmptySecret(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected a panic on an empty secret")
		}
	}()
	NewCallbackData[buyItem]("buy").WithHMAC(nil)
}

func TestNewCallbackData_PanicsOnBadSchema(t *testing.T) {
	assertPanics := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: expected a panic at wiring time", name)
			}
		}()
		f()
	}

	assertPanics("separator in prefix", func() { NewCallbackData[buyItem]("bu:y") })
	assertPanics("non-struct payload", func() { NewCallbackData[int]("n") })
	type badField struct{ M map[string]int }
	assertPanics("unsupported field type", func() { NewCallbackData[badField]("b") })
}
