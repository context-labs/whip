package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/config"
)

// dimNew marks catalog-advertised routes that have no config entry yet.
const dimNew = "  (new)"

// modelItem is one selectable model@provider route.
type modelItem struct {
	model    string
	provider string
	url      string
	// fromCatalog marks routes advertised by the provider's /models catalog
	// rather than configured in ~/.whip/config.json — rendered dim with a
	// (new) marker.
	fromCatalog bool
}

// modelFilter is the shared type-to-filter for every model-selecting surface
// (/model picker routes, palette model panels): a query plus the row indexes
// it matches. Matches are tiered fuzzy (substring of the model or provider,
// then subsequence), ranked best-first via matchTier. An empty query matches
// everything (match == nil); a non-empty query with no hits yields an empty
// (non-nil) match so views can render "no models match".
type modelFilter struct {
	query string
	match []int // indexes into the caller's list, in ranked order; nil = no query
}

// apply refilters the rows with the given scoring function; empty query
// restores the full list. score returns the match tier for row i (lower is
// better, negative = no match).
func (f *modelFilter) apply(rows int, score func(i int) int) {
	q := strings.ToLower(strings.TrimSpace(f.query))
	if q == "" {
		f.match = nil
		return
	}
	type hit struct {
		i, tier int
	}
	var hits []hit
	for i := range rows {
		if tier := score(i); tier >= 0 {
			hits = append(hits, hit{i, tier})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].tier < hits[b].tier })
	f.match = make([]int, 0, len(hits))
	for _, h := range hits {
		f.match = append(f.match, h.i)
	}
}

// view returns the indexes into rows the filter currently shows (all rows in
// order when the query is empty).
func (f *modelFilter) view(rows int) []int {
	if f.match == nil {
		idx := make([]int, rows)
		for i := range idx {
			idx[i] = i
		}
		return idx
	}
	return f.match
}

// typeRunes appends typed runes to the query; backspace trims one. Both
// return whether the query changed (so callers re-apply).
func (f *modelFilter) typeRunes(rs []rune) bool {
	if len(rs) == 0 {
		return false
	}
	f.query += string(rs)
	return true
}

func (f *modelFilter) backspace() bool {
	if f.query == "" {
		return false
	}
	f.query = f.query[:len(f.query)-1]
	return true
}

// modelPicker is the /model browser: models grouped, providers indented under them.
type modelPicker struct {
	items      []modelItem
	filter     modelFilter // type-to-filter over items
	idx        int
	staleHints []string // providers whose cached catalog is past its TTL
	// sessionOnly marks a picker opened by /model-for-session: the selection
	// switches the model without persisting it as the new default.
	sessionOnly bool
}

// view returns the items the picker is currently showing.
func (p *modelPicker) view() []modelItem {
	idx := p.filter.view(len(p.items))
	out := make([]modelItem, len(idx))
	for i, j := range idx {
		out[i] = p.items[j]
	}
	return out
}

// applyQuery refilters items; empty query restores the full list.
func (p *modelPicker) applyQuery() {
	q := strings.ToLower(strings.TrimSpace(p.filter.query))
	p.filter.apply(len(p.items), func(i int) int {
		it := p.items[i]
		return bestTier(it.model, it.provider, q)
	})
}

// applyModelList filters a plain list of model names (palette compact/subagent
// panels, whose rows are names, not routes). The "(new)" catalog marker and
// the leading "default (…)" row are stripped for scoring so the query matches
// the real name.
func (f *modelFilter) applyModelList(list []string) {
	q := strings.ToLower(strings.TrimSpace(f.query))
	f.apply(len(list), func(i int) int {
		name := strings.TrimSuffix(list[i], dimNew)
		if inner, ok := strings.CutPrefix(name, "default ("); ok {
			name = strings.TrimSuffix(inner, ")")
		}
		return bestTier(name, "", q)
	})
}

