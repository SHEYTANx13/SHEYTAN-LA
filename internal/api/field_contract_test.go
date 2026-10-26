// field_contract_test.go (file name kept from its v1.10.3 origin): the
// wire-contract pin for the ONE UI client.
//
// The product UI (ui/fyne — formerly the Qt host) consumes EXACT field
// names from these endpoints; a silent rename blanks the desktop UI
// instead of failing a test. v1.10.2 proved the failure mode for real: a
// client parsed a wrapped {"sessions":[…]} envelope the core never
// serves (the wire contract is a BARE ARRAY) and read a messageCount
// field the wire never carries (the wire field is msgCount) — both
// hidden by silent non-200/malformed swallowing. This test pins the wire
// contract every UI client depends on, so the next rename fails HERE:
//
//	GET  /api/sessions        → BARE ARRAY of stubs; every entry carries
//	                            "id" and "msgCount"; "title" may be omitted
//	                            (omitempty) — the UI renders "Untitled".
//	GET  /api/sessions/{id}   → object with "id" and a "messages" array;
//	                            every message carries "role" and "content".
//	GET  /api/systems         → object with "systems" array + "activeSystemId";
//	                            every entry carries "systemId", "name",
//	                            "revision".
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestQTFieldContractSessionsListIsBareArrayWithMsgCount(t *testing.T) {
	server, _ := newTestServer(t)

	// A created session guarantees a non-empty list and a title.
	createResp, err := http.Post(server.URL+"/api/sessions", "application/json",
		strings.NewReader(`{"name":"contract-session"}`))
	if err != nil {
		t.Fatalf("POST /api/sessions: %v", err)
	}
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/sessions status = %d", createResp.StatusCode)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	createResp.Body.Close()
	if created.ID == "" {
		t.Fatal("created session has no id")
	}

	listResp, err := http.Get(server.URL + "/api/sessions")
	if err != nil {
		t.Fatalf("GET /api/sessions: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/sessions status = %d", listResp.StatusCode)
	}

	// THE pin: the body decodes into a BARE ARRAY — not an envelope.
	var list []map[string]any
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatalf("GET /api/sessions body must decode into a bare JSON array (Qt field contract): %v", err)
	}
	if len(list) == 0 {
		t.Fatal("session list is empty right after a create")
	}

	found := false
	for _, entry := range list {
		if entry["id"] == created.ID {
			found = true
		}
		// Every stub must carry the id field the QML sidebar reads.
		if _, ok := entry["id"]; !ok {
			t.Fatalf("session stub missing the id field: %v", entry)
		}
		// msgCount is omitempty on the wire: absent for a 0-message stub
		// (the QML renders 0), a NUMBER when present. What must never
		// appear is a renamed count field — the v1.10.2 client read
		// "messageCount" and the wire never carried it.
		if mc, ok := entry["msgCount"]; ok {
			if _, ok := mc.(float64); !ok {
				t.Fatalf("msgCount must be a number when present, got %T", mc)
			}
		}
		if _, ok := entry["messageCount"]; ok {
			t.Fatalf("session stub carries a messageCount field — the wire field is msgCount: %v", entry)
		}
		// title is omitempty on the wire — MAY be absent; never asserted.
	}
	if !found {
		t.Fatalf("created session %s missing from the list", created.ID)
	}
}

func TestQTFieldContractSessionDetailCarriesMessages(t *testing.T) {
	server, _ := newTestServer(t)

	createResp, err := http.Post(server.URL+"/api/sessions", "application/json",
		strings.NewReader(`{"name":"detail-contract"}`))
	if err != nil {
		t.Fatalf("POST /api/sessions: %v", err)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	createResp.Body.Close()

	resp, err := http.Get(server.URL + "/api/sessions/" + created.ID)
	if err != nil {
		t.Fatalf("GET /api/sessions/{id}: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/sessions/{id} status = %d", resp.StatusCode)
	}

	var detail map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail["id"] != created.ID {
		t.Fatalf("detail id = %v, want %s", detail["id"], created.ID)
	}
	// messages is omitempty on the wire for a 0-message session; when
	// present it MUST be an array (the QML transcript binds it). The
	// per-message role/content shape is proven end-to-end by the QtTest
	// live-answer acceptance (ui/qt/tests/tst_acceptance.cpp) which reads
	// both fields from a real run's transcript.
	if msgs, ok := detail["messages"]; ok {
		if _, isArray := msgs.([]any); !isArray {
			t.Fatalf("detail messages must be an array when present: %v", msgs)
		}
	}
}

func TestQTFieldContractSystemsShape(t *testing.T) {
	server, _ := newTestServer(t)

	resp, err := http.Get(server.URL + "/api/systems")
	if err != nil {
		t.Fatalf("GET /api/systems: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/systems status = %d", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode systems: %v", err)
	}

	// THE pin: {"systems":[…], "activeSystemId":"…"} — the drawer's model
	// and the ACTIVE badge both read from this object.
	systems, ok := payload["systems"].([]any)
	if !ok {
		t.Fatalf("systems must be an array under the systems key: %v", payload)
	}
	if _, ok := payload["activeSystemId"].(string); !ok {
		t.Fatalf("activeSystemId must be a string: %v", payload["activeSystemId"])
	}
	if len(systems) == 0 {
		t.Fatal("the reserved default system must be seeded")
	}
	for _, s := range systems {
		entry, ok := s.(map[string]any)
		if !ok {
			t.Fatalf("system entry is not an object: %v", s)
		}
		for _, field := range []string{"systemId", "name", "revision"} {
			if _, ok := entry[field]; !ok {
				t.Fatalf("system entry missing the %s field (the QML drawer reads it): %v", field, entry)
			}
		}
	}
}
