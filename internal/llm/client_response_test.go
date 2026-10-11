package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func TestChatRejectsEmptyChoices(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			w.Header().Set(
				"Content-Type",
				"application/json",
			)

			_, _ = w.Write(
				[]byte(`{
					"choices": [],
					"usage": {
						"prompt_tokens": 1,
						"completion_tokens": 0,
						"total_tokens": 1
					}
				}`),
			)
		}),
	)

	defer server.Close()

	cfg := config.Default()
	cfg.LLMBaseURL = server.URL
	cfg.Provider = config.ProviderLocal

	client := NewClient(config.NewSource(cfg))

	_, err := client.Chat(
		context.Background(),
		&ChatRequest{
			Model: "test-model",
			Messages: []Message{
				{
					Role:    "user",
					Content: "hello",
				},
			},
		},
	)

	if err == nil {
		t.Fatal(
			"expected empty choices response to return an error",
		)
	}

	if !strings.Contains(
		err.Error(),
		"no choices",
	) {
		t.Fatalf(
			"unexpected error: %v",
			err,
		)
	}
}

func TestChatAcceptsNonEmptyChoices(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			w.Header().Set(
				"Content-Type",
				"application/json",
			)

			_, _ = w.Write(
				[]byte(`{
					"choices": [
						{
							"message": {
								"role": "assistant",
								"content": "hello back"
							},
							"finish_reason": "stop"
						}
					]
				}`),
			)
		}),
	)

	defer server.Close()

	cfg := config.Default()
	cfg.LLMBaseURL = server.URL
	cfg.Provider = config.ProviderLocal

	client := NewClient(config.NewSource(cfg))

	response, err := client.Chat(
		context.Background(),
		&ChatRequest{
			Model: "test-model",
			Messages: []Message{
				{
					Role:    "user",
					Content: "hello",
				},
			},
		},
	)

	if err != nil {
		t.Fatalf(
			"unexpected Chat error: %v",
			err,
		)
	}

	if response == nil {
		t.Fatal("expected response")
	}

	if len(response.Choices) != 1 {
		t.Fatalf(
			"expected one choice, got %d",
			len(response.Choices),
		)
	}

	if response.Choices[0].Message.Content != "hello back" {
		t.Fatalf(
			"unexpected response content: %q",
			response.Choices[0].Message.Content,
		)
	}
}
