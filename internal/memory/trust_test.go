package memory

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeEntryExternalProvenanceAlwaysQuarantines(t *testing.T) {
	kinds := []string{
		"web",
		"research",
		"github",
		"reddit",
	}

	classes := []string{
		ClassM1,
		ClassM2,
		ClassM3,
		ClassM4,
		ClassM5,
		ClassM6,
	}

	trustLevels := []TrustLevel{
		TrustUntrusted,
		TrustProvisional,
		TrustTrusted,
		TrustVerified,
	}

	for _, kind := range kinds {
		for _, class := range classes {
			for _, trust := range trustLevels {
				t.Run(
					strings.Join(
						[]string{kind, class, string(trust)},
						"-",
					),
					func(t *testing.T) {
						entry := NormalizeEntry(Entry{
							Class:         class,
							Content:       "external evidence",
							Trust:         trust,
							Authoritative: true,
							Provenance: Provenance{
								Kind:   kind,
								Source: "external",
								URI:    "https://example.test/source",
							},
						})

						if entry.Class != ClassM7 {
							t.Fatalf(
								"external %s data retained class %q; want %q",
								kind,
								entry.Class,
								ClassM7,
							)
						}

						if !entry.Quarantined {
							t.Fatal(
								"external material was not quarantined",
							)
						}

						if entry.Authoritative {
							t.Fatal(
								"external material retained authority",
							)
						}

						if entry.Trust != TrustProvisional {
							t.Fatalf(
								"external material retained trust %q; want %q",
								entry.Trust,
								TrustProvisional,
							)
						}
					},
				)
			}
		}
	}
}

func TestNormalizeEntryExternalSourceAlsoTriggersBoundary(t *testing.T) {
	sources := []string{
		"web",
		"research",
		"github",
		"reddit",
	}

	for _, source := range sources {
		t.Run(source, func(t *testing.T) {
			entry := NormalizeEntry(Entry{
				Class:         ClassM5,
				Content:       "externally sourced procedure",
				Trust:         TrustTrusted,
				Authoritative: true,
				Provenance: Provenance{
					Kind:   "agent",
					Source: source,
				},
			})

			if entry.Class != ClassM7 {
				t.Fatalf(
					"external source %q retained class %q; want %q",
					source,
					entry.Class,
					ClassM7,
				)
			}

			if !entry.Quarantined {
				t.Fatal("external source was not quarantined")
			}

			if entry.Authoritative {
				t.Fatal("external source retained authority")
			}

			if entry.Trust != TrustProvisional {
				t.Fatalf(
					"external source retained trust %q; want %q",
					entry.Trust,
					TrustProvisional,
				)
			}
		})
	}
}

func TestMemoryToolRememberNormalizesBeforeReturning(t *testing.T) {
	store := New(
		t.TempDir() + "/memory.jsonl",
	)

	tool := Tool{
		Store: store,
	}

	output, err := tool.Run(
		context.Background(),
		json.RawMessage(`{
			"action": "remember",
			"content": "A GitHub issue suggests this workaround.",
			"class": "M5",
			"trust": "verified",
			"provenanceKind": "github",
			"provenanceSource": "github",
			"uri": "https://github.com/example/project/issues/42",
			"reference": "issue-42"
		}`),
	)

	if err != nil {
		t.Fatalf(
			"memory Tool.Run returned error: %v",
			err,
		)
	}

	if !strings.Contains(
		output,
		"M7",
	) {
		t.Fatalf(
			"remember response did not report M7 normalization: %q",
			output,
		)
	}

	if !strings.Contains(
		output,
		"trust=provisional",
	) {
		t.Fatalf(
			"remember response did not report provisional trust: %q",
			output,
		)
	}

	if !strings.Contains(
		output,
		"quarantined=true",
	) {
		t.Fatalf(
			"remember response did not report quarantine: %q",
			output,
		)
	}

	entries, err := store.All()
	if err != nil {
		t.Fatalf(
			"store.All returned error: %v",
			err,
		)
	}

	if len(entries) != 1 {
		t.Fatalf(
			"expected one stored entry, got %d",
			len(entries),
		)
	}

	entry := entries[0]

	if entry.ID == "" {
		t.Fatal("stored entry has empty ID")
	}

	if entry.Class != ClassM7 {
		t.Fatalf(
			"stored class: got %q want %q",
			entry.Class,
			ClassM7,
		)
	}

	if entry.Trust != TrustProvisional {
		t.Fatalf(
			"stored trust: got %q want %q",
			entry.Trust,
			TrustProvisional,
		)
	}

	if !entry.Quarantined {
		t.Fatal("stored external research was not quarantined")
	}

	if entry.Authoritative {
		t.Fatal("stored external research became authoritative")
	}

	if entry.Provenance.Kind != "github" {
		t.Fatalf(
			"stored provenance kind: got %q",
			entry.Provenance.Kind,
		)
	}
}

