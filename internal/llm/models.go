package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ListRemoteModels fetches the model list from the active provider's
// /v1/models endpoint (used by the GUI "Test connection" button).
func ListRemoteModels(cfg interface {
	EffectiveBaseURL() string
	EffectiveAPIKey() string
}) ([]string, error) {
	base := cfg.EffectiveBaseURL()
	if base == "" {
		return nil, fmt.Errorf("no base URL configured")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), "GET", base+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.EffectiveAPIKey())
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s/models: %w", base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d from %s/models", resp.StatusCode, base)
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}
	models := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("endpoint reachable but returned no models")
	}
	return models, nil
}
