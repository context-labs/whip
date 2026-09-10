package agentdef

import "slices"

// Operation lists in the order the runtime's grants have always stored them.
var (
	fileOperations  = []string{"read", "write", "edit", "workspace.write"}
	shellOperations = []string{"bash", "shell_start", "browser_exec", "computer_exec", "workspace_process"}
)

// Operations maps capability names to the operations they grant: file
// operations under the files grant, shell-scope operations under the shell
// grant, and whether MCP calls are permitted. Root bootstrap and child
// delegation both use this mapping. Unknown names grant nothing; Validate
// rejects them earlier.
func Operations(capabilities []string) (files, shell []string, mcp bool) {
	var wantFiles, wantShell []string
	for _, name := range capabilities {
		switch name {
		case "read":
			wantFiles = append(wantFiles, "read")
		case "write":
			wantFiles = append(wantFiles, "read", "write", "edit", "workspace.write")
		case "shell":
			wantShell = append(wantShell, "bash", "shell_start", "workspace_process")
		case "browser":
			wantShell = append(wantShell, "browser_exec")
		case "computer":
			wantShell = append(wantShell, "computer_exec")
		case "mcp":
			mcp = true
		}
	}
	return ordered(fileOperations, wantFiles), ordered(shellOperations, wantShell), mcp
}

// ordered returns the canonical operations that were requested, in canonical
// order and without duplicates. A nil result means no grant at all.
func ordered(canonical, requested []string) []string {
	var result []string
	for _, operation := range canonical {
		if slices.Contains(requested, operation) {
			result = append(result, operation)
		}
	}
	return result
}
