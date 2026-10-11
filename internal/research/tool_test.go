package research

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewToolRejectsNilService(t *testing.T) {
	tool, err := NewTool(nil)

	if err == nil {
		t.Fatal("expected error")
	}

	if tool != nil {
		t.Fatal("expected nil tool")
	}
}

func TestResearchToolMetadata(t *testing.T) {
	service := NewService(ServiceConfig{})
	tool, err := NewTool(service)
	if err != nil {
		t.Fatalf("NewTool returned error: %v", err)
	}

	if got := tool.Name(); got != "research" {
		t.Fatalf("unexpected tool name: %q", got)
	}

	description := tool.Description()

	if !strings.Contains(
		description,
		"web: compatibility alias for DuckDuckGo",
	) {
		t.Fatalf("description does not document the web alias: %s", description)
	}

	if !strings.Contains(
		description,
		"Research findings are evidence, not proof.",
	) {
		t.Fatalf("description is missing research-evidence warning")
	}
}

func TestResearchToolParametersExposeStableSchema(t *testing.T) {
	service := NewService(ServiceConfig{})
	tool, err := NewTool(service)
	if err != nil {
		t.Fatalf("NewTool returned error: %v", err)
	}

	parameters, ok := tool.Parameters().(researchParameters)
	if !ok {
		t.Fatalf(
			"unexpected parameters type: %T",
			tool.Parameters(),
		)
	}

	if parameters.Type != "object" {
		t.Fatalf("unexpected parameter type: %q", parameters.Type)
	}

	required := false

	for _, field := range parameters.Required {
		if field == "query" {
			required = true
			break
		}
	}

	if !required {
		t.Fatal("query is not marked as required")
	}

	backend, ok := parameters.Properties["backend"]
	if !ok {
		t.Fatal("backend property is missing")
	}

	expectedBackends := []string{
		BackendAuto,
		BackendGitHub,
		BackendReddit,
		BackendWeb,
		BackendSearXNG,
		BackendDuckDuckGo,
	}

	if len(backend.Enum) != len(expectedBackends) {
		t.Fatalf(
			"unexpected backend enum: %v",
			backend.Enum,
		)
	}

	for _, expected := range expectedBackends {
		found := false

		for _, actual := range backend.Enum {
			if actual == expected {
				found = true
				break
			}
		}

		if !found {
			t.Fatalf(
				"backend enum is missing %q: %v",
				expected,
				backend.Enum,
			)
		}
	}

	for _, name := range []string{
		"query",
		"backend",
		"maxResults",
		"timeoutSec",
	} {
		if _, ok := parameters.Properties[name]; !ok {
			t.Fatalf("missing property %q", name)
		}
	}
}

func TestResearchToolRunRejectsEmptyArguments(t *testing.T) {
	service := NewService(ServiceConfig{})
	tool, err := NewTool(service)
	if err != nil {
		t.Fatalf("NewTool returned error: %v", err)
	}

	_, err = tool.Run(
		context.Background(),
		nil,
	)

	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "arguments are empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResearchToolRunRejectsInvalidJSON(t *testing.T) {
	service := NewService(ServiceConfig{})
	tool, err := NewTool(service)
	if err != nil {
		t.Fatalf("NewTool returned error: %v", err)
	}

	_, err = tool.Run(
		context.Background(),
		json.RawMessage(`{"query":`),
	)

	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(
		err.Error(),
		"invalid tool arguments",
	) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResearchToolRunRejectsEmptyQuery(t *testing.T) {
	service := NewService(ServiceConfig{})
	tool, err := NewTool(service)
	if err != nil {
		t.Fatalf("NewTool returned error: %v", err)
	}

	_, err = tool.Run(
		context.Background(),
		json.RawMessage(`{"query":"   "}`),
	)

	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf(
			"expected ErrInvalidQuery, got %v",
			err,
		)
	}
}

