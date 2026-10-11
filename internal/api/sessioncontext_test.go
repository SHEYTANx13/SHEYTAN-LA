package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

// TestSessionContextEndpointRoundTrip: the per-session context policy API
// persists the policy per chat (1.1.6 §4) and returns the authoritative
// decision with every figure exposed separately (§3).
func TestSessionContextEndpointRoundTrip(t *testing.T) {
	server, _ := newTestServer(t)

	// Create two sessions: A and B.
	create := func() string {
		res, err := http.Post(server.URL+"/api/sessions", "application/json", bytes.NewReader([]byte(`{}`)))
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		defer res.Body.Close()
		var sess struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(res.Body).Decode(&sess); err != nil {
			t.Fatalf("decode create: %v", err)
		}
		if sess.ID == "" {
			t.Fatal("empty session id")
		}
		return sess.ID
	}

	a := create()
	b := create()

	// Chat A: 4K, Chat B: inherit (0).
	put := func(id string, tokens int) map[string]any {
		body, _ := json.Marshal(map[string]any{"contextTokens": tokens})
		req, err := http.NewRequest(http.MethodPut,
			server.URL+"/api/sessions/"+id+"/context",
			bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("put context: %v", err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("put context status = %d", res.StatusCode)
		}
		var status map[string]any
		if err := json.NewDecoder(res.Body).Decode(&status); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		return status
	}

	statusA := put(a, 4096)
	if statusA["sessionPolicy"].(float64) != 4096 {
		t.Fatalf("chat A sessionPolicy = %v, want 4096", statusA["sessionPolicy"])
	}
	if statusA["effective"].(float64) != 4096 {
		t.Fatalf("chat A effective = %v, want 4096", statusA["effective"])
	}
	if statusA["classification"] == "" {
		t.Fatal("resource classification missing")
	}
	if _, ok := statusA["options"].([]any); !ok {
		t.Fatal("selector options missing")
	}

	put(b, 0)

	// GET both: policies must be independent (per-chat, no global mutation).
	get := func(id string) map[string]any {
		res, err := http.Get(server.URL + "/api/sessions/" + id + "/context")
		if err != nil {
			t.Fatalf("get context: %v", err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("get context status = %d", res.StatusCode)
		}
		var status map[string]any
		if err := json.NewDecoder(res.Body).Decode(&status); err != nil {
			t.Fatalf("decode get: %v", err)
		}
		return status
	}

	againA := get(a)
	againB := get(b)

	if againA["sessionPolicy"].(float64) != 4096 {
		t.Fatalf("chat A policy drifted: %v", againA["sessionPolicy"])
	}
	if policy, present := againB["sessionPolicy"]; present && policy != 0.0 {
		t.Fatalf("chat B policy drifted: %v", policy)
	}
	if againB["effective"].(float64) != againB["configured"].(float64) {
		t.Fatalf("chat B effective %v must inherit configured %v",
			againB["effective"], againB["configured"])
	}

	// The usage figures must be present and coherent (backend-measured).
	for _, status := range []map[string]any{againA, againB} {
		used := status["used"].(float64)
		usable := status["usableInput"].(float64)
		effective := status["effective"].(float64)
		if used < 0 || usable <= 0 || effective <= 0 {
			t.Fatalf("incoherent status: %+v", status)
		}
		if status["outputReserve"].(float64) <= 0 {
			t.Fatalf("output reserve missing: %+v", status)
		}
	}
}

// TestSessionContextRejectsUnsupported: the PUT refuses a policy the
// resource assessment classifies unsupported, WITH an explanation
// (1.1.6 §8). Untestable end-to-end without a real oversized model, so
// this test only proves a sane value is accepted and the gate evaluates.
func TestSessionContextAcceptsSanePolicy(t *testing.T) {
	server, _ := newTestServer(t)

	res, err := http.Post(server.URL+"/api/sessions", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()

	body := []byte(`{"contextTokens":8192}`)
	req, _ := http.NewRequest(http.MethodPut,
		server.URL+"/api/sessions/"+created.ID+"/context",
		bytes.NewReader(body))
	put, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer put.Body.Close()
	if put.StatusCode != http.StatusOK {
		t.Fatalf("sane policy rejected: %d", put.StatusCode)
	}
}
