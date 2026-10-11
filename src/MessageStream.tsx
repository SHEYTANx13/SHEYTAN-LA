import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";

import type { ChatMessage } from "./api";
import { useRuntimeStore } from "./store";
import { memoryEvidenceLine } from "./memory-evidence";
import { isLivePhase, PHASE_LABELS, type RunPhase } from "./run-phase";
import { historySurfaceState } from "./history-surface";
import { recordStreamTrace } from "./stream-trace";
import {
  activityTransportDegraded,
  ensureActivityWsOrigin,
} from "./config";

const MAX_RENDERED_MESSAGES = 200;

// v1.2.0: stick-to-bottom threshold (px from the bottom still counts as
// "the user is reading the latest message").
const AUTO_SCROLL_THRESHOLD = 96;

// v1.2.2: the generation timeline's elapsed clock refresh cadence. One
// tick per second — enough for a live feel, cheap for React.
const GENERATION_CLOCK_MS = 1000;

function formatElapsed(startedAt: number | null): string {
  if (!startedAt) {
    return "0:00";
  }

  const seconds = Math.max(0, Math.floor((Date.now() - startedAt) / 1000));

  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;

  return `${minutes}:${String(rest).padStart(2, "0")}`;
}

function formatBytes(bytes: number): string {
  if (bytes >= 1_048_576) {
    return `${(bytes / 1_048_576).toFixed(1)} MB`;
  }

  if (bytes >= 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }

  return `${bytes} B`;
}

// v1.2.0: per-message clipboard copy with a quiet "Copied" confirmation.
function CopyButton({
  text,
  label = "Copy",
}: {
  text: string;
  label?: string;
}) {
  const [copied, setCopied] = useState(false);

  const onCopy = useCallback(() => {
    const done = () => {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    };

    if (navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(text).then(done, () => {
        setCopied(false);
      });
    } else {
      // Clipboard API unavailable (older WebView2): fall back silently.
      const area = document.createElement("textarea");
      area.value = text;
      document.body.appendChild(area);
      area.select();
      try {
        document.execCommand("copy");
        done();
      } catch {
        setCopied(false);
      }
      document.body.removeChild(area);
    }
  }, [text]);

  return (
    <button
      type="button"
      className="copy-button"
      onClick={onCopy}
      title={copied ? "Copied" : label}
      aria-label={copied ? "Copied" : label}
    >
      {copied ? "Copied" : "Copy"}
    </button>
  );
}

// v1.2.0: code blocks gain a language label and one-click copy. The
// highlight classes come from rehype-highlight; the theme is a small
// hand-rolled token set in styles.css (no global hljs theme import).
function CodeBlock({
  className,
  children,
}: {
  className?: string;
  children?: React.ReactNode;
}) {
  const codeRef = useRef<HTMLPreElement | null>(null);
  const match = /language-(\w+)/.exec(className ?? "");
  const language = match ? match[1] : "";

  const text = codeRef.current?.textContent ?? "";

  return (
    <div className="code-block">
      <div className="code-block-head">
        <span className="code-block-lang">{language || "code"}</span>
        <CopyButton text={text} label="Copy code" />
      </div>
      <pre ref={codeRef} className={className}>
        {children}
      </pre>
    </div>
  );
}

function AttachmentChip({
  attachment,
  onRemove,
  previewUrl,
}: {
  attachment: { id?: string; name: string; kind: string; size: number };
  onRemove?: (id: string) => void;
  previewUrl?: string;
}) {
  const isImage =
    attachment.kind === "image" ||
    /\.(png|jpe?g|webp|gif|bmp)$/i.test(attachment.name);

  return (
    <span className={`attachment-chip kind-${attachment.kind}`}>
      {isImage && previewUrl ? (
        <img className="attachment-chip-thumb" src={previewUrl} alt="" />
      ) : (
        <span className="attachment-chip-kind">{attachment.kind}</span>
      )}

      <span className="attachment-chip-name" title={attachment.name}>
        {attachment.name}
      </span>

      <span className="attachment-chip-size">
        {formatBytes(attachment.size)}
      </span>

      {onRemove && attachment.id ? (
        <button
          type="button"
          className="attachment-chip-remove"
          onClick={() => {
            const id = attachment.id;
            if (id) onRemove(id);
          }}
          aria-label={`Remove ${attachment.name}`}
        >
          ×
        </button>
      ) : null}
    </span>
  );
}

