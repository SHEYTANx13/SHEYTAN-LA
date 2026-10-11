package tools

import (
        "context"
        "encoding/json"
        "fmt"
        "math/rand"
        "os"
        "path/filepath"
        "strings"
        "sync"
        "time"

        "github.com/chromedp/chromedp"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/browser"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
)

// --- Browser automation (human-like, with page understanding) ---

// BrowserTool drives a persistent Chromium/Chrome/Edge session so the agent
// can work with the web the way a human does: navigate, click, type, scroll,
// read, and take screenshots. The `extract` action returns a structured
// understanding of the current page (text, links, buttons, form fields) so
// the LLM can reason about it.
type BrowserTool struct {
        cfg *config.Config

        mu   sync.Mutex
        sess *browser.Session
}

// NewBrowserTool constructs the browser tool bound to the app config.
func NewBrowserTool(cfg *config.Config) *BrowserTool {
        return &BrowserTool{cfg: cfg}
}

func (b *BrowserTool) Name() string { return "browser" }

func (b *BrowserTool) Description() string {
        return `Automate a real Chromium browser like a human, and understand pages.
Actions (pass exactly one per call, JSON object):
  navigate  {"url":"https://..."}          — open a page (waits for load)
  click     {"selector":"#id|.class|xpath|text=Visible Text"} — click a link/button
  type      {"selector":"...","text":"hello","clear":true}   — type text like a human (per-char delays)
  press     {"key":"Enter|Tab|Escape|ArrowDown|..."}         — press a keyboard key
  scroll    {"direction":"down|up","amount":600}             — scroll the page
  extract   {"maxChars":3000}               — READ + UNDERSTAND the page: returns URL, title,
                                              description, visible text, top links, buttons, form fields
  text      {"selector":"..."}              — innerText of one element (or the whole page if omitted)
  screenshot {}                             — full-page PNG, saved under the logs dir; returns the path
  wait      {"selector":"..."} or {"ms":1500} — wait for an element to appear, or sleep
  url       {}                              — current URL + title only (cheap)
  eval      {"js":"1+1"}                    — run JavaScript in the page, returns JSON result
  back | forward | reload                   — history navigation
  hover     {"selector":"..."}              — hover an element
  select    {"selector":"select#s","value":"opt"} — choose an <option> by value
  close     {}                              — shut the browser down (frees memory)
Tips: start with navigate, then extract to understand the page, then act.
Selectors accept CSS ("#main .btn"), XPath ("//a[contains(.,'Sign in')]"),
or human text ("text=Sign in").`
}

func (b *BrowserTool) Parameters() any {
        return struct {
                Action    string `json:"action"`
                URL       string `json:"url,omitempty"`
                Selector  string `json:"selector,omitempty"`
                Text      string `json:"text,omitempty"`
                Key       string `json:"key,omitempty"`
                Direction string `json:"direction,omitempty"`
                Amount    int    `json:"amount,omitempty"`
                Value     string `json:"value,omitempty"`
                Js        string `json:"js,omitempty"`
                MaxChars  int    `json:"maxChars,omitempty"`
                Clear     bool   `json:"clear,omitempty"`
                Ms        int    `json:"ms,omitempty"`
        }{}
}

// pageInfo is the structured page understanding returned by `extract`.
type pageInfo struct {
        URL         string `json:"url"`
        Title       string `json:"title"`
        Description string `json:"description"`
        Text        string `json:"text"`
        Links       []struct {
                Text string `json:"text"`
                Href string `json:"href"`
        } `json:"links"`
        Buttons []string `json:"buttons"`
        Inputs  []struct {
                Tag         string `json:"tag"`
                Type        string `json:"type"`
                Name        string `json:"name"`
                ID          string `json:"id"`
                Placeholder string `json:"placeholder"`
        } `json:"inputs"`
        ReadyState string `json:"readyState"`
}

