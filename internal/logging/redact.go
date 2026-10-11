package logging

// redact.go — v1.8.2 HUMAN-FACING IDENTITY REDACTION (central sink).
//
// The user asked for opaque run/session identity tokens to disappear from
// human-facing logs and reports while internal identity is preserved.
// The redaction is applied EXACTLY ONCE — here, at the point where a log
// record's message becomes a rendered line — so every sink (app.log, the
// UI recent-ring / LogViewer, stderr bootstrap output, crash reports)
// inherits the rule without each call site re-implementing it.
//
// Rules (deliberately narrow — over-redaction destroys diagnostics):
//
//	removed:  runId=<opaque>  runID=<opaque>  session=<opaque>
//	          sessionId=<opaque>  sessionID=<opaque>
//	kept:     timestamps, severity, subsystem, durations, outcomes,
//	          error causes, every other diagnostic field, ordinary prose
//	          that merely CONTAINS the word "session", URLs and paths
//	          (only the identity token inside them is removed, the rest
//	          of the string survives intact)
//
// Internal state is NOT touched by this file: API objects, run state,
// journals/checkpoints, in-memory identity and storage keys keep their
// identifiers. Only the rendered text loses the token.

import "strings"

// redactTokens is the exact key set removed from rendered messages.
// Matching is case-sensitive on purpose: the codebase writes these keys
// in these exact spellings, and a case-insensitive match would risk
// eating unrelated prose.
var redactTokens = []string{
	"runId=",
	"runID=",
	"session=",
	"sessionId=",
	"sessionID=",
}

// tokenStopChars bounds a token's value: a run of characters that ends at
// whitespace or at a punctuation boundary that plausibly separates the
// token from the next diagnostic field. Inside a URL a token may be
// followed by '&' — the '&' survives, the token does not.
const tokenStopChars = " \t\n\r,;)}]\"'&>"

// isRedactStartBoundary reports whether prev can precede a key=value
// token (start of string, whitespace, or an opening/pairing character).
func isRedactStartBoundary(prev byte) bool {
	switch prev {
	case ' ', '\t', '\n', '\r', '(', '[', '{', '"', '\'', '?', '&':
		return true
	}
	return false
}

// RedactIdentityTokens removes the human-facing opaque identity tokens
// from one rendered log message. Idempotent and safe on every string;
// prose without identity tokens is returned unchanged (the common case
// exits on the first scan). When a token is removed, ONE separating
// space before it is removed too (or a trailing one when the token began
// the string), so no double-space scars are left behind.
func RedactIdentityTokens(message string) string {
	if !strings.Contains(message, "=") {
		return message
	}

	changed := true
	for changed {
		changed = false

		for _, token := range redactTokens {
			i := strings.Index(message, token)
			if i < 0 {
				continue
			}

			// Token-start guard: the match must START a token — at the
			// beginning, after whitespace, or after an opening/pairing
			// character. This keeps "xrunId=" or "mysession=" intact and
			// never mangles a word mid-way.
			if i > 0 && !isRedactStartBoundary(message[i-1]) {
				continue
			}

			// End of the opaque value: run to the next stop character.
			j := i + len(token)
			for j < len(message) && !strings.ContainsRune(tokenStopChars, rune(message[j])) {
				j++
			}

			// A token that ends the message takes its preceding
			// separator with it: "kept, session=s123" → "kept", never
			// "kept," with a dangling comma.
			rest := message[j:]
			if strings.TrimSpace(rest) == "" {
				message = strings.TrimRight(message[:i], " \t,;")
				changed = true

				if message == "" {
					return message
				}

				continue
			}

			// An emptied pair of parentheses is consumed too:
			// "run aborted (runId=abc)" → "run aborted", not "run aborted ()".
			if i > 1 && j < len(message) && message[i-1] == '(' && message[j] == ')' {
				i--

				// Consume one separating space before the '(' as well.
				if i > 0 && message[i-1] == ' ' {
					i--
				}
				j++
			} else {
				// Consume one separating space so the join stays clean:
				// prefer a trailing space at the value end, else a leading
				// space before the token (never both, never mid-word).
				if j < len(message) && message[j] == ' ' {
					j++
				} else if i > 0 && message[i-1] == ' ' {
					i--
				}
			}

			message = message[:i] + message[j:]
			changed = true

			if message == "" {
				return message
			}
		}
	}

	return message
}
