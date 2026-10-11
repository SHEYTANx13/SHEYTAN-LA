package research

import (
	"testing"
)

func TestWebBackendAliasUsesDuckDuckGo(t *testing.T) {
	service := NewService(ServiceConfig{})

	provider := &mockResearchProvider{
		name: BackendDuckDuckGo,
	}

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if err := service.SetBackend(BackendWeb); err != nil {
		t.Fatalf(
			"SetBackend(%q) returned error: %v",
			BackendWeb,
			err,
		)
	}

	if got := service.Backend(); got != BackendDuckDuckGo {
		t.Fatalf(
			"expected backend %q, got %q",
			BackendDuckDuckGo,
			got,
		)
	}

	resolved, ok := service.Provider(BackendWeb)
	if !ok {
		t.Fatal("expected web alias provider lookup to succeed")
	}

	if resolved != provider {
		t.Fatal("web alias returned a different provider")
	}

	service.Unregister(BackendWeb)

	if _, ok := service.Provider(BackendDuckDuckGo); ok {
		t.Fatal("expected unregistering web alias to remove duckduckgo provider")
	}
}