const extractJS = `(() => {
  const abs = h => { try { return new URL(h, location.href).href } catch (e) { return h } };
  const vis = el => { const r = el.getBoundingClientRect(); return r.width > 0 && r.height > 0 };
  const links = [...document.querySelectorAll('a[href]')].filter(vis).slice(0, 30)
    .map(a => ({ text: (a.innerText || a.textContent || '').trim().slice(0, 80), href: abs(a.getAttribute('href')) }));
  const buttons = [...document.querySelectorAll('button, input[type=submit], input[type=button], [role=button]')]
    .filter(vis).slice(0, 20)
    .map(b => (b.innerText || b.value || b.getAttribute('aria-label') || '').trim().slice(0, 60))
    .filter(Boolean);
  const inputs = [...document.querySelectorAll('input, textarea, select')].filter(vis).slice(0, 20)
    .map(i => ({ tag: i.tagName.toLowerCase(), type: i.type || '', name: i.name || '',
                 id: i.id || '', placeholder: i.getAttribute('placeholder') || '' }));
  const d = document.querySelector('meta[name="description"]');
  return {
    url: location.href,
    title: document.title,
    description: d ? d.content : '',
    text: (document.body ? document.body.innerText : '').replace(/\n{3,}/g, '\n\n').slice(0, 4000),
    links, buttons, inputs,
    readyState: document.readyState
  };
})()`

