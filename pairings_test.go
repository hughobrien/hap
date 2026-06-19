package hap

import (
	"github.com/brutella/hap/accessory"
	"github.com/brutella/hap/tlv8"

	"bytes"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPairingsHandlerRequests is a regression test for the recurring
// "pairings.go: tlv8: EOF" reports (#21, #44).
//
// A ListPairings request carries only Method (tag 0) and State (tag 6) — it has
// no Identifier (tag 1). The handler decoded into a struct that marked
// Identifier as required, so tlv8.UnmarshalReader returned io.EOF and the
// handler rejected the request with HTTP 400. iOS uses ListPairings to
// reconcile a home's controllers (e.g. resident HomePods/Apple TVs), so the
// persistent error left it retrying forever and unable to converge.
//
// The bodies below are captured verbatim from a real iOS controller.
func TestPairingsHandlerRequests(t *testing.T) {
	a := accessory.New(accessory.Info{Name: "Test"}, accessory.TypeOutlet)
	srv, err := NewServer(NewMemStore(), a)
	if err != nil {
		t.Fatal(err)
	}

	const addr = "192.0.2.10:50000"
	srv.mux.Lock()
	srv.sess[addr] = &session{Pairing: Pairing{Name: "admin", Permission: PermissionAdmin}}
	srv.mux.Unlock()

	cases := []struct {
		name string
		body string // hex, captured off the wire
	}{
		{"ListPairings", "000105060101"},
		{"AddPairing", "000103060101012437463141374344452d454645442d343943342d394431462d39373245364645384544413203208d8b2b72ff96811a7e4ab6ef5e3719bcacc5582b293d5d2ea3fc74dbf13a68620b0101"},
	}

	for _, tc := range cases {
		raw, err := hex.DecodeString(tc.body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/pairings", bytes.NewReader(raw))
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()

		srv.pairings(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("%s: handler returned HTTP %d, want 200 (request rejected — tlv8 unmarshal EOF on a valid request)", tc.name, rec.Code)
		}
	}
}

// TestRemovePairingNonExistentReturnsSuccess covers the RemovePairing handler
// when the target pairing does not exist. Per Apple's HomeKitADK
// (HAPPairingPairingsRemovePairingGetM2), the accessory MUST return success in
// that case — removal is idempotent. The handler previously returned
// TlvErrorUnknown, which makes iOS retry the cleanup indefinitely.
func TestRemovePairingNonExistentReturnsSuccess(t *testing.T) {
	a := accessory.New(accessory.Info{Name: "Test"}, accessory.TypeOutlet)
	srv, err := NewServer(NewMemStore(), a)
	if err != nil {
		t.Fatal(err)
	}

	const addr = "192.0.2.10:50000"
	srv.mux.Lock()
	srv.sess[addr] = &session{Pairing: Pairing{Name: "admin", Permission: PermissionAdmin}}
	srv.mux.Unlock()

	body, err := tlv8.Marshal(struct {
		Method     byte   `tlv8:"0"`
		Identifier string `tlv8:"1"`
		State      byte   `tlv8:"6"`
	}{Method: MethodDeletePairing, Identifier: "DOES-NOT-EXIST", State: M1})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/pairings", bytes.NewReader(body))
	req.RemoteAddr = addr
	rec := httptest.NewRecorder()

	srv.pairings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d, want 200", rec.Code)
	}

	var resp struct {
		State byte `tlv8:"6"`
		Error byte `tlv8:"7,optional"`
	}
	if err := tlv8.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != 0 {
		t.Fatalf("response error = %d, want 0 (success): removing a non-existent pairing must succeed", resp.Error)
	}
	if resp.State != M2 {
		t.Fatalf("response state = %d, want M2 (%d)", resp.State, M2)
	}
}
