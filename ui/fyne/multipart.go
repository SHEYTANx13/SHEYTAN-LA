//go:build !headless

package fyneui

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/textproto"
	"strings"
)

// multipartWriter builds the /api/attachments multipart body (field
// `files[]`), the exact contract internal/api/attachments.go consumes.
type multipartWriter struct {
	w         *multipart.Writer
	files     []AttachmentInput
	sessionID string
}

func newMultipartWriter(dst io.Writer, files []AttachmentInput, sessionID string) *multipartWriter {
	return &multipartWriter{w: multipart.NewWriter(dst), files: files, sessionID: sessionID}
}

func (m *multipartWriter) ContentType() string { return m.w.FormDataContentType() }

func (m *multipartWriter) Write() error {
	defer m.w.Close()
	if m.sessionID != "" {
		if err := m.w.WriteField("sessionId", m.sessionID); err != nil {
			return err
		}
	}
	for _, f := range m.files {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition",
			`form-data; name="files[]"; filename="`+escapeQuote(f.Name)+`"`)
		h.Set("Content-Type", "application/octet-stream")
		part, err := m.w.CreatePart(h)
		if err != nil {
			return err
		}
		if _, err := io.Copy(part, bytes.NewReader(f.Data)); err != nil {
			return err
		}
	}
	return nil
}

func escapeQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

// jsonPretty renders raw JSON for read-only evidence cards (bounded).
func jsonPretty(raw json.RawMessage, max int) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		s := string(raw)
		if max > 0 && len(s) > max {
			return s[:max] + " …"
		}
		return s
	}
	s := buf.String()
	if max > 0 && len(s) > max {
		return s[:max] + " …"
	}
	return s
}
