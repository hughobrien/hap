package tlv8

import (
	"bytes"
	"reflect"
	"testing"
)

func TestFloat32RoundTrip(t *testing.T) {
	type payload struct {
		F float32 `tlv8:"1"`
	}
	in := payload{F: 153.5}
	b, err := Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out payload
	if err := Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.F != in.F {
		t.Fatalf("got %v want %v (encoded bytes %x)", out.F, in.F, b)
	}
}

func TestWriteBytes(t *testing.T) {
	wr := newWriter()

	buf := make([]byte, 256)
	wr.writeBytes(1, buf)

	expected := make([]byte, 260)
	expected[0] = 0x1
	expected[1] = 0xFF
	expected[257] = 0x1
	expected[258] = 0x1

	if is, want := wr.bytes(), expected; !reflect.DeepEqual(is, want) {
		t.Fatalf("%v != %v", is, want)
	}

	rd, err := newReader(bytes.NewBuffer(wr.bytes()))
	if err != nil {
		t.Fatal(err)
	}

	read, err := rd.readBytes(0x1)
	if err != nil {
		t.Fatal(err)
	}
	if is, want := read, buf; !reflect.DeepEqual(is, want) {
		t.Fatalf("%v len(%d) != %v len(%d)", is, len(is), want, len(want))
	}
}
