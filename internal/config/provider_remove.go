package config

// RemoveProvider forgets a provider: its entry, every model route through it
// (models left with no route are dropped), and any default/compact/task
// provider pin that named it. It also drops the provider's cached catalog.
// The caller owns Save().
func (c *Config) RemoveProvider(name string) {
	delete(c.Providers, name)
	for id, m := range c.Models {
		var keep []string
		for _, p := range m.Providers {
			if p != name {
				keep = append(keep, p)
			}
		}
		if len(keep) == 0 {
			delete(c.Models, id)
			continue
		}
		m.Providers = keep
		c.Models[id] = m
	}
	for _, pin := range []*string{&c.DefaultProvider, &c.CompactProvider, &c.TaskProvider} {
		if *pin == name {
			*pin = ""
		}
	}
	cats := LoadCatalogs()
	if _, ok := cats[name]; ok {
		delete(cats, name)
		_ = SaveCatalogs(cats) // best effort: the TUI never refetches a removed provider
	}
	logf("config.provider", "removed provider %s", name)
}
