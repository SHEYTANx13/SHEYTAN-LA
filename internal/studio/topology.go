package studio

import "context"

// ViewSource produces the topology DTO. The ONE core authority implements
// it (internal/api) over the same read models the governor surface uses —
// the studio never composes a second copy of that state.
type ViewSource interface {
	StudioTopology(ctx context.Context) (Topology, error)
}

// Node is one topology element.
type Node struct {
	ID      string            `json:"id"`
	Kind    string            `json:"kind"` // host|cpu|ram|gpu|npu|engine|governor|sessions|tools|model
	Label   string            `json:"label"`
	Detail  string            `json:"detail,omitempty"`
	Status  string            `json:"status,omitempty"` // ok|warn|offline|unknown
	Metrics map[string]string `json:"metrics,omitempty"`
}

// Edge is one directed relation between nodes.
type Edge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"` // contains|serves|observes
}

// Topology is the complete read-only view DTO.
type Topology struct {
	GeneratedAt string `json:"generatedAt"` // RFC3339 UTC
	AppVersion  string `json:"appVersion"`
	Source      string `json:"source"` // provenance, e.g. "governor read model"
	Nodes       []Node `json:"nodes"`
	Edges       []Edge `json:"edges"`
}

// Validate is the schema gate the viewer server applies before serving:
// a topology must carry at least one node, every node a non-empty ID and
// label, every edge must reference existing nodes. Malformed sources fail
// CLOSED (an error, not a partial render).
func (t Topology) Validate() error {
	if len(t.Nodes) == 0 {
		return errEmptyTopology
	}
	ids := make(map[string]bool, len(t.Nodes))
	for _, n := range t.Nodes {
		if n.ID == "" || n.Label == "" {
			return errInvalidNode
		}
		if ids[n.ID] {
			return errDuplicateNode
		}
		ids[n.ID] = true
	}
	for _, e := range t.Edges {
		if !ids[e.From] || !ids[e.To] {
			return errDanglingEdge
		}
	}
	return nil
}
