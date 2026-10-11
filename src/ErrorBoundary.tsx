import { Component, type ErrorInfo, type ReactNode } from "react";

// v1.2.2 — the black-screen fix. Before this file there was NO React error
// boundary anywhere in the app: a single render exception in any panel
// (e.g. SystemPanel reading `gpus.length` off a `null` GPU list) unmounted
// the ENTIRE React tree and left a dead window. One panel failing must
// never blank the whole application — each boundary renders a scoped
// recover/reload surface instead.

interface BoundaryProps {
  // Human label shown in the failure card ("System Centre", "Agent"…).
  label: string;
  // Changing this value RESETS the boundary (e.g. switching workspace
  // views) so a previous failure cannot pin the panel.
  resetKey?: string | number;
  children: ReactNode;
}

interface BoundaryState {
  error: Error | null;
}

// AppErrorBoundary is the top-level boundary around the whole workspace.
// It keeps the app chrome honest: a catastrophic render failure shows a
// full recovery card (reload / try again) instead of a black window.
export class AppErrorBoundary extends Component<
  { children: ReactNode },
  BoundaryState
> {
  override state: BoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): BoundaryState {
    return { error };
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    // Best-effort console trail — the runtime log viewer keeps the rest.
    console.error(
      "[SHEYTAN] application error boundary:",
      error,
      info.componentStack,
    );
  }

  private reset = (): void => {
    this.setState({ error: null });
  };

  private reload = (): void => {
    window.location.reload();
  };

  override render(): ReactNode {
    const { error } = this.state;

    if (!error) {
      return this.props.children;
    }

    return (
      <div className="error-boundary error-boundary-app" role="alert">
        <div className="error-boundary-mark">✕</div>
        <strong>SHEYTAN hit an unexpected error</strong>
        <p className="error-boundary-detail">
          {error.message || String(error)}
        </p>
        <div className="error-boundary-actions">
          <button
            type="button"
            className="secondary-button"
            onClick={this.reset}
          >
            Try again
          </button>
          <button type="button" className="text-button" onClick={this.reload}>
            Reload app
          </button>
        </div>
      </div>
    );
  }
}

// PanelErrorBoundary wraps ONE lazy workspace panel (Agent, System,
// Settings, Lab, Research). A crash inside the panel is contained to the
// panel's own card — navigation and the rest of the app keep working.
export class PanelErrorBoundary extends Component<
  BoundaryProps,
  BoundaryState
> {
  override state: BoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): BoundaryState {
    return { error };
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error(
      `[SHEYTAN] panel error boundary (${this.props.label}):`,
      error,
      info.componentStack,
    );
  }

  override componentDidUpdate(prevProps: BoundaryProps): void {
    // View switch (resetKey change) clears a stale failure so the panel
    // remounts fresh.
    if (
      this.state.error !== null &&
      prevProps.resetKey !== this.props.resetKey
    ) {
      this.setState({ error: null });
    }
  }

  private reset = (): void => {
    this.setState({ error: null });
  };

  private reload = (): void => {
    window.location.reload();
  };

  override render(): ReactNode {
    const { error } = this.state;

    if (!error) {
      return this.props.children;
    }

    return (
      <div className="error-boundary error-boundary-panel" role="alert">
        <div className="error-boundary-mark">✕</div>
        <strong>{this.props.label} could not be displayed</strong>
        <p className="error-boundary-detail">
          {error.message || String(error)}
        </p>
        <div className="error-boundary-actions">
          <button
            type="button"
            className="secondary-button"
            onClick={this.reset}
          >
            Recover {this.props.label}
          </button>
          <button type="button" className="text-button" onClick={this.reload}>
            Reload app
          </button>
        </div>
      </div>
    );
  }
}
