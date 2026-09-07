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

// renameProvider moves a provider entry (only when its API matches, so an
// unrelated user-named provider is left alone) and rewrites every route, pin,
// and cached catalog that referenced the old key.
func (c *Config) renameProvider(from, to, api string) {
	p, ok := c.Providers[from]
	if !ok || p.API != api {
		return
	}
	if _, taken := c.Providers[to]; taken {
		c.RemoveProvider(from)
		return
	}
	delete(c.Providers, from)
	c.Providers[to] = p
	for id, m := range c.Models {
		for i, prov := range m.Providers {
			if prov == from {
				m.Providers[i] = to
			}
		}
		c.Models[id] = m
	}
	for _, pin := range []*string{&c.DefaultProvider, &c.CompactProvider, &c.TaskProvider} {
		if *pin == from {
			*pin = to
		}
	}
	cats := LoadCatalogs()
	if cat, ok := cats[from]; ok {
		delete(cats, from)
		cats[to] = cat
		_ = SaveCatalogs(cats)
	}
	logf("config.provider", "renamed provider %s -> %s", from, to)
}