// bestTier is the best (lowest non-negative) match tier of the model and
// provider names against query q; -1 if neither matches.
func bestTier(model, provider, q string) int {
	tm, tp := matchTier(model, q), matchTier(provider, q)
	switch {
	case tm >= 0 && tp >= 0:
		return min(tm, tp)
	case tm >= 0:
		return tm
	default:
		return tp
	}
}

// resolveModelFuzzy fuzzy-matches name against the known model routes (config +
// catalog). Exact names pass through untouched. A single best-tier hit wins;
// several equally-good distinct models report false with the candidates named.
func resolveModelFuzzy(cfg *config.Config, name string) (string, bool, []string) {
	if _, ok := cfg.Models[name]; ok {
		return name, true, nil
	}
	for p := range cfg.Providers {
		if cat, ok := config.LoadCatalogs()[p]; ok && cat.Find(name) != nil {
			return name, true, nil // exact catalog id
		}
	}
	q := strings.ToLower(name)
	type hit struct {
		model string
		tier  int
	}
	var hits []hit
	for _, it := range buildModelItems(cfg) {
		if tier := bestTier(it.model, it.provider, q); tier >= 0 {
			hits = append(hits, hit{it.model, tier})
		}
	}
	if len(hits) == 0 {
		return "", false, nil
	}
	best := hits[0].tier
	seen := map[string]bool{}
	var models []string
	for _, h := range hits {
		if h.tier != best || seen[h.model] {
			continue
		}
		seen[h.model] = true
		models = append(models, h.model)
	}
	if len(models) > 1 {
		return "", false, models
	}
	return models[0], true, nil
}

// modelNamesFor lists every selectable model name: cfg.Models sorted
// alphabetically, then catalog-advertised ids without a config entry (marked
// "(new)"), sorted by name. The catalog fallback in Resolve makes the extra
// ids usable without a config entry, so pickers list them alongside.
func modelNamesFor(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Models))
	for _, it := range buildModelItems(cfg) {
		name := it.model
		if it.fromCatalog {
			name += dimNew
		}
		if len(names) == 0 || names[len(names)-1] != name {
			names = append(names, name)
		}
	}
	return names
}

// buildModelItems flattens the config into selectable routes, models sorted
// alphabetically, providers in each model's declared order. Models advertised
// by a provider's cached /models catalog but absent from cfg.Models follow in
// a dim "(new)" section — selecting one resolves through the catalog fallback
// and persists to config only via switchModel.
func buildModelItems(cfg *config.Config) []modelItem {
	names := make([]string, 0, len(cfg.Models))
	for name := range cfg.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	var items []modelItem
	for _, name := range names {
		for _, p := range cfg.Models[name].Providers {
			url := ""
			if prov, ok := cfg.Providers[p]; ok {
				url = prov.BaseURL
			}
			items = append(items, modelItem{model: name, provider: p, url: url})
		}
	}
	return appendCatalogRoutes(items, cfg, config.LoadCatalogs())
}

// appendCatalogRoutes adds one route per catalog-advertised model that has no
// cfg.Models entry, sorted by model name. Configured models win: a catalog id
// already in cfg.Models adds nothing.
func appendCatalogRoutes(items []modelItem, cfg *config.Config, cats map[string]config.Catalog) []modelItem {
	provs := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		provs = append(provs, name)
	}
	sort.Strings(provs)
	var extra []modelItem
	for _, p := range provs {
		cat, ok := cats[p]
		if !ok {
			continue
		}
		for _, mi := range cat.Models {
			if _, configured := cfg.Models[mi.ID]; configured {
				continue
			}
			extra = append(extra, modelItem{model: mi.ID, provider: p, url: cat.BaseURL, fromCatalog: true})
		}
	}
	sort.Slice(extra, func(a, b int) bool {
		if extra[a].model != extra[b].model {
			return extra[a].model < extra[b].model
		}
		return extra[a].provider < extra[b].provider
	})
	return append(items, extra...)
}

