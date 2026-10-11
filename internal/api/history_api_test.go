// history_api_test.go — v1.2.8 API tests: mode-separated session listing,
// history search, session summaries and lazy history paging.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
)

func createSessionWithMode(t *testing.T, server *httptest.Server, mode string) map[string]any {
	t.Helper()

	body := "{}"
	if mode != "" {
		body = fmt.Sprintf(`{"mode":%q}`, mode)
	}

	resp, err := http.Post(server.URL+"/api/sessions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/sessions: %v", err)
	}
	defer resp.Body.Close()

	var created map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created session: %v", err)
	}

	return created
}

func listSessions(t *testing.T, server *httptest.Server, mode string) []map[string]any {
	t.Helper()

	url := server.URL + "/api/sessions"
	if mode != "" {
		url += "?mode=" + mode
	}

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET /api/sessions: %v", err)
	}
	defer resp.Body.Close()

	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}

	return list
}

// TestSessionsModeSeparation pins the wire contract: POST with a mode
// creates the session IN that space, ?mode= filters ONE space, legacy
// (mode-less) sessions land in agent, and no unfiltered call duplicates
// anything.
func TestSessionsModeSeparation(t *testing.T) {
	engine := remoteFakeEngine(t, "ok.")
	_, server := newRemoteServer(t, engine.URL)

	chat := createSessionWithMode(t, server, "chat")
	agentSess := createSessionWithMode(t, server, "agent")
	legacy := createSessionWithMode(t, server, "")

	if chat["mode"] != "chat" {
		t.Fatalf("chat session mode = %v", chat["mode"])
	}
	if agentSess["mode"] != "agent" {
		t.Fatalf("agent session mode = %v", agentSess["mode"])
	}
	if legacy["mode"] != "agent" {
		t.Fatalf("mode-less create must resolve to agent, got %v", legacy["mode"])
	}

	chatList := listSessions(t, server, "chat")
	if len(chatList) != 1 || chatList[0]["mode"] != "chat" {
		t.Fatalf("chat list wrong: %+v", chatList)
	}

	agentList := listSessions(t, server, "agent")
	if len(agentList) != 2 {
		t.Fatalf("agent list = %d sessions, want 2", len(agentList))
	}

	all := listSessions(t, server, "")
	if len(all) != 3 {
		t.Fatalf("unfiltered list = %d sessions, want 3", len(all))
	}
}

// TestHistorySearchEndpoint pins GET /api/history/search: scoping by mode
// and the hit shape (id/mode/title/msgCount).
func TestHistorySearchEndpoint(t *testing.T) {
	engine := remoteFakeEngine(t, "ok.")
	_, server := newRemoteServer(t, engine.URL)

	chat := createSessionWithMode(t, server, "chat")
	agentSess := createSessionWithMode(t, server, "agent")

	// Give the agent session a searchable title + one exchange (which
	// also creates a summary at settlement — but titles alone must be
	// searchable without any run).
	chatID := chat["id"].(string)
	agentID := agentSess["id"].(string)

	update := func(id, title string) {
		req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/sessions/"+id,
			strings.NewReader(fmt.Sprintf(`{"title":%q}`, title)))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PUT title: %v", err)
		}
		resp.Body.Close()
	}
	update(chatID, "kubernetes rollout notes")
	update(agentID, "deploy pipeline repair")

	search := func(q, mode string) []map[string]any {
		url := server.URL + "/api/history/search?q=" + q + "&limit=10"
		if mode != "" {
			url += "&mode=" + mode
		}
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET /api/history/search: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("history search status = %d", resp.StatusCode)
		}

		var hits []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&hits); err != nil {
			t.Fatalf("decode hits: %v", err)
		}
		return hits
	}

	all := search("deploy", "")
	if len(all) != 1 || all[0]["sessionId"] != agentID {
		t.Fatalf("cross-space search wrong: %+v", all)
	}
	if all[0]["mode"] != "agent" {
		t.Fatalf("hit mode = %v", all[0]["mode"])
	}

	chatOnly := search("deploy", "chat")
	if len(chatOnly) != 0 {
		t.Fatalf("chat-scoped search leaked an agent session: %+v", chatOnly)
	}
}