// v1.2.0: assistant content renders as real Markdown — fenced code with
// highlighting + copy, tables, lists, links. User content stays plain
// text (it is what the user typed). Streaming bubbles stay plain text by
// design: the rAF-coalescing path renders one update per frame, and the
// completed message is parsed once when it lands in history.
const Markdown = memo(function Markdown({ content }: { content: string }) {
  return (
    <div className="message-markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
        components={{
          pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
          a: ({ href, children }) => (
            <a href={href} target="_blank" rel="noreferrer noopener">
              {children}
            </a>
          ),
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
});

const MessageBubble = memo(function MessageBubble({
  message,
  query,
}: {
  message: ChatMessage;
  query: string | null;
}) {
  const isUser = message.role === "user";

  // v1.8.5 SHOW/HIDE THINKING: visibility only — the persisted user
  // preference gates the PRESENTATION of backend-reported reasoning; it
  // never changes what was (or is) generated. Absent reasoning stays
  // absent — nothing is fabricated when hidden or shown.
  const showThinking = useRuntimeStore((state) => state.showThinking);

  // v1.1.4: recall feedback. 👍/👎 on an assistant reply steers the
  // recall engine's future relevance scoring (the backend steering has
  // existed since v1.0.6 — this is its first user-facing write path).
  const sendFeedback = useRuntimeStore((state) => state.sendFeedback);
  const [feedback, setFeedback] = useState<"liked" | "disliked" | null>(null);
  const [feedbackError, setFeedbackError] = useState(false);

  const handleFeedback = (liked: boolean) => {
    if (!query || feedback) {
      return;
    }

    const next = liked ? "liked" : "disliked";

    setFeedback(next);

    sendFeedback(query, liked).catch(() => {
      // Optimistic verdict failed — revert so the user can retry.
      setFeedback(null);
      setFeedbackError(true);
    });
  };

  return (
    <article className={`message-row ${isUser ? "from-user" : "from-agent"}`}>
      <div className="message-avatar" aria-hidden="true">
        {isUser ? "U" : "S"}
      </div>

      <div className="message-bubble">
        {message.reasoning && showThinking ? (
          <ReasoningPanel reasoning={message.reasoning} live={false} />
        ) : null}

        {isUser ? (
          <p className="message-content">{message.content}</p>
        ) : (
          <Markdown content={message.content} />
        )}

        {/* v1.2.0: image attachments render inline (data URLs from the
            backend's vision wire format); missing values render nothing. */}
        {message.images && message.images.length > 0 ? (
          <div className="message-images">
            {message.images.map((src, index) => (
              <img
                key={index}
                src={src}
                alt={`attached image ${index + 1}`}
                className="message-image"
                loading="lazy"
              />
            ))}
          </div>
        ) : null}

        {message.attachments && message.attachments.length > 0 ? (
          <div className="message-attachments">
            {message.attachments.map((name) => (
              <span key={name} className="attachment-chip kind-text">
                <span className="attachment-chip-name" title={name}>
                  {name}
                </span>
              </span>
            ))}
          </div>
        ) : null}

        <div className="message-meta">
          {!isUser && message.content && query ? (
            <div
              className="message-feedback"
              role="group"
              aria-label="Rate this reply for recall relevance"
            >
              <button
                type="button"
                className={`feedback-button like ${feedback === "liked" ? "active" : ""}`}
                disabled={feedback !== null}
                onClick={() => handleFeedback(true)}
                aria-label="Helpful — prioritize similar past exchanges in recall"
                title="Helpful — recall will prioritize this exchange"
              >
                ◆
              </button>

              <button
                type="button"
                className={`feedback-button dislike ${feedback === "disliked" ? "active" : ""}`}
                disabled={feedback !== null}
                onClick={() => handleFeedback(false)}
                aria-label="Not helpful — deprioritize similar past exchanges"
                title="Not helpful — recall will deprioritize this exchange"
              >
                ◇
              </button>

              {feedbackError ? (
                <span className="feedback-error">couldn't save rating</span>
              ) : null}
            </div>
          ) : null}

          {message.content ? <CopyButton text={message.content} /> : null}
        </div>
      </div>
    </article>
  );
});

// ReasoningPanel is the collapsible reasoning surface used BOTH while
// streaming (auto-open while the model thinks, auto-fold when the answer
// starts, always user-togglable) and after completion inside the settled
// message bubble. Only text the backend actually emitted is shown.
const ReasoningPanel = memo(function ReasoningPanel({
  reasoning,
  live,
}: {
  reasoning: string;
  live: boolean;
}) {
  const [manualOpen, setManualOpen] = useState<boolean | null>(null);

  // Auto-open while the run is still in the Thinking phase; auto-fold
  // once Generating begins — unless the user made an explicit choice.
  const open = manualOpen ?? live;

  return (
    <details
      className="message-reasoning"
      open={open}
      onToggle={(event) => {
        // User intent wins over any automatic behaviour.
        setManualOpen((event.target as HTMLDetailsElement).open);
      }}
    >
      <summary>
        {live ? "Thinking… " : ""}Reasoning
        <span className="message-reasoning-meta">
          {reasoning.length.toLocaleString()} chars
        </span>
      </summary>

      <p className="message-reasoning-body">{reasoning}</p>
    </details>
  );
});

// GenerationBubble is the v1.2.2 live generation timeline. It mounts the
// INSTANT the run starts (no dead gap while the engine gate warms up) and
// walks the real backend-driven lifecycle:
//   Preparing → Thinking → Generating → Finalising → (history replaces it)
// Activity (tool/context/engine) events render progressively alongside.
function GenerationBubble() {
  const runPhase = useRuntimeStore((state) => state.runPhase);
  const runStartedAt = useRuntimeStore((state) => state.runStartedAt);
  const runNote = useRuntimeStore((state) => state.runNote);
  const streaming = useRuntimeStore((state) => state.streaming);
  const activity = useRuntimeStore((state) => state.activity);
  // v1.8.2: the backend-truth memory evidence for the current run.
  const memoryEvidence = useRuntimeStore((state) => state.memoryEvidence);

  const [, setClockTick] = useState(0);

  // One lightweight tick per second while a run is live — drives the
  // elapsed clock only (content updates ride the store's rAF coalescing).
  useEffect(() => {
    if (!isLivePhase(runPhase)) {
      return;
    }

    const timer = window.setInterval(
      () => setClockTick((tick) => tick + 1),
      GENERATION_CLOCK_MS,
    );

    return () => window.clearInterval(timer);
  }, [runPhase]);

  // Progressive activity alongside the generation (tool/context/engine).
  const runActivity = useMemo(() => {
    const interesting = activity.filter(
      (item) =>
        item.type === "tool_start" ||
        item.type === "tool_end" ||
        item.type === "engine" ||
        item.type === "context" ||
        item.type === "thinking" ||
        // v1.2.5: tier escalation notices join the visible timeline.
        item.type === "escalation",
    );

    return interesting.slice(-6);
  }, [activity]);

  const reasoning = streaming?.reasoning ?? "";
  const content = streaming?.content ?? "";

  // v1.8.5 SHOW/HIDE THINKING (live bubble): the same persisted
  // visibility preference gates the LIVE reasoning presentation.
  // Visibility only — the reasoning stream keeps folding into the store
  // (cumulative snapshots unchanged), the request's reasoning level and
  // budget are unaffected, and switching back reveals the real
  // backend-reported reasoning that accumulated while hidden.
  const showThinking = useRuntimeStore((s) => s.showThinking);

  // v1.2.5: the panel follows the backend's OWN thinking_start/end
  // markers (store thinkingPanelOpen) — auto-open while the model thinks,
  // fold when the answer streams (explicit user choice overrides both).
  const thinkingPanelOpen = useRuntimeStore((s) => s.thinkingPanelOpen);
  const reasoningLive =
    showThinking &&
    (thinkingPanelOpen || runPhase === "thinking" || runPhase === "preparing");

  // v1.8.2: a concise FACTUAL state while the run is in the thinking
  // phase and the model has emitted no reasoning stream. Never fake
  // reasoning, never generated labels presented as model thoughts — a
  // plain statement of what is (not) happening, replaced the moment real
  // reasoning arrives. v1.8.5: hidden reasoning is NOT "no reasoning" —
  // the honest note only renders while the surface is visible.
  const noReasoningNote =
    showThinking &&
    !reasoning &&
    (runPhase === "thinking" || runPhase === "preparing")
      ? "Thinking · this model is not exposing a reasoning stream"
      : null;

  // v1.8.2: the memory line is the backend's own evidence (null while
  // the context report has not arrived yet — nothing is shown rather
  // than a guess).
  const memoryLine = memoryEvidenceLine(memoryEvidence);

  // v1.9.4 (P0-A): the DOM-VISIBILITY boundary — what the live bubble
  // actually renders after each commit, recorded as LENGTHS only. This
  // is the last client boundary before the compositor; a run whose
  // store/flush boundaries advance while this stays flat is the exact
  // last-good/first-bad split the release contract requires the trace to
  // identify. Disabled builds pay one boolean check per commit.
  useEffect(() => {
    recordStreamTrace("dom_visible", {
      contentChars: content.length,
      reasoningChars: reasoning.length,
      batchDepth: runActivity.length,
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- trace-only; the values above ARE the observation
  }, [content, reasoning]);

  return (
    <article
      className="message-row from-agent generation-row"
      aria-live="polite"
    >
      <div className="message-avatar" aria-hidden="true">
        S
      </div>

      <div className="message-bubble streaming generation-bubble">
        <div className="generation-timeline">
          <span
            className={`generation-phase phase-${runPhase}${
              runPhase === "finalising" ? " settling" : ""
            }`}
          >
            <span className="generation-phase-dot" aria-hidden="true" />
            {PHASE_LABELS[runPhase] ?? runPhase}
          </span>

          <span className="generation-elapsed" title="Elapsed generation time">
            {formatElapsed(runStartedAt)}
          </span>
        </div>

        {reasoning && showThinking ? (
          <ReasoningPanel reasoning={reasoning} live={reasoningLive} />
        ) : noReasoningNote ? (
          <p className="generation-memory" data-role="no-reasoning">
            {noReasoningNote}
          </p>
        ) : null}

        <p className="message-content">
          {content || (
            <span data-stream-placeholder="true">
              {runPhase === "preparing"
                ? "Connecting to the engine and preparing the turn…"
                : "…"}
            </span>
          )}

          {runPhase !== "finalising" ? (
            <span className="stream-cursor" aria-hidden="true" />
          ) : null}
        </p>

        {memoryLine ? (
          <p className="generation-memory" data-role="memory-evidence">
            {memoryLine}
          </p>
        ) : null}

        {runActivity.length > 0 ? (
          <div
            className="conversation-activity in-generation"
            aria-label="Runtime activity"
          >
            {runActivity.map((item) => (
              <span
                key={item.id}
                className={`conversation-activity-item type-${item.type}`}
              >
                <span className="conversation-activity-type">{item.type}</span>

                <span className="conversation-activity-caption">
                  {typeof item.data.caption === "string"
                    ? item.data.caption
                    : item.type}
                </span>
              </span>
            ))}
          </div>
        ) : null}

        {runPhase === "finalising" && runNote ? (
          <p className="generation-note">{runNote}</p>
        ) : null}
      </div>
    </article>
  );
}

// phaseTone maps a settled/terminal phase for the note surface.
function phaseTone(runPhase: RunPhase): string {
  if (runPhase === "complete") return "complete";
  if (runPhase === "error") return "error";
  if (runPhase === "aborted") return "aborted";
  return "idle";
}

// RunOutcomeNote surfaces the settled outcome of the LAST run (Complete /
// Stopped / Failed with the backend's own caption) directly under the
// conversation — never invented, only what the wire carried.
function RunOutcomeNote() {
  const runPhase = useRuntimeStore((state) => state.runPhase);
  const runNote = useRuntimeStore((state) => state.runNote);

  if (runPhase === "idle" || isLivePhase(runPhase)) {
    return null;
  }

  const tone = phaseTone(runPhase);

  return (
    <div className={`run-outcome tone-${tone}`} role="status">
      <span className="run-outcome-label">
        {PHASE_LABELS[runPhase] ?? runPhase}
      </span>
      {runNote ? <span className="run-outcome-note">{runNote}</span> : null}
    </div>
  );
}

function StreamingBubble() {
  const streaming = useRuntimeStore((state) => state.streaming);
  const runPhase = useRuntimeStore((state) => state.runPhase);
  // v1.8.5: the same visibility preference gates this safety-net surface
  // (presentation only — the data path is untouched).
  const showThinking = useRuntimeStore((state) => state.showThinking);

  if (!streaming) {
    return null;
  }

  // While a run is live the GenerationBubble owns the surface; the plain
  // StreamingBubble only exists as a safety net for stray streaming data
  // after the timeline settled (e.g. a late flush racing finalisation).
  if (isLivePhase(runPhase)) {
    return null;
  }

  return (
    <article className="message-row from-agent">
      <div className="message-avatar" aria-hidden="true">
        S
      </div>

      <div className="message-bubble streaming">
        {streaming.reasoning && showThinking ? (
          <p className="message-reasoning-body">{streaming.reasoning}</p>
        ) : null}

        <p className="message-content">
          {streaming.content || (
            <span data-stream-placeholder="true">…</span>
          )}

          <span className="stream-cursor" aria-hidden="true" />
        </p>
      </div>
    </article>
  );
}

function EmptyConversation() {
  const engineState = useRuntimeStore((state) => state.engine?.state);
  // v1.1.8: mode-aware empty state — Chat invites conversation, Agent
  // invites a task. Same stream, same runtime underneath.
  const mode = useRuntimeStore((state) => state.mode);
  const chat = mode === "chat";

  const engineLive =
    engineState === "ready" ||
    engineState === "running" ||
    engineState === "busy";

  return (
    <div className="conversation-empty">
      <div className="activity-empty-mark">✦</div>

      <strong>{chat ? "Ready when you are" : "Conversation ready"}</strong>

      <span>
        {!engineLive
          ? chat
            ? "Send a message below — the engine starts automatically."
            : "Send a task below — the engine starts automatically."
          : chat
            ? "Engine is live. Say hello, ask anything, or attach files."
            : "Engine is live. Send a task below to begin."}
      </span>
    </div>
  );
}

// v1.4.0: the transcript surface distinguishes the four real states.
// "Loading history" and "loading failure" are NEVER rendered as the
// empty-conversation invitation — an in-flight (or failed) fetch says
// nothing about whether the history is empty.
function HistoryLoading() {
  return (
    <div className="conversation-empty" data-history-state="loading">
      <div className="activity-empty-mark">✦</div>
      <strong>Loading conversation…</strong>
      <span>Fetching this session's history from the backend.</span>
    </div>
  );
}

function HistoryError({
  message,
  onRetry,
}: {
  message: string;
  onRetry: () => void;
}) {
  return (
    <div className="conversation-empty" data-history-state="error">
      <div className="activity-empty-mark">!</div>
      <strong>Could not load this conversation</strong>
      <span>{message}</span>
      <button type="button" className="text-button" onClick={onRetry}>
        Retry loading history
      </button>
    </div>
  );
}

function MessageStream() {
  const messages = useRuntimeStore((state) => state.messages);
  const streaming = useRuntimeStore((state) => state.streaming);
  const running = useRuntimeStore((state) => state.running);
  const runPhase = useRuntimeStore((state) => state.runPhase);
  const activity = useRuntimeStore((state) => state.activity);

  // v1.4.0: the explicit transcript surface states — loading, error,
  // ready — from the authoritative store (see store.ts loadSession).
  const historyStatus = useRuntimeStore((state) => state.historyStatus);
  const historyError = useRuntimeStore((state) => state.historyError);
  const activeSessionId = useRuntimeStore(
    (state) => state.activeSessionId,
  );
  const loadSession = useRuntimeStore((state) => state.loadSession);

  // v1.2.8: lazy history paging — "Load earlier" fetches the next older
  // page on demand; the browser never needs the whole transcript.
  const olderHasMore = useRuntimeStore((state) => state.olderHasMore);
  const olderLoading = useRuntimeStore((state) => state.olderLoading);
  const loadOlderMessages = useRuntimeStore((state) => state.loadOlderMessages);

  const streamRef = useRef<HTMLDivElement | null>(null);
  const endRef = useRef<HTMLDivElement | null>(null);
  const stickToBottomRef = useRef(true);
  const [showJump, setShowJump] = useState(false);

  // v1.9.5 (P0): the desktop activity transport's degraded state is a
  // VISIBLE fault, never a silent one — when the packaged desktop page
  // cannot discover the loopback transport, live answer streaming is
  // broken and the user must know instead of staring at a silent run.
  const [transportDegraded, setTransportDegraded] = useState(
    activityTransportDegraded,
  );

  useEffect(() => {
    let cancelled = false;

    void ensureActivityWsOrigin().then(() => {
      if (!cancelled) {
        setTransportDegraded(activityTransportDegraded);
      }
    });

    return () => {
      cancelled = true;
    };
  }, []);

  // v1.4.0: ONE pure decision function (history-surface.ts) selects the
  // transcript surface state; the UI and the unit tests share it.
  const surface = historySurfaceState({
    historyStatus,
    messageCount: messages.length,
    streaming: Boolean(streaming),
    runLive: isLivePhase(runPhase),
  });

  const visibleMessages = useMemo(
    () =>
      messages.length > MAX_RENDERED_MESSAGES
        ? messages.slice(-MAX_RENDERED_MESSAGES)
        : messages,
    [messages],
  );

  // The user message immediately preceding each assistant reply is the
  // exchange's recall query — the backend derives the same capsule id.
  const messageQueries = useMemo(() => {
    const queries: (string | null)[] = [];
    let lastUser: string | null = null;

    for (const message of messages) {
      if (message.role === "user") {
        lastUser = message.content;
        queries.push(null);
      } else {
        queries.push(lastUser);
      }
    }

    return queries;
  }, [messages]);

  const queryWindow =
    messages.length > MAX_RENDERED_MESSAGES
      ? messageQueries.slice(-MAX_RENDERED_MESSAGES)
      : messageQueries;

  // Only the last ~40 activity entries are mirrored inline; the full
  // activity feed stays bounded in the store. While a run is LIVE the
  // GenerationBubble carries its own activity strip, so the standalone
  // block renders only for idle/idle-phase tails.
  const inlineActivity = useMemo(() => {
    const interesting = activity.filter(
      (item) =>
        item.type === "tool_start" ||
        item.type === "tool_end" ||
        item.type === "engine" ||
        item.type === "context",
    );

    return isLivePhase(runPhase) ? [] : interesting.slice(-8);
  }, [activity, runPhase]);

  // v1.2.0: intelligent autoscroll — follow the stream ONLY while the
  // user stays near the bottom; any deliberate scroll-up suspends the
  // follow behaviour and surfaces a jump-to-latest affordance.
  const handleStreamScroll = useCallback(() => {
    const el = streamRef.current;
    if (!el) {
      return;
    }

    const distance = el.scrollHeight - el.scrollTop - el.clientHeight;
    const atBottom = distance <= AUTO_SCROLL_THRESHOLD;
    stickToBottomRef.current = atBottom;
    setShowJump(!atBottom);
  }, []);

  const jumpToLatest = useCallback(() => {
    stickToBottomRef.current = true;
    setShowJump(false);
    endRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, []);

  useEffect(() => {
    if (!stickToBottomRef.current) {
      return;
    }

    const frame = requestAnimationFrame(() => {
      endRef.current?.scrollIntoView({
        behavior: "auto",
        block: "nearest",
      });
    });

    return () => cancelAnimationFrame(frame);
  }, [
    visibleMessages.length,
    streaming?.content,
    streaming?.reasoning,
    inlineActivity.length,
    runPhase,
  ]);

  return (
    <div className="conversation-panel">
      {transportDegraded ? (
        <div className="transport-degraded-banner" role="alert">
          Live answer streaming is unavailable in this window (the desktop
          activity transport could not be reached). Answers may only appear
          after a run completes — restart the application to repair it.
        </div>
      ) : null}
      <div
        className="conversation-stream"
        ref={streamRef}
        onScroll={handleStreamScroll}
      >
        {olderHasMore ? (
          <div className="history-pager">
            <button
              type="button"
              className="text-button"
              onClick={() => void loadOlderMessages()}
              disabled={olderLoading}
            >
              {olderLoading ? "Loading…" : "Load earlier messages"}
            </button>
          </div>
        ) : null}

        {surface === "error" ? (
          <HistoryError
            message={historyError ?? "The history fetch failed."}
            onRetry={() => {
              if (activeSessionId) {
                void loadSession(activeSessionId);
              }
            }}
          />
        ) : surface === "loading" ? (
          <HistoryLoading />
        ) : surface === "empty" ? (
          <EmptyConversation />
        ) : (
          <>
            {visibleMessages.map((message, index) => (
              <MessageBubble
                key={`${message.at ?? "na"}-${message.role}-${index}`}
                message={message}
                query={queryWindow[index] ?? null}
              />
            ))}

            {inlineActivity.length > 0 ? (
              <div
                className="conversation-activity"
                aria-label="Runtime activity"
              >
                {inlineActivity.map((item) => (
                  <span
                    key={item.id}
                    className={`conversation-activity-item type-${item.type}`}
                  >
                    <span className="conversation-activity-type">
                      {item.type}
                    </span>

                    <span className="conversation-activity-caption">
                      {typeof item.data.caption === "string"
                        ? item.data.caption
                        : item.type}
                    </span>
                  </span>
                ))}
              </div>
            ) : null}

            {/* v1.2.2: the live generation timeline — mounts the instant a
                run starts (running || live phase), not just when the first
                token lands. */}
            {running || isLivePhase(runPhase) ? <GenerationBubble /> : null}

            <StreamingBubble />

            <RunOutcomeNote />
          </>
        )}

        <div ref={endRef} />
      </div>

      {showJump ? (
        <button
          type="button"
          className="jump-to-latest"
          onClick={jumpToLatest}
          aria-label="Jump to latest message"
        >
          ↓ Latest
        </button>
      ) : null}
    </div>
  );
}

export { AttachmentChip, CopyButton };

// v1.2.4: memoized export — AgentBody re-renders on many store slices;
// memo keeps the (bounded but non-trivial) message list subtree out of
// those cascades unless its own data changed.
export default memo(MessageStream);
