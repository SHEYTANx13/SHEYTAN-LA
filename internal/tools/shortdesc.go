package tools

// shortdesc.go — v1.1.7 concise tool descriptions for the Options UI.
//
// The FULL Description() strings remain the model-facing operational spec
// (they document the JSON action syntax the agent needs — removing them
// would break orchestration). ShortDescription() is the one-line version
// surfaced by /api/tools so Settings → Tools stays scannable:
//
//      Shell     — Run bounded terminal commands.
//      Files     — Read and modify project files.
//      ...
//
// Detailed documentation lives with the tool specs, not in the UI.

// Shell — bounded terminal commands.
func (Shell) ShortDescription() string {
	return "Run bounded terminal commands."
}

// Files — read and modify project files.
func (Files) ShortDescription() string {
	return "Read and modify project files."
}

// CodeExec — sandboxed Python snippets.
func (CodeExec) ShortDescription() string {
	return "Run short Python snippets for calculations."
}

// Git — repository state and history.
func (Git) ShortDescription() string {
	return "Inspect and manage repository state."
}

// Browser — real browser automation.
func (b *BrowserTool) ShortDescription() string {
	return "Automate a real browser to open and read pages."
}

// Fetch — public URL fetcher.
func (t *FetchTool) ShortDescription() string {
	return "Fetch a public web page or file by URL."
}

// Screenshot — see the screen.
func (Screenshot) ShortDescription() string {
	return "Capture and analyze the current screen."
}

// DataTool — datasets and charts.
func (t *DataTool) ShortDescription() string {
	return "Analyze CSV/JSON data and render charts."
}

// JSONTool — JSON queries.
func (JSONTool) ShortDescription() string {
	return "Query and transform JSON files."
}

// LinuxSim — safe shell simulator.
func (t *LinuxSim) ShortDescription() string {
	return "Run commands in a safe built-in shell simulator."
}

// DiffTool — file comparison.
func (DiffTool) ShortDescription() string {
	return "Compare two files and show the differences."
}

// ArchiveTool — zip/unzip.
func (t *ArchiveTool) ShortDescription() string {
	return "Create and extract ZIP archives."
}
