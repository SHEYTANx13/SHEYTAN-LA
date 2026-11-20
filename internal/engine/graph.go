package engine

import (
	"fmt"
)

// Node is one operation in the canonical graph IR: named operands (the
// map keys each kernel reads), the output tensor name, and the declared
// output dtype/shape. The IR is deliberately small and explicit — the
// fixture architecture compiles to it, and future device backends plan
// over the same nodes (work order section 5.1 layer 5/6).
type Node struct {
	Op       string
	Inputs   map[string]string // kernel operand name -> tensor name
	Output   string
	DType    DType
	Shape    Shape
	Attrs    map[string]string
	Produces []string // tensors this node ALSO defines (scratch reuse), optional
}

// Graph is an ordered node list over named tensors. Execution order is
// list order (the adapters own the topological order; Validate enforces
// that every consumed tensor is defined before use — a canonical IR with
// an honest ordering contract, not an implicit one).
type Graph struct {
	// Inputs are the tensor names the caller must supply.
	Inputs []string
	// Outputs are the tensor names the caller consumes.
	Outputs []string
	Nodes   []Node
	// declared records every tensor name the graph defines (inputs +
	// node outputs + produces) for validation.
	declared map[string]bool
}

// NewGraph constructs an empty graph with the given inputs/outputs.
func NewGraph(inputs, outputs []string) *Graph {
	g := &Graph{
		Inputs:   append([]string{}, inputs...),
		Outputs:  append([]string{}, outputs...),
		declared: make(map[string]bool),
	}
	for _, in := range inputs {
		g.declared[in] = true
	}
	return g
}

// Append adds a node, declaring its output (and Produces) tensors.
func (g *Graph) Append(n Node) {
	g.Nodes = append(g.Nodes, n)
	g.declared[n.Output] = true
	for _, p := range n.Produces {
		g.declared[p] = true
	}
}

// Validate enforces the ordering contract: every node's input operands
// must be declared BEFORE the node executes, outputs are declared after,
// the requested outputs exist, and every node's operator/dtype resolves
// in the reference registry (a registry miss is a typed unsupported
// answer surfaced at compile time, not mid-execution).
func (g *Graph) Validate() error {
	known := make(map[string]bool, len(g.declared))
	for _, in := range g.Inputs {
		known[in] = true
	}
	for i, n := range g.Nodes {
		for operand, tensor := range n.Inputs {
			if !known[tensor] {
				return fmt.Errorf("graph node %d (%s): operand %q consumes tensor %q before it is defined", i, n.Op, operand, tensor)
			}
		}
		if _, err := lookupKernel(OpKey{Op: n.Op, DType: n.DType, BackendClass: "cpu"}); err != nil {
			return fmt.Errorf("graph node %d: %w", i, err)
		}
		known[n.Output] = true
		for _, p := range n.Produces {
			known[p] = true
		}
	}
	for _, out := range g.Outputs {
		if !known[out] {
			return fmt.Errorf("graph output %q is never produced", out)
		}
	}
	return nil
}

// executeGraph runs the nodes against the tensor map through the
// reference registry. Every operand must exist (Validate ran at compile
// time; the executor re-checks because tensors is caller-supplied).
func executeGraph(g *Graph, tensors map[string]*Tensor, b *CPUBackend) error {
	for i, n := range g.Nodes {
		fn, err := lookupKernel(OpKey{Op: n.Op, DType: n.DType, BackendClass: "cpu"})
		if err != nil {
			return fmt.Errorf("graph node %d: %w", i, err)
		}
		resolved := make(map[string]*Tensor, len(n.Inputs)+1)
		for operand, tensor := range n.Inputs {
			t, ok := tensors[tensor]
			if !ok {
				return fmt.Errorf("graph node %d (%s): operand tensor %q missing at execution", i, n.Op, tensor)
			}
			resolved[operand] = t
		}
		outT, ok := tensors[n.Output]
		if !ok {
			// The output tensor was not preallocated by the caller:
			// allocate it from the node's declared dtype/shape.
			t, aerr := NewTensor(n.DType, n.Shape)
			if aerr != nil {
				return fmt.Errorf("graph node %d (%s): output allocation: %v", i, n.Op, aerr)
			}
			outT = t
			tensors[n.Output] = outT
		}
		resolved["__out"] = outT
		if err := fn(resolved, n.Attrs, "__out"); err != nil {
			return fmt.Errorf("graph node %d (%s): %v", i, n.Op, err)
		}
	}
	return nil
}