// Run executes one browser action.
func (b *BrowserTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
        var p struct {
                Action    string `json:"action"`
                URL       string `json:"url"`
                Selector  string `json:"selector"`
                Text      string `json:"text"`
                Key       string `json:"key"`
                Direction string `json:"direction"`
                Amount    int    `json:"amount"`
                Value     string `json:"value"`
                Js        string `json:"js"`
                MaxChars  int    `json:"maxChars"`
                Clear     bool   `json:"clear"`
                Ms        int    `json:"ms"`
        }
        if err := json.Unmarshal(args, &p); err != nil {
                // Detect the common mistake of nesting the action parameters:
                // {"action":{"navigate":{"url":...}}} → teach the flat format.
                var probe map[string]json.RawMessage
                if json.Unmarshal(args, &probe) == nil {
                        if rawAction, ok := probe["action"]; ok && len(rawAction) > 0 && rawAction[0] == '{' {
                                var inner map[string]any
                                if json.Unmarshal(rawAction, &inner) == nil && len(inner) > 0 {
                                        // figure out which action they meant
                                        for k := range inner {
                                                return "", fmt.Errorf(
                                                        "invalid args: \"action\" must be a STRING like %q, not a nested object. "+
                                                                "Did you mean {\"action\":\"%s\", ...}? Flatten the parameters, e.g. "+
                                                                "{\"action\":\"%s\",\"url\":\"https://example.com\"}", "navigate", k, k)
                                        }
                                }
                        }
                }
                return "", fmt.Errorf("bad args: %w — expected a flat JSON object like {\"action\":\"navigate\",\"url\":\"https://example.com\"}", err)
        }
        if p.Action == "" {
                return "", fmt.Errorf("action is required (navigate|click|type|press|scroll|extract|text|screenshot|wait|url|eval|back|forward|reload|hover|select|close)")
        }

        // `close` needs no live session.
        if p.Action == "close" {
                b.closeSession()
                return "browser closed", nil
        }

        // Offline fast-fail for web actions: local file:// pages keep
        // working, but there is no point launching a browser to reach a
        // remote URL with no connectivity.
        if netcheck.IsOffline() {
                remote := true
                if p.Action == "navigate" {
                        u := strings.TrimSpace(p.URL)
                        if strings.HasPrefix(u, "file://") || filepath.IsAbs(u) {
                                remote = false
                        }
                }
                if remote {
                        logging.Default().Warn("browser", "offline — refusing web action %q", p.Action)
                        return "", fmt.Errorf("browser tool unavailable for web pages: no internet connection detected. " +
                                "Local pages (file:// URLs) still work. webSearch is also offline; " +
                                "all local tools (files, shell, codeExec, git, dataAnalysis, memory) remain fully available")
                }
        }

        // Whole-action timeout.
        actx, cancel := context.WithTimeout(ctx, 45*time.Second)
        defer cancel()

        sess, err := b.session(ctx)
        if err != nil {
                return "", err
        }

        human := func() { // human-like pause between interactions
                d := 150 + rand.Intn(250) + b.cfg.BrowserSlowMo
                select {
                case <-time.After(time.Duration(d) * time.Millisecond):
                case <-actx.Done():
                }
        }

        switch strings.ToLower(p.Action) {
        case "navigate":
                url := strings.TrimSpace(p.URL)
                if url == "" {
                        return "", fmt.Errorf("url is required")
                }
                if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
                        url = "https://" + url
                }
                human()
                var title, finalURL string
                err := sess.Do(actx,
                        chromedp.Navigate(url),
                        // v1.4.0: replace the fixed 300 ms post-navigate sleep with the
                        // REAL readiness conditions — chromedp.WaitReady blocks until the
                        // document is interactive and WaitVisible asserts the body
                        // actually rendered. Elapsed time was a proxy for these states;
                        // slow pages previously raced the title extraction while fast
                        // pages paid a fixed latency tax.
                        chromedp.WaitReady("body"),
                        chromedp.WaitVisible("body"),
                        chromedp.Evaluate(`document.title`, &title),
                        chromedp.Evaluate(`location.href`, &finalURL),
                )
                if err != nil {
                        return "", fmt.Errorf("navigate: %w", err)
                }
                sess.CountNav()
                logging.Default().Info("browser", "navigated to %s (%q)", finalURL, title)
                return fmt.Sprintf("Loaded %s\nTitle: %s", finalURL, title), nil

        case "click":
                if p.Selector == "" {
                        return "", fmt.Errorf("selector is required")
                }
                human()
                if err := sess.Do(actx, chromedp.Click(p.Selector, selectorOpts(p.Selector)...)); err != nil {
                        return "", fmt.Errorf("click %q: %w", p.Selector, err)
                }
                human()
                // report where we landed
                var after string
                _ = sess.Do(actx, chromedp.Evaluate(`location.href`, &after))
                return fmt.Sprintf("clicked %q — now at %s", p.Selector, after), nil

        case "type":
                if p.Text == "" {
                        return "", fmt.Errorf("text is required")
                }
                if p.Selector == "" {
                        return "", fmt.Errorf("selector is required (type into a specific element)")
                }
                human()
                actions := []chromedp.Action{}
                if p.Clear {
                        actions = append(actions, chromedp.Clear(p.Selector, selectorOpts(p.Selector)...))
                }
                actions = append(actions, chromedp.Click(p.Selector, selectorOpts(p.Selector)...))
                if err := sess.Do(actx, actions...); err != nil {
                        return "", fmt.Errorf("focus %q: %w", p.Selector, err)
                }
                // per-character typing with jitter = human-like
                for _, r := range p.Text {
                        if actx.Err() != nil {
                                break
                        }
                        if err := sess.Do(actx, chromedp.KeyEvent(string(r))); err != nil {
                                return "", fmt.Errorf("typing: %w", err)
                        }
                        d := 25 + rand.Intn(55) + b.cfg.BrowserSlowMo
                        select {
                        case <-time.After(time.Duration(d) * time.Millisecond):
                        case <-actx.Done():
                        }
                }
                return fmt.Sprintf("typed %d characters into %q", len(p.Text), p.Selector), nil

        case "press":
                if p.Key == "" {
                        return "", fmt.Errorf("key is required")
                }
                human()
                if err := sess.Do(actx, chromedp.KeyEvent(p.Key)); err != nil {
                        return "", fmt.Errorf("press %q: %w", p.Key, err)
                }
                return fmt.Sprintf("pressed %s", p.Key), nil

        case "scroll":
                dir := strings.ToLower(p.Direction)
                if dir == "" {
                        dir = "down"
                }
                if dir != "down" && dir != "up" && dir != "top" && dir != "bottom" {
                        return "", fmt.Errorf("direction must be down|up|top|bottom")
                }
                if p.Amount <= 0 {
                        p.Amount = 600
                }
                human()
                js := fmt.Sprintf(`window.scrollBy({top:%d, behavior:'smooth'})`, p.Amount)
                switch dir {
                case "top":
                        js = `window.scrollTo({top:0, behavior:'smooth'})`
                case "bottom":
                        js = `window.scrollTo({top:document.body.scrollHeight, behavior:'smooth'})`
                case "up":
                        js = fmt.Sprintf(`window.scrollBy({top:%d, behavior:'smooth'})`, -p.Amount)
                }
                var dummy any
                // v1.4.0: the smooth scroll is an async animation — the REAL
                // completion condition is the scroll position itself, not a fixed
                // 500 ms nap. The poll loop observes the actual window.scrollY /
                // scrollHeight state and settles as soon as the browser reaches
                // the commanded position (bounded by the action context).
                if err := sess.Do(actx,
                        chromedp.Evaluate(js, &dummy),
                        chromedp.Poll(`window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 8 || window.scrollY === 0`, &dummy, chromedp.WithPollingTimeout(2*time.Second)),
                ); err != nil {
                        // A mid-document scrollBy legitimately never reaches the
                        // bottom or the top: the poll timeout is EXPECTED there.
                        // The scroll command itself already ran (Evaluate above);
                        // success is reported because the animation was commanded,
                        // not because a terminal position was verified.
                        logging.Default().Debug(
                                "browser",
                                "scroll %s settled without reaching a terminal position (partial scroll)",
                                dir,
                        )
                }
                return fmt.Sprintf("scrolled %s", dir), nil

        case "extract":
                maxChars := p.MaxChars
                if maxChars <= 0 {
                        maxChars = 3000
                }
                var info pageInfo
                if err := sess.Do(actx, chromedp.Evaluate(extractJS, &info)); err != nil {
                        return "", fmt.Errorf("extract: %w", err)
                }
                return formatPageInfo(&info, maxChars), nil

        case "text":
                var text string
                var err error
                if p.Selector == "" {
                        err = sess.Do(actx, chromedp.Evaluate(`document.body ? document.body.innerText : ''`, &text))
                } else {
                        err = sess.Do(actx, chromedp.Text(p.Selector, &text, selectorOpts(p.Selector)...))
                }
                if err != nil {
                        return "", fmt.Errorf("text: %w", err)
                }
                return clip(text, 4000), nil

        case "screenshot":
                dir := b.cfg.ScreenshotsDir()
                if err := os.MkdirAll(dir, 0o755); err != nil {
                        return "", err
                }
                var buf []byte
                if err := sess.Do(actx, chromedp.FullScreenshot(&buf, 90)); err != nil {
                        return "", fmt.Errorf("screenshot: %w", err)
                }
                path := filepath.Join(dir, "shot-"+time.Now().Format("20060102-150405")+".png")
                if err := os.WriteFile(path, buf, 0o644); err != nil {
                        return "", err
                }

                // v1.1.4: screenshots used to accumulate without bound — every
                // capture lived on disk forever. Keep the most recent 50.
                pruneScreenshots(dir, 50)

                logging.Default().Info("browser", "screenshot saved: %s (%d KB)", path, len(buf)/1024)
                return fmt.Sprintf("screenshot saved: %s (%d KB)", path, len(buf)/1024), nil

        case "wait":
                if p.Selector != "" {
                        if err := sess.Do(actx, chromedp.WaitVisible(p.Selector, selectorOpts(p.Selector)...)); err != nil {
                                return "", fmt.Errorf("wait for %q: %w", p.Selector, err)
                        }
                        return fmt.Sprintf("element visible: %s", p.Selector), nil
                }
                ms := p.Ms
                if ms <= 0 {
                        ms = 1000
                }
                select {
                case <-time.After(time.Duration(ms) * time.Millisecond):
                case <-actx.Done():
                }
                return fmt.Sprintf("waited %dms", ms), nil

        case "url":
                var url, title string
                if err := sess.Do(actx,
                        chromedp.Evaluate(`location.href`, &url),
                        chromedp.Evaluate(`document.title`, &title),
                ); err != nil {
                        return "", fmt.Errorf("url: %w", err)
                }
                return fmt.Sprintf("URL: %s\nTitle: %s", url, title), nil

        case "eval":
                if p.Js == "" {
                        return "", fmt.Errorf("js is required")
                }
                var res any
                if err := sess.Do(actx, chromedp.Evaluate(p.Js, &res)); err != nil {
                        return "", fmt.Errorf("eval: %w", err)
                }
                out, err := json.Marshal(res)
                if err != nil {
                        out = []byte(fmt.Sprintf("%v", res))
                }
                return clip(string(out), 4000), nil

        case "back":
                if err := sess.Do(actx, chromedp.NavigateBack()); err != nil {
                        return "", fmt.Errorf("back: %w", err)
                }
                return "went back", nil

        case "forward":
                if err := sess.Do(actx, chromedp.NavigateForward()); err != nil {
                        return "", fmt.Errorf("forward: %w", err)
                }
                return "went forward", nil

        case "reload":
                if err := sess.Do(actx, chromedp.Reload()); err != nil {
                        return "", fmt.Errorf("reload: %w", err)
                }
                return "reloaded", nil

        case "hover":
                if p.Selector == "" {
                        return "", fmt.Errorf("selector is required")
                }
                human()
                if err := sess.Do(actx, browser.HoverJS(p.Selector)); err != nil {
                        return "", fmt.Errorf("hover: %w", err)
                }
                return fmt.Sprintf("hovered %q", p.Selector), nil

        case "select":
                if p.Selector == "" || p.Value == "" {
                        return "", fmt.Errorf("selector and value are required")
                }
                human()
                if err := sess.Do(actx, chromedp.SetValue(p.Selector, p.Value, selectorOpts(p.Selector)...)); err != nil {
                        return "", fmt.Errorf("select: %w", err)
                }
                return fmt.Sprintf("set %q = %q", p.Selector, p.Value), nil

        default:
                return "", fmt.Errorf("unknown action %q", p.Action)
        }
}