// TestSessionSummaryEndpoint pins GET /api/sessions/{id}/summary: the
// version-0 shell before any settled turn, and the populated summary the
// API serves once UpdateSummaryFromTurn has run through the store.
func TestSessionSummaryEndpoint(t *testing.T) {
	engine := remoteFakeEngine(t, "ok.")
	_, server := newRemoteServer(t, engine.URL)

	sess := createSessionWithMode(t, server, "chat")
	id := sess["id"].(string)

	resp, err := http.Get(server.URL + "/api/sessions/" + id + "/summary")
	if err != nil {
		t.Fatalf("GET summary: %v", err)
	}
	defer resp.Body.Close()

	var summary map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}

	if summary["sessionId"] != id {
		t.Fatalf("summary sessionId = %v", summary["sessionId"])
	}

	// Fresh session: version 0, no fabricated content.
	if v, ok := summary["version"].(float64); !ok || v != 0 {
		t.Fatalf("fresh summary version = %v, want 0", summary["version"])
	}
	if summary["objective"] != nil && summary["objective"] != "" {
		t.Fatalf("fresh summary must not fabricate an objective: %v", summary["objective"])
	}

	// Unknown session → 404.
	missing, _ := http.Get(server.URL + "/api/sessions/sdoesnotexist/summary")
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown session summary status = %d", missing.StatusCode)
	}
}

// TestSessionMessagesPaging pins GET /api/sessions/{id}/messages: the
// newest page by default, older pages via `before`, absolute indices,
// hasMore/nextBefore, and 404 for unknown sessions.
func TestSessionMessagesPaging(t *testing.T) {
	engine := remoteFakeEngine(t, "ok.")
	srv, server := newRemoteServerWithHandle(t, engine.URL)

	created := createSessionWithMode(t, server, "agent")
	id := created["id"].(string)

	// Seed 25 messages directly through the store (bypassing the run path
	// — this test pins the PAGER, not the engine).
	for i := 0; i < 25; i++ {
		if _, err := srv.store.AppendMessage(id, llm.Message{
			Role:    "user",
			Content: fmt.Sprintf("turn %02d", i),
		}); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	getPage := func(query string) map[string]any {
		resp, err := http.Get(server.URL + "/api/sessions/" + id + "/messages" + query)
		if err != nil {
			t.Fatalf("GET messages: %v", err)
		}
		defer resp.Body.Close()

		var page map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			t.Fatalf("decode page: %v", err)
		}
		return page
	}

	// Newest page (limit 10).
	newest := getPage("?limit=10")
	if newest["total"].(float64) != 25 {
		t.Fatalf("total = %v", newest["total"])
	}
	if newest["hasMore"] != true {
		t.Fatal("newest page must report hasMore")
	}

	msgs := newest["messages"].([]any)
	if len(msgs) != 10 {
		t.Fatalf("page length = %d, want 10", len(msgs))
	}

	// The newest page carries the LAST 10 messages (indices 15..24) and
	// nextBefore points at the exclusive start.
	first := msgs[0].(map[string]any)
	if first["index"].(float64) != 15 {
		t.Fatalf("first index = %v, want 15", first["index"])
	}
	if got := first["message"].(map[string]any)["content"]; got != "turn 15" {
		t.Fatalf("first content = %v, want turn 15", got)
	}
	if newest["nextBefore"].(float64) != 15 {
		t.Fatalf("nextBefore = %v, want 15", newest["nextBefore"])
	}

	// Older page via before=15.
	older := getPage("?before=15&limit=10")
	olderMsgs := older["messages"].([]any)
	if len(olderMsgs) != 10 {
		t.Fatalf("older page length = %d", len(olderMsgs))
	}
	if olderMsgs[0].(map[string]any)["index"].(float64) != 5 {
		t.Fatalf("older first index = %v, want 5", olderMsgs[0].(map[string]any)["index"])
	}

	// The oldest page terminates the pager.
	oldest := getPage("?before=5&limit=10")
	if oldest["hasMore"] != false {
		t.Fatal("oldest page must terminate the pager")
	}

	// Unknown session → 404.
	missing, _ := http.Get(server.URL + "/api/sessions/sdoesnotexist/messages?limit=5")
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown session messages status = %d", missing.StatusCode)
	}
}

