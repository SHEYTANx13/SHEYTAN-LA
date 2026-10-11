package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/screen"
)

// VisionCheck (v1.0.6) is installed by the runtime: it returns an error when
// the active engine cannot SEE images (local engine without a multimodal
// projector, or a remote provider where tool-result images are not part of
// the wire protocol). The screenshot tool refuses politely instead of
// returning pixels the model is unable to look at. Always nil in CLI mode —
// callers must nil-check.
var VisionCheck func() error

// Screenshot is the v1.0.6 vision input: it grabs the primary display with a
// pure-syscall GDI chain (no console flash, no external binary) and hands
// the PNG to the model through the [[IMG:…]] bridge — the orchestrator moves
// it onto the tool message and the multimodal client turns it into an
// image_url part the vision encoder actually sees.
type Screenshot struct{}

func (Screenshot) Name() string { return "screenshot" }

func (Screenshot) Description() string {
	return "Capture the primary display and SEE it (vision). Returns the screenshot as an image the model can analyze — UI states, error dialogs, charts, anything currently on screen. Use it when the user asks 'what's on my screen', 'look at this error', or when a visual check would answer the question faster than text."
}

func (Screenshot) Parameters() any {
	return struct {
		Monitor int    `json:"monitor,omitempty"`
		Note    string `json:"note,omitempty"`
	}{}
}

func (Screenshot) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Monitor int    `json:"monitor"`
		Note    string `json:"note"`
	}
	_ = json.Unmarshal(args, &p)
	if p.Monitor != 0 {
		return "", fmt.Errorf("only the primary display (monitor 0) is supported in this release")
	}

	// Gate on vision: capturing pixels the model cannot see wastes a turn.
	if VisionCheck != nil {
		if err := VisionCheck(); err != nil {
			return "", fmt.Errorf("vision unavailable: %v", err)
		}
	}
	if !screen.Supported() {
		return "", fmt.Errorf("screen capture is only supported on Windows")
	}

	png, err := screen.CapturePNG(0)
	if err != nil {
		return "", err
	}

	dir := BaseDir()
	if dir == "" {
		return "", fmt.Errorf("base dir not configured")
	}
	shots := filepath.Join(dir, "logs", "screenshots")
	if err := os.MkdirAll(shots, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(shots, "screen-"+time.Now().Format("20060102-150405")+".png")
	if err := os.WriteFile(dst, png, 0o644); err != nil {
		return "", err
	}

	// v1.1.4: bound the screenshot directory (see pruneScreenshots).
	pruneScreenshots(shots, 50)
	if OnFileCreated != nil {
		OnFileCreated(dst)
	}

	note := "Analyze the screenshot and answer the user's question about it."
	if p.Note != "" {
		note = p.Note
	}
	return fmt.Sprintf("Captured the primary display (%d bytes) → %s\n%s\n[[IMG:%s]]",
		len(png), dst, note, dst), nil
}
