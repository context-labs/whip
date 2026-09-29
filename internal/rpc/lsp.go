package rpc

import (
	"strings"

	"github.com/context-labs/whip/internal/lsp"
	"github.com/context-labs/whip/internal/protocol"
)

func languageServersFromDomain(values []lsp.Status) protocol.LanguageServersResult {
	result := protocol.LanguageServersResult{Items: []protocol.LanguageServerStatus{}}
	for _, value := range values {
		row := protocol.LanguageServerStatus{Name: value.Name, State: strings.ReplaceAll(value.State, " ", "_")}
		if value.Root != "" {
			row.WorkspaceRoot = new(value.Root)
		}
		if value.Err != "" {
			row.Failure = new("Language server connection is unavailable.")
		}
		result.Items = append(result.Items, row)
	}
	return result
}
