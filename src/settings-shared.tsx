// settings-shared.tsx — v1.1.7 shared building blocks for the redesigned
// Options experience:
//
//   Tip        — small delayed hover tooltip (1–2 sentences, CSS-only)
//   Chip       — tiny status pill (Supported / Unsupported / Auto /
//                Restart required / Recommended)
//   fmtNA      — "N/A" for genuinely unmeasured values (never a guess)
//   fmtBytes   — compact byte formatting
//
// Plain language first: tooltips answer "what does this do?" and "what is
// the trade-off?" — no raw engine jargon without a practical effect.

import type { ReactNode } from "react";

export type ChipTone = "neutral" | "good" | "warn" | "bad" | "accent";

export function Chip({
  tone = "neutral",
  children,
}: {
  tone?: ChipTone;
  children: ReactNode;
}) {
  return <span className={`settings-chip chip-${tone}`}>{children}</span>;
}

// RestartChip — engine-affecting options show this next to the control.
export function RestartChip() {
  return <Chip tone="warn">Restart required</Chip>;
}

// Tip — a hover tooltip with a short delay. Content must stay 1–2
// concise sentences; longer documentation belongs elsewhere.
export function Tip({ text }: { text: string }) {
  return (
    <span
      className="settings-tip"
      data-tip={text}
      tabIndex={0}
      aria-label={text}
    >
      ?
    </span>
  );
}

// FieldLabel — option name + optional tooltip + optional chips, laid out
// above the control exactly like the existing .settings-field labels.
export function FieldLabel({
  name,
  tip,
  chips,
}: {
  name: string;
  tip?: string;
  chips?: ReactNode;
}) {
  return (
    <span className="settings-field-label">
      <span className="settings-field-name">
        {name}
        {tip ? <Tip text={tip} /> : null}
      </span>
      {chips ? <span className="settings-field-chips">{chips}</span> : null}
    </span>
  );
}

export function fmtNA(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return "N/A";
  }
  return String(value);
}

export function fmtBytes(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined || !Number.isFinite(bytes)) {
    return "N/A";
  }
  if (bytes >= 1024 ** 3) {
    return `${(bytes / 1024 ** 3).toFixed(1)} GB`;
  }
  if (bytes >= 1024 ** 2) {
    return `${(bytes / 1024 ** 2).toFixed(0)} MB`;
  }
  return `${bytes} B`;
}

// fmtPct — one decimal, honest N/A.
export function fmtPct(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return "N/A";
  }
  return `${value.toFixed(1)}%`;
}

// fmtDelta — signed percentage change for the before/after comparison.
export function fmtDelta(before: number, after: number): string {
  if (!Number.isFinite(before) || !Number.isFinite(after) || before <= 0) {
    return "—";
  }
  const pct = ((after - before) / before) * 100;
  const sign = pct >= 0 ? "+" : "";
  return `${sign}${pct.toFixed(1)}%`;
}