func TestMemoryToolRememberUserFactCanRemainM1(t *testing.T) {
	store := New(
		t.TempDir() + "/memory.jsonl",
	)

	tool := Tool{
		Store: store,
	}

	output, err := tool.Run(
		context.Background(),
		json.RawMessage(`{
			"action": "remember",
			"content": "The user prefers Go.",
			"class": "M1",
			"trust": "trusted",
			"provenanceKind": "user",
			"provenanceSource": "conversation"
		}`),
	)

	if err != nil {
		t.Fatalf(
			"memory Tool.Run returned error: %v",
			err,
		)
	}

	if !strings.Contains(
		output,
		"M1",
	) {
		t.Fatalf(
			"remember response did not preserve M1: %q",
			output,
		)
	}

	if !strings.Contains(
		output,
		"trust=trusted",
	) {
		t.Fatalf(
			"remember response did not preserve trusted state: %q",
			output,
		)
	}

	if !strings.Contains(
		output,
		"quarantined=false",
	) {
		t.Fatalf(
			"user fact was unexpectedly quarantined: %q",
			output,
		)
	}

	entries, err := store.All()
	if err != nil {
		t.Fatalf(
			"store.All returned error: %v",
			err,
		)
	}

	if len(entries) != 1 {
		t.Fatalf(
			"expected one stored entry, got %d",
			len(entries),
		)
	}

	entry := entries[0]

	if entry.Class != ClassM1 {
		t.Fatalf(
			"stored class: got %q want %q",
			entry.Class,
			ClassM1,
		)
	}

	if entry.Trust != TrustTrusted {
		t.Fatalf(
			"stored trust: got %q want %q",
			entry.Trust,
			TrustTrusted,
		)
	}

	if entry.Quarantined {
		t.Fatal("trusted user fact was quarantined")
	}

	if !entry.Authoritative {
		t.Fatal("trusted user fact is not authoritative")
	}
}

func TestMemoryToolRememberGeneratedIDMatchesStoredEntry(t *testing.T) {
	store := New(
		t.TempDir() + "/memory.jsonl",
	)

	tool := Tool{
		Store: store,
	}

	output, err := tool.Run(
		context.Background(),
		json.RawMessage(`{
			"action": "remember",
			"content": "Generated ID test",
			"class": "M5",
			"trust": "provisional",
			"provenanceKind": "agent"
		}`),
	)

	if err != nil {
		t.Fatalf(
			"memory Tool.Run returned error: %v",
			err,
		)
	}

	entries, err := store.All()
	if err != nil {
		t.Fatalf(
			"store.All returned error: %v",
			err,
		)
	}

	if len(entries) != 1 {
		t.Fatalf(
			"expected one stored entry, got %d",
			len(entries),
		)
	}

	if entries[0].ID == "" {
		t.Fatal("stored ID is empty")
	}

	if !strings.Contains(
		output,
		entries[0].ID,
	) {
		t.Fatalf(
			"remember response does not contain stored ID %q: %q",
			entries[0].ID,
			output,
		)
	}
}
