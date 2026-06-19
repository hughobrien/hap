package hap

import (
	"github.com/brutella/hap/accessory"

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
