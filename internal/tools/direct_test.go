package tools

func directTools() []Tool {
	services := NewServices()
	return []Tool{bashTool(services), readTool(), writeTool(services), editTool(services)}
}