// selectorOpts converts a friendly selector into chromedp query options:
// "text=Sign in" → plain-text search, XPath expressions → search, else CSS.
func selectorOpts(sel string) []chromedp.QueryOption {
        s := strings.TrimSpace(sel)
        if strings.HasPrefix(s, "text=") {
                return []chromedp.QueryOption{chromedp.BySearch}
        }
        if strings.HasPrefix(s, "//") || strings.HasPrefix(s, "(") || strings.HasPrefix(s, "./") {
                return []chromedp.QueryOption{chromedp.BySearch}
        }
        return nil
}

// session lazily boots the shared browser session. The parent context
// bounds startup (30s) and is propagated so a hung Chrome can never block
// the agent loop forever.
func (b *BrowserTool) session(parent context.Context) (*browser.Session, error) {
        b.mu.Lock()
        defer b.mu.Unlock()
        if b.sess != nil && b.sess.Alive() {
                return b.sess, nil
        }
        if b.sess != nil {
                b.sess.Close()
                b.sess = nil
        }
        execPath, err := browser.FindChrome(b.cfg.BrowserExecutablePath)
        if err != nil {
                logging.Default().Error("browser", "discovery failed: %v", err)
                return nil, err
        }
        bootCtx, cancel := context.WithTimeout(parent, 30*time.Second)
        defer cancel()
        sess, err := browser.NewSession(bootCtx, execPath, b.cfg.BrowserProfileDir(), b.cfg.BrowserHeadless, time.Duration(b.cfg.BrowserSlowMo)*time.Millisecond)
        if err != nil {
                return nil, err
        }
        b.sess = sess
        return sess, nil
}