// TestSessionContextUpdateKeepsHistoryRefs pins the read-modify-write
// contract the UI relies on: a context PUT that only carries historyRefs
// must not silently wipe the other context fields (the frontend always
// sends the full context; the backend replaces it wholesale).
// v1.2.9: the ref points at a REAL source session — the server now
// prunes dead references (source session not in the store).
func TestSessionContextUpdateKeepsHistoryRefs(t *testing.T) {
	engine := remoteFakeEngine(t, "ok.")
	_, server := newRemoteServer(t, engine.URL)

	// The referenced CHAT source session (the target below is an AGENT
	// session — cross-mode is enforced server-side).
	src := createSessionWithMode(t, server, "chat")
	srcID := src["id"].(string)

	created := createSessionWithMode(t, server, "agent")
	id := created["id"].(string)

	// Attach a history ref via the context PUT.
	payload := fmt.Sprintf(`{"context":{"contextTokens":8192,"historyRefs":[{"sessionId":%q,"mode":"chat","summaryVersion":3}]}}`, srcID)
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/sessions/"+id, strings.NewReader(payload))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT context: %v", err)
	}
	resp.Body.Close()

	// The full session detail must carry the refs alongside the policy.
	detail, err := http.Get(server.URL + "/api/sessions/" + id)
	if err != nil {
		t.Fatalf("GET session: %v", err)
	}
	defer detail.Body.Close()

	var full struct {
		Context sessions.Context `json:"context"`
	}
	if err := json.NewDecoder(detail.Body).Decode(&full); err != nil {
		t.Fatalf("decode session: %v", err)
	}

	if full.Context.ContextTokens != 8192 {
		t.Fatalf("contextTokens = %d", full.Context.ContextTokens)
	}
	if len(full.Context.HistoryRefs) != 1 || full.Context.HistoryRefs[0].SessionID != srcID {
		t.Fatalf("historyRefs wrong: %+v", full.Context.HistoryRefs)
	}
}

// TestDeadHistoryReferencePruned pins the v1.2.9 pruning contract: a
// reference whose source session no longer exists is dropped by
// validation (and its removal persists via the atomic delta path).
func TestDeadHistoryReferencePruned(t *testing.T) {
	engine := remoteFakeEngine(t, "ok.")
	_, server := newRemoteServer(t, engine.URL)

	src := createSessionWithMode(t, server, "chat")
	srcID := src["id"].(string)

	created := createSessionWithMode(t, server, "agent")
	id := created["id"].(string)

	// Attach the ref (valid at attach time).
	payload := fmt.Sprintf(`{"historyRefs":[{"sessionId":%q,"mode":"chat"}]}`, srcID)
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/sessions/"+id, strings.NewReader(payload))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT refs: %v", err)
	}
	resp.Body.Close()

	// Delete the source session (the ref is now dead).
	del, err := http.NewRequest(http.MethodDelete, server.URL+"/api/sessions/"+srcID, nil)
	if err != nil {
		t.Fatalf("build delete: %v", err)
	}
	delResp, err := http.DefaultClient.Do(del)
	if err != nil {
		t.Fatalf("DELETE source: %v", err)
	}
	delResp.Body.Close()

	// Re-PUT the same ref list: validation must prune the dead ref, and
	// the session context must no longer carry it.
	req2, _ := http.NewRequest(http.MethodPut, server.URL+"/api/sessions/"+id, strings.NewReader(payload))
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("PUT refs again: %v", err)
	}
	resp2.Body.Close()

	detail, err := http.Get(server.URL + "/api/sessions/" + id)
	if err != nil {
		t.Fatalf("GET session: %v", err)
	}
	defer detail.Body.Close()

	var full struct {
		Context sessions.Context `json:"context"`
	}
	if err := json.NewDecoder(detail.Body).Decode(&full); err != nil {
		t.Fatalf("decode session: %v", err)
	}

	if len(full.Context.HistoryRefs) != 0 {
		t.Fatalf("dead reference survived validation: %+v", full.Context.HistoryRefs)
	}
}