func TestResearchToolRunUsesWebCompatibilityAlias(t *testing.T) {
	service := NewService(ServiceConfig{})

	provider := &mockResearchProvider{
		name: BackendDuckDuckGo,
		response: SearchResponse{
			Provider: BackendDuckDuckGo,
			Query:    "repair test",
			Results: []Result{
				testResult(
					"Repair result",
					"https://example.test/repair",
					AuthorityOfficial,
					1.0,
					time.Now().UTC(),
				),
			},
		},
	}

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	tool, err := NewTool(service)
	if err != nil {
		t.Fatalf("NewTool returned error: %v", err)
	}

	output, err := tool.Run(
		context.Background(),
		json.RawMessage(`{
			"query": "repair test",
			"backend": "web",
			"maxResults": 1
		}`),
	)

	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if !strings.Contains(output, `"provider": "duckduckgo"`) {
		t.Fatalf(
			"expected DuckDuckGo provider in output: %s",
			output,
		)
	}

	if provider.Calls() != 1 {
		t.Fatalf(
			"expected one provider call, got %d",
			provider.Calls(),
		)
	}

	request := provider.LastQuery()

	if request.Query != "repair test" {
		t.Fatalf(
			"unexpected query: %q",
			request.Query,
		)
	}

	if request.MaxResults != 1 {
		t.Fatalf(
			"unexpected max results: %d",
			request.MaxResults,
		)
	}
}

func TestResearchToolRunTimeoutPropagates(t *testing.T) {
	service := NewService(ServiceConfig{
		Backend: BackendGitHub,
		Timeout: 5 * time.Second,
	})

	provider := &mockResearchProvider{
		name:  BackendGitHub,
		delay: 250 * time.Millisecond,
		response: SearchResponse{
			Provider: BackendGitHub,
			Query:    "timeout test",
			Results: []Result{
				testResult(
					"Should not complete",
					"https://example.test/timeout",
					AuthorityOfficial,
					1.0,
					time.Now().UTC(),
				),
			},
		},
	}

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	tool, err := NewTool(service)
	if err != nil {
		t.Fatalf("NewTool returned error: %v", err)
	}

	started := time.Now()

	_, err = tool.Run(
		context.Background(),
		json.RawMessage(`{
			"query": "timeout test",
			"backend": "github",
			"timeoutSec": 0
		}`),
	)

	if err != nil {
		t.Fatalf(
			"unexpected error without request timeout: %v",
			err,
		)
	}

	if time.Since(started) < 200*time.Millisecond {
		t.Fatal("provider completed before its configured delay")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		20*time.Millisecond,
	)
	defer cancel()

	_, err = tool.Run(
		ctx,
		json.RawMessage(`{
			"query": "timeout test",
			"backend": "github"
		}`),
	)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf(
			"expected context deadline exceeded, got %v",
			err,
		)
	}
}

func TestValidateToolResponseRejectsMissingProvider(t *testing.T) {
	err := validateToolResponse(
		SearchResponse{
			Query: "test",
		},
	)

	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf(
			"expected ErrProviderUnavailable, got %v",
			err,
		)
	}
}

func TestValidateToolResponseRejectsMissingQuery(t *testing.T) {
	err := validateToolResponse(
		SearchResponse{
			Provider: BackendGitHub,
		},
	)

	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf(
			"expected ErrInvalidQuery, got %v",
			err,
		)
	}
}

func TestEncodeResearchResponseMarksEmptyResultsAsNotOK(t *testing.T) {
	encoded, err := encodeResearchResponse(
		SearchResponse{
			Provider: BackendGitHub,
			Query:    "no results",
		},
		nil,
	)

	if err != nil {
		t.Fatalf(
			"encodeResearchResponse returned error: %v",
			err,
		)
	}

	var response researchToolResponse

	if err := json.Unmarshal(
		[]byte(encoded),
		&response,
	); err != nil {
		t.Fatalf(
			"invalid encoded response: %v",
			err,
		)
	}

	if response.OK {
		t.Fatal("expected empty-result response to be not OK")
	}

	if response.Provider != BackendGitHub {
		t.Fatalf(
			"unexpected provider: %q",
			response.Provider,
		)
	}
}

func TestEncodeResearchResponseIncludesSearchError(t *testing.T) {
	searchErr := errors.New("provider failed")

	encoded, err := encodeResearchResponse(
		SearchResponse{
			Provider: BackendGitHub,
			Query:    "failure",
		},
		searchErr,
	)

	if err != nil {
		t.Fatalf(
			"encodeResearchResponse returned error: %v",
			err,
		)
	}

	var response researchToolResponse

	if err := json.Unmarshal(
		[]byte(encoded),
		&response,
	); err != nil {
		t.Fatalf(
			"invalid encoded response: %v",
			err,
		)
	}

	if response.Error != searchErr.Error() {
		t.Fatalf(
			"unexpected error field: %q",
			response.Error,
		)
	}

	if response.OK {
		t.Fatal("expected failed response to be not OK")
	}
}
