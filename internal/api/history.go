// history.go — v1.2.8 history surfaces:
//
//	GET /api/history/search           → picker search over titles + summaries
//	GET /api/sessions/{id}/summary    → the rolling session summary
//	GET /api/sessions/{id}/messages   → lazy history paging (bounded pages)
//
// plus the settlement helpers that keep the durable layers fresh after a
// completed turn: the rolling session summary update and the agent.md
// handoff write. All of these read/write the EXISTING persistence
// architecture (sessions store + sidecars) — no second store, no parallel
// truth.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/agent"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/histref"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/llm"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/logging"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/sessions"
)

// handleHistorySearch serves GET /api/history/search?q=&mode=&limit=.
// Search covers session titles and rolling summaries — never whole
// transcripts. The mode filter ("chat" | "agent" | empty = all) is how one
// space explicitly browses the OTHER space's history (cross-mode picker).
func (s *Server) handleHistorySearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	limit := 8
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}

	hits := histref.Search(s.store, q, mode, limit)

	if hits == nil {
		hits = []histref.Hit{}
	}

	writeJSON(w, hits)
}

// handleSessionSummary serves GET /api/sessions/{id}/summary — the durable
// rolling summary of ONE session (Chat or Agent). A session without a
// settled turn yet returns the honest version-0 shell (never fabricated
// content).
func (s *Server) handleSessionSummary(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}

	sum, err := s.store.SummaryForRun(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}

	writeJSON(w, sum)
}

// messagesPage is the wire shape of one history page.
type messagesPage struct {
	SessionID  string           `json:"sessionId"`
	Total      int              `json:"total"`
	HasMore    bool             `json:"hasMore"`
	NextBefore int              `json:"nextBefore,omitempty"` // exclusive index of the oldest returned message
	Page       int              `json:"page"`
	Messages   []indexedMessage `json:"messages"`
}

// indexedMessage carries the message's ABSOLUTE index in the session
// transcript — the stable coordinate the pager and the history ranges
// speak in (messages themselves have no ids).
type indexedMessage struct {
	Index   int         `json:"index"`
	Message llm.Message `json:"message"`
}

// handleSessionMessages serves GET /api/sessions/{id}/messages — lazy
// history paging. Long histories must NOT require loading the entire
// transcript into the browser (or the model). The NEWEST page is served by
// default; `before` (exclusive message index) fetches older pages:
//
//	GET /api/sessions/{id}/messages                  → newest page
//	GET /api/sessions/{id}/messages?before=40        → messages [before-page, before)
//	GET /api/sessions/{id}/messages?before=40&limit=80
//
// Streaming and run settlement are unaffected — the authoritative run
// transport lives on the activity WebSocket, not here.
func (s *Server) handleSessionMessages(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}

	limit := 60
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}
	if limit > 200 {
		limit = 200
	}

	before := -1
	if v, err := strconv.Atoi(r.URL.Query().Get("before")); err == nil && v >= 0 {
		before = v
	}

	sess, err := s.store.Get(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}

	total := len(sess.Messages)

	end := total
	if before >= 0 && before < end {
		end = before
	}

	start := end - limit
	if start < 0 {
		start = 0
	}

	page := make([]indexedMessage, 0, end-start)
	for i := start; i < end; i++ {
		page = append(page, indexedMessage{Index: i, Message: sess.Messages[i]})
	}

	out := messagesPage{
		SessionID:  sess.ID,
		Total:      total,
		HasMore:    start > 0,
		NextBefore: start,
		Page:       len(page),
		Messages:   page,
	}

	if out.NextBefore <= 0 {
		out.NextBefore = 0
	}

	writeJSON(w, out)
}

// updateSessionSummaryRolling folds one settled turn into the session's
// durable summary. Deterministic, bounded, rolling — the transcript is
// never re-summarized from scratch per turn. Best-effort: a summary
// failure is logged, never fails the run (the transcript itself is the
// authoritative record).
//
// v1.2.8.1: the load-base → merge → save sequence is serialized per
// session — a cancelled run's tail settlement and the replacement run's
// settlement could otherwise interleave their read-modify-write and lose
// a turn or regress the summary version.
var summaryLocks sync.Map // sessionID -> *sync.Mutex

func (s *Server) updateSessionSummaryRolling(sess *sessions.Session, userMsg, assistantMsg string, toolsUsed []string) {
	if sess == nil {
		return
	}

	lockRaw, _ := summaryLocks.LoadOrStore(sess.ID, &sync.Mutex{})
	lock := lockRaw.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	// Regenerate carries no fresh user text — retrieve against the last
	// user turn so the summary reflects the turn that actually ran.
	if strings.TrimSpace(userMsg) == "" {
		for i := len(sess.Messages) - 1; i >= 0; i-- {
			if sess.Messages[i].Role == "user" {
				userMsg = sess.Messages[i].Content
				break
			}
		}
	}

	base, err := s.store.SummaryForRun(sess.ID)
	if err != nil {
		logging.Default().Warn("summary", "load for update: %v", err)
		return
	}

	updated := sessions.UpdateSummaryFromTurn(base, sess.ID, sess.Mode, userMsg, assistantMsg, toolsUsed)

	if err := s.store.SaveSummary(updated); err != nil {
		logging.Default().Warn("summary", "save: %v", err)
		return
	}

	logging.Default().Info(
		"summary",
		// v1.9.5 LOG CONTRACT: no session id in ordinary logs.
		"objective updated: version=%d objective=%t facts=%d decisions=%d files=%d",
		updated.Version,
		updated.Objective != "",
		len(updated.ImportantFacts), len(updated.KeyDecisions), len(updated.FilesAndArtifacts),
	)
}

// writeAgentHandoff updates <workspace>/agent.md after a COMPLETED agent
// run. v1.2.8.1: EVERY completed agent-mode run writes the handoff —
// including runs with no engineering evidence, which write an honest
// "No engineering changes were made." section (HandoffFromTaskState).
// Chat mode never reaches this path (the caller gates on ModeAgent).
// Returns the written path, or "" with a NON-NIL error when the handoff
// could not be written — the caller must surface the actual failure and
// must not claim the handoff exists.
func (s *Server) writeAgentHandoff(sess *sessions.Session, task *agent.TaskState, outcome string) (string, error) {
	if sess == nil || task == nil {
		return "", fmt.Errorf("handoff: no task state for session %s", sess.ID)
	}

	cfg := s.src.Load()

	wsRoot := strings.TrimSpace(cfg.EffectiveWorkspaceRoot())
	if wsRoot == "" {
		return "", fmt.Errorf("handoff: no workspace root configured")
	}

	h := agent.HandoffFromTaskState(*task, task.Verification, outcome)

	// The handoff is written ONLY to the exact lowercase filename
	// `agent.md` (Windows case-insensitivity — never a second, differently
	// cased file).
	path := filepath.Join(wsRoot, agent.HandoffFileName)

	if err := agent.WriteHandoffFile(path, h); err != nil {
		logging.Default().Warn("handoff", "write %s: %v", path, err)
		return "", err
	}

	logging.Default().Info(
		"handoff",
		"agent.md handoff updated at %s (outcome=%s files=%d commands=%d tests=%d)",
		path, outcome, len(task.FilesChanged), len(task.CommandsRun), len(task.TestsRun),
	)

	return path, nil
}

// marshalForLog keeps the settle-path log lines honest without dumping
// payloads (used by tests as well).
func marshalForLog(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	if len(data) > 512 {
		return string(data[:512])
	}
	return string(data)
}

// unusedTimeGuard keeps the time import honest when log shapes change.
var _ = time.Now
var _ = os.Getpid