// staleCatalogs names configured providers whose cached catalog is missing or
// past its TTL — the picker's hint that freshly announced models may not show.
func staleCatalogs(cfg *config.Config, cats map[string]config.Catalog) []string {
	var out []string
	for name := range cfg.Providers {
		if cat, ok := cats[name]; !ok || cat.Stale() {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func (m *model) openModelPicker(sessionOnly bool) {
	items := buildModelItems(m.cfg)
	if len(items) == 0 {
		m.append(errStyle.Render("no models configured in ~/.whip/config.json"))
		return
	}
	mp := &modelPicker{items: items, staleHints: staleCatalogs(m.cfg, config.LoadCatalogs()), sessionOnly: sessionOnly}
	for i, it := range items { // start on the active route
		if it.model == m.modelName && it.provider == m.provName {
			mp.idx = i
			break
		}
	}
	m.mpicker = mp
}

func (m *model) modelPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.mpicker
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.mpicker = nil
	case tea.KeyUp, tea.KeyCtrlP, tea.KeyShiftTab:
		if p.idx > 0 {
			p.idx--
		}
	case tea.KeyDown, tea.KeyCtrlN, tea.KeyTab:
		if p.idx < len(p.view())-1 {
			p.idx++
		}
	case tea.KeyBackspace:
		if p.filter.backspace() {
			p.applyQuery()
			p.idx = 0
		}
	case tea.KeyEnter:
		v := p.view()
		if len(v) == 0 {
			return m, nil
		}
		it := v[p.idx]
		sessionOnly := p.sessionOnly
		m.mpicker = nil
		m.switchModel(it.model, it.provider, !sessionOnly)
	case tea.KeyRunes, tea.KeySpace:
		p.filter.typeRunes(msg.Runes)
		p.applyQuery()
		if p.idx >= len(p.view()) {
			p.idx = max(len(p.view())-1, 0)
		}
	}
	return m, nil
}

func (m *model) modelPickerView() string {
	p := m.mpicker
	view := p.view()
	var rows []string
	rows = append(rows, "  "+botStyle.Render("/")+p.filter.query+dimStyle.Render("▏"))
	lastModel := ""
	for i, it := range view {
		heading := " " + it.model
		if it.fromCatalog {
			heading = dimStyle.Render(heading + dimNew)
		}
		if it.model != lastModel {
			rows = append(rows, heading)
			lastModel = it.model
		}
		cur := ""
		if it.model == m.modelName && it.provider == m.provName {
			cur = dimStyle.Render("  (current)")
		}
		line := fmt.Sprintf("%-12s  ", it.provider) + dimStyle.Render(it.url)
		if it.fromCatalog {
			line = dimStyle.Render(line)
		}
		if i == p.idx {
			rows = append(rows, botStyle.Render("   → "+line)+cur)
		} else {
			rows = append(rows, "     "+line+cur)
		}
	}
	if len(view) == 0 {
		rows = append(rows, dimStyle.Render("  no models match "+strconv.Quote(p.filter.query)))
	}
	rows = append(rows, dimStyle.Render(fmt.Sprintf("  (%d/%d) type to filter · ↑/↓ select · enter switch · esc cancel", p.idx+1, len(view))))
	if len(p.staleHints) > 0 {
		rows = append(rows, dimStyle.Render("  catalog stale for "+strings.Join(p.staleHints, ", ")+" — /model refresh to pull newly announced models"))
	}
	avail := m.height - 1
	if avail < 1 { // terminal size unknown: no padding or windowing
		return strings.Join(rows, "\n")
	}
	for len(rows) < avail {
		rows = append(rows, "")
	}
	if len(rows) > avail { // small terminals: keep the selection visible
		// selection row = query line (1) + headings so far; approximate with idx+1
		sel := p.idx + 1
		start := max(min(sel-2, len(rows)-avail), 0)
		rows = rows[start : start+avail]
	}
	return strings.Join(rows, "\n")
}