// Close shuts the browser down (called at app exit).
func (b *BrowserTool) Close() { b.closeSession() }

func (b *BrowserTool) closeSession() {
        b.mu.Lock()
        defer b.mu.Unlock()
        if b.sess != nil {
                b.sess.Close()
                b.sess = nil
        }
}

// formatPageInfo renders the structured page understanding for the LLM.
func formatPageInfo(info *pageInfo, maxChars int) string {
        var b strings.Builder
        fmt.Fprintf(&b, "URL:         %s\n", info.URL)
        fmt.Fprintf(&b, "Title:       %s\n", info.Title)
        if info.Description != "" {
                fmt.Fprintf(&b, "Description: %s\n", clip(info.Description, 200))
        }
        fmt.Fprintf(&b, "ReadyState:  %s\n", info.ReadyState)

        b.WriteString("\n--- Page text ---\n")
        text := info.Text
        if maxChars > 0 && len(text) > maxChars {
                text = text[:maxChars] + fmt.Sprintf("\n…[truncated %d chars]", len(info.Text)-maxChars)
        }
        b.WriteString(text)

        if len(info.Links) > 0 {
                b.WriteString("\n--- Links ---\n")
                for i, l := range info.Links {
                        label := l.Text
                        if label == "" {
                                label = "(no text)"
                        }
                        fmt.Fprintf(&b, "%d. %s → %s\n", i+1, clip(label, 80), l.Href)
                }
        }
        if len(info.Buttons) > 0 {
                b.WriteString("\n--- Buttons ---\n")
                b.WriteString(strings.Join(info.Buttons, " | "))
                b.WriteString("\n")
        }
        if len(info.Inputs) > 0 {
                b.WriteString("\n--- Form fields ---\n")
                for _, i := range info.Inputs {
                        desc := i.Tag
                        if i.Type != "" {
                                desc += " type=" + i.Type
                        }
                        if i.Name != "" {
                                desc += " name=" + i.Name
                        }
                        if i.ID != "" {
                                desc += " #" + i.ID
                        }
                        if i.Placeholder != "" {
                                desc += fmt.Sprintf(" (%q)", i.Placeholder)
                        }
                        b.WriteString(desc + "\n")
                }
        }
        return b.String()
}

func clip(s string, n int) string {
        if len(s) <= n {
                return s
        }
        return s[:n] + "…"
}
