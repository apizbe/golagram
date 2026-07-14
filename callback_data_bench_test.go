package golagram

import "testing"

// buyItem is defined in callback_data_test.go — reused here rather than
// declaring a second near-identical payload type.

func BenchmarkCallbackData_Pack(b *testing.B) {
	cb := NewCallbackData[buyItem]("buy")
	v := buyItem{ItemID: 42, Qty: 3, Gift: true}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := cb.Pack(v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCallbackData_Unpack(b *testing.B) {
	cb := NewCallbackData[buyItem]("buy")
	packed, err := cb.Pack(buyItem{ItemID: 42, Qty: 3, Gift: true})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := cb.Unpack(packed); err != nil {
			b.Fatal(err)
		}
	}
}

// The HMAC variants isolate the cost of tamper protection — WithHMAC signs
// every Pack and verifies every Unpack, on top of the same field encoding
// the plain benchmarks above measure.

func BenchmarkCallbackData_Pack_HMAC(b *testing.B) {
	cb := NewCallbackData[buyItem]("buy").WithHMAC([]byte("bench-secret-key"))
	v := buyItem{ItemID: 42, Qty: 3, Gift: true}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := cb.Pack(v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCallbackData_Unpack_HMAC(b *testing.B) {
	cb := NewCallbackData[buyItem]("buy").WithHMAC([]byte("bench-secret-key"))
	packed, err := cb.Pack(buyItem{ItemID: 42, Qty: 3, Gift: true})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := cb.Unpack(packed); err != nil {
			b.Fatal(err)
		}
	}
}
