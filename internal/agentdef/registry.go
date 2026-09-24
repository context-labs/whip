package agentdef

// definitions lists every first-party definition in display order.
var definitions = []func() Definition{Coding, JuniorDeveloper}

// Lookup returns the definition with the given id.
func Lookup(id string) (Definition, bool) {
	for _, build := range definitions {
		if definition := build(); definition.ID == id {
			return definition, true
		}
	}
	return Definition{}, false
}

// IDs lists every definition id in display order, for validation messages and
// command-line help.
func IDs() []string {
	ids := make([]string, 0, len(definitions))
	for _, build := range definitions {
		ids = append(ids, build().ID)
	}
	return ids
}
