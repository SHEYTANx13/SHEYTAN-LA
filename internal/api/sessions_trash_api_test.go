package api

// sessions_trash_api_test.go — v1.11.1 content-integrity milestone: the
// recoverable-deletion surface over HTTP. The API contract under test:
//
//   - DELETE /api/sessions/{id} is recoverable-by-default and says so;
//   - GET    /api/sessions/trash lists tombstones (metadata only);
//   - POST   /api/sessions/{id}/restore undoes a delete (409 on conflict);
//   - POST   /api/sessions/{id}/purge permanently destroys an
//     already-deleted session and is REFUSED for a live session — the
//     two-step destructive policy is enforced by the server, not the UI.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestSessionTrashLifecycleOverAPI(t *testing.T) {
	server, _ := newTestServer(t)

	// Create and persist a session (a rename forces the durable write —
	// a fresh "new chat" is an in-memory pending draft until then).
	resp, err := http.Post(server.URL+"/api/sessions", "application/json", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var sess struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	resp.Body.Close()
	if sess.ID == "" {
		t.Fatal("no session id")
	}
	renameBody, _ := json.Marshal(map[string]any{"title": "trash api test"})
	reqRename, _ := http.NewRequest(http.MethodPut, server.URL+"/api/sessions/"+sess.ID, bytes.NewReader(renameBody))
	reqRename.Header.Set("Content-Type", "application/json")
	if _, err := http.DefaultClient.Do(reqRename); err != nil {
		t.Fatalf("rename (durable write): %v", err)
	}

	// A PENDING draft delete is honest about having nothing to recover:
	// create a second session, delete it immediately, and the response
	// must say recoverable=false with an empty trash behind it.
	respPending, err := http.Post(server.URL+"/api/sessions", "application/json", nil)
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}
	var pending struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(respPending.Body).Decode(&pending)
	respPending.Body.Close()
	reqPendingDel, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/sessions/"+pending.ID, nil)
	respPendingDel, err := http.DefaultClient.Do(reqPendingDel)
	if err != nil {
		t.Fatalf("delete pending: %v", err)
	}
	var pendingDel struct {
		OK          bool `json:"ok"`
		Recoverable bool `json:"recoverable"`
	}
	_ = json.NewDecoder(respPendingDel.Body).Decode(&pendingDel)
	respPendingDel.Body.Close()
	if !pendingDel.OK || pendingDel.Recoverable {
		t.Fatalf("pending-draft delete body = %+v, want ok=true recoverable=false (no bytes ever persisted)", pendingDel)
	}

	// DELETE of the persisted session is recoverable and says so.
	reqDel, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/sessions/"+sess.ID, nil)
	respDel, err := http.DefaultClient.Do(reqDel)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	var delBody struct {
		OK          bool   `json:"ok"`
		Recoverable bool   `json:"recoverable"`
		Restore     string `json:"restore"`
	}
	if err := json.NewDecoder(respDel.Body).Decode(&delBody); err != nil {
		t.Fatalf("decode delete body: %v", err)
	}
	respDel.Body.Close()
	if !delBody.OK || !delBody.Recoverable || delBody.Restore == "" {
		t.Fatalf("delete body = %+v, want ok+recoverable+restore path", delBody)
	}

	// The session is gone from the active store...
	detail, err := http.Get(server.URL + "/api/sessions/" + sess.ID)
	if err != nil {
		t.Fatalf("get deleted: %v", err)
	}
	detail.Body.Close()
	if detail.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted session status = %d, want 404", detail.StatusCode)
	}

	// ...but listed in the trash with its tombstone.
	trashResp, err := http.Get(server.URL + "/api/sessions/trash")
	if err != nil {
		t.Fatalf("get trash: %v", err)
	}
	var tombs []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Actor string `json:"actor"`
	}
	if err := json.NewDecoder(trashResp.Body).Decode(&tombs); err != nil {
		t.Fatalf("decode trash: %v", err)
	}
	trashResp.Body.Close()
	if len(tombs) != 1 || tombs[0].ID != sess.ID || tombs[0].Actor != "user" {
		t.Fatalf("trash = %+v, want exactly one user-deleted tombstone for %s", tombs, sess.ID)
	}

	// RESTORE undoes the delete and returns the live session.
	respRestore, err := http.Post(server.URL+"/api/sessions/"+sess.ID+"/restore", "application/json", nil)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	var restored struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(respRestore.Body).Decode(&restored); err != nil {
		t.Fatalf("decode restore: %v", err)
	}
	respRestore.Body.Close()
	if restored.ID != sess.ID {
		t.Fatalf("restored id = %q, want %q", restored.ID, sess.ID)
	}

	// A second restore is an honest 409 conflict, never a fake success.
	respRestore2, err := http.Post(server.URL+"/api/sessions/"+sess.ID+"/restore", "application/json", nil)
	if err != nil {
		t.Fatalf("restore 2: %v", err)
	}
	io.Copy(io.Discard, respRestore2.Body)
	respRestore2.Body.Close()
	if respRestore2.StatusCode != http.StatusConflict {
		t.Fatalf("second restore status = %d, want 409", respRestore2.StatusCode)
	}

	// PURGE of a LIVE session is refused (409): permanent destruction is
	// only ever the second step, after a delete.
	respPurgeLive, err := http.Post(server.URL+"/api/sessions/"+sess.ID+"/purge", "application/json", nil)
	if err != nil {
		t.Fatalf("purge live: %v", err)
	}
	io.Copy(io.Discard, respPurgeLive.Body)
	respPurgeLive.Body.Close()
	if respPurgeLive.StatusCode != http.StatusConflict {
		t.Fatalf("purge of a live session status = %d, want 409 (two-step policy)", respPurgeLive.StatusCode)
	}
	// The refused purge destroyed nothing: the session is still there.
	still, err := http.Get(server.URL + "/api/sessions/" + sess.ID)
	if err != nil || still.StatusCode != http.StatusOK {
		t.Fatalf("live session lost after refused purge (err=%v status=%d)", err, still.StatusCode)
	}
	still.Body.Close()

	// delete → purge destroys permanently; the trash is empty afterwards
	// and a further restore is an honest 409.
	reqDel2, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/sessions/"+sess.ID, nil)
	if _, err := http.DefaultClient.Do(reqDel2); err != nil {
		t.Fatalf("delete 2: %v", err)
	}
	respPurge, err := http.Post(server.URL+"/api/sessions/"+sess.ID+"/purge", "application/json", nil)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	io.Copy(io.Discard, respPurge.Body)
	respPurge.Body.Close()
	if respPurge.StatusCode != http.StatusOK {
		t.Fatalf("purge status = %d, want 200", respPurge.StatusCode)
	}

	trashResp2, err := http.Get(server.URL + "/api/sessions/trash")
	if err != nil {
		t.Fatalf("get trash 2: %v", err)
	}
	var tombs2 []json.RawMessage
	if err := json.NewDecoder(trashResp2.Body).Decode(&tombs2); err != nil {
		t.Fatalf("decode trash 2: %v", err)
	}
	trashResp2.Body.Close()
	if len(tombs2) != 0 {
		t.Fatalf("trash after purge = %d entries, want 0", len(tombs2))
	}

	respRestore3, err := http.Post(server.URL+"/api/sessions/"+sess.ID+"/restore", "application/json", nil)
	if err != nil {
		t.Fatalf("restore after purge: %v", err)
	}
	io.Copy(io.Discard, respRestore3.Body)
	respRestore3.Body.Close()
	if respRestore3.StatusCode != http.StatusConflict {
		t.Fatalf("restore after purge status = %d, want 409", respRestore3.StatusCode)
	}
}
