package tui

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

// Clickable links (OSC 8 hyperlinks): the terminal owns the click — no mouse
// plumbing here. Two post-render passes over glamour's output:
//
//  1. hyperlinkGlamourLinks rewires rendered markdown links: glamour prints
//     "[label](url)" as underlined `label url` with no hyperlink; the label
//     atoms become one OSC 8 hyperlink to the href and the href atoms vanish
//     (no doubled "label url", at any width). Bare autolinks become clickable
//     in place.
//  2. linkifyRenderedFilePaths wraps bare path/to/file[:N] tokens in file://
//     hyperlinks, gated on the file existing on disk.
//
// linkifyFilePaths applies the same file-ref linkification to raw user text
// (submit/resume/steer echoes), which never goes through glamour.
//     as a second copy. Bare autolinks become clickable in place.
//
// Unsupported terminals ignore OSC 8 and show the underlined text as before;
// ansi.Strip (copy/selection) drops the sequences. Width-safe: verified
// against ansi.Wrap/Hardwrap/StringWidth.

// fileRefRE matches a file reference with an optional :line suffix: a path
// with at least one slash and a dotted extension. The leading run of
// path-ish characters spans the full reference; matches inside [text](target)
// or a URL are rejected in linkifyFilePaths via the preceding byte. Bare
// filenames without a slash are not matched — too common as prose.
var fileRefRE = regexp.MustCompile(
	`/?[\w@+~-][\w@+~.-]*(?:/[\w@+~-][\w@+~.-]*)+(?::\d+)?` + // path with slashes
		`|/?[\w@+~-][\w@+~.-]*\.[A-Za-z]{2,10}(?::\d+)?` + // bare multi-letter ext
		`|\.{1,2}/[\w@+~-][\w@+~.-]*(?:/[\w@+~-][\w@+~.-]*)*(?::\d+)?`,
) // ./ ../

// linkifyFilePaths wraps mentions of existing local files in OSC 8 file://
// hyperlinks. exists decides whether a candidate path names a real file
// (injectable for tests). Markdown link internals are skipped: a match
// immediately preceded by '(' or ']' is part of [text](target) and gets
// handled after rendering instead. Extensionless and bare single-letter-ext
// matches are ignored — prose, not file refs.
func linkifyFilePaths(s string, exists func(string) bool) string {
	return linkifyFilePathsWith(s, exists, absFileURI)
}

func linkifyFilePathsAt(s, root string) string {
	return linkifyFilePathsWith(s, func(path string) bool { return realFileExistsAt(root, path) }, func(path, line string) string {
		return absFileURIAt(root, path, line)
	})
}

func linkifyFilePathsWith(s string, exists func(string) bool, uri func(string, string) string) string {
	return replaceMatches(s, fileRefRE, func(m string, before byte) string {
		// Skip markdown link internals, URL tails, code spans, quotes. A
		// parenthesized path in prose — "(see tui.go)" — is not linkified:
		// '(' also opens a markdown [text](target), which the one-byte
		// lookahead can't distinguish. The markdown path is covered after
		// rendering instead.
		if strings.ContainsRune("([]/:;\"`", rune(before)) {
			return m
		}
		path, line := splitLineRef(m)
		if !isFileRef(path) || !exists(path) {
			return m
		}
		return hyperlink(uri(path, line), m)
	})
}

// isFileRef gates the disk check to strings shaped like a file reference:
// a dotted extension. The regex can't enforce the multi-letter minimum for
// bare filenames (Go regexp {2,10} accepts "go"), so the extension length is
// checked here: paths with a slash take any extension, bare filenames need
// at least two letters (links_test.go yes, tui.go no — too prose-shaped).
func isFileRef(path string) bool {
	dot := strings.LastIndexByte(path, '.')
	if dot < 0 {
		// no extension: only a slashed path counts (internal/tui, /etc/hostname)
		return strings.Contains(path, "/")
	}
	ext := path[dot+1:]
	if strings.ContainsRune(ext, '/') {
		return false // dot was in a directory segment
	}
	if strings.Contains(path, "/") {
		return len(ext) >= 1
	}
	// bare filename with any dotted extension (tui.go, links_test.go): the
	// existence check below is the real gate
	return len(ext) >= 1
}

// hyperlink wraps text in an OSC 8 hyperlink to uri.
func hyperlink(uri, text string) string {
	return ansi.SetHyperlink(uri) + text + ansi.ResetHyperlink()
}

// replaceMatches applies fn to each regex match; fn also receives the byte
// preceding the match (0 at string start) for cheap context checks.
func replaceMatches(s string, re *regexp.Regexp, fn func(m string, before byte) string) string {
	var b strings.Builder
	b.Grow(len(s) + len(s)/4)
	last := 0
	for _, loc := range re.FindAllStringIndex(s, -1) {
		var before byte
		if loc[0] > 0 {
			before = s[loc[0]-1]
		}
		b.WriteString(s[last:loc[0]])
		b.WriteString(fn(s[loc[0]:loc[1]], before))
		last = loc[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// realFileExists stats path relative to the process working directory (whip
// runs at the project root) and reports whether it is a regular file.
func realFileExists(path string) bool {
	return realFileExistsAt("", path)
}

func realFileExistsAt(root, path string) bool {
	if !filepath.IsAbs(path) {
		if root == "" {
			var err error
			root, err = os.Getwd()
			if err != nil {
				return false
			}
		}
		path = filepath.Join(root, path)
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// splitLineRef separates a trailing :N line number and any absorbed trailing
// sentence punctuation from a file reference.
func splitLineRef(ref string) (path, line string) {
	i := strings.LastIndexByte(ref, ':')
	if i > 0 && i < len(ref)-1 && isDigits(ref[i+1:]) {
		return strings.TrimRight(ref[:i], ".,;:!?"), ref[i+1:]
	}
	return strings.TrimRight(ref, ".,;:!?"), ""
}

func isDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

// absFileURI builds an absolute file:// URI. The :line suffix stays in the
// URI path: handlers that understand it jump to the line, the rest still
// open the file or its directory. Returns "" only when the CWD is unknown.
func absFileURI(path, line string) string {
	return absFileURIAt("", path, line)
}

func absFileURIAt(root, path, line string) string {
	if !filepath.IsAbs(path) {
		if root == "" {
			var err error
			root, err = os.Getwd()
			if err != nil {
				return ""
			}
		}
		path = filepath.Join(root, path)
	}
	if line != "" {
		path += ":" + line
	}
	return "file://" + (&url.URL{Path: path}).String()
}

// --- glamour link rewiring -------------------------------------------------

// Rendered-link shape, verified against glamour v1.0.0: text is emitted as
// word atoms, each an SGR span closed by \x1b[0m. Dark theme: LinkText =
// 38;5;35;1m (bold), Link = 38;5;30;4m (underlined); light: 29;1 and 36;4.
// A "[label](url)" link renders as label atoms + doc-colored space atoms +
// href atoms; glamour's wrap may split any atom across lines (a newline
// lands inside the atom). A bare autolink renders as href atoms alone.
//
// hyperlinkGlamourLinks therefore works in two passes over the atoms: first
// it groups consecutive label/href atoms into links (across newlines and
// padding), recording each link's href and label spans; then it rewrites —
// the label atoms become one OSC 8 hyperlink to the href, the href atoms and
// their gap vanish, and a standalone href (autolink) becomes clickable in
// place. Width-independent: the merge is structural, not adjacency-at-one-
// width.
const (
	// glamour's stock dark/light link colors, kept as fallback candidates so
	// hand-built strings and the neutral style keep working
	linkTextSGRDark  = "\x1b[38;5;35;1m"
	linkTextSGRLight = "\x1b[38;5;29;1m"
	linkSGRDark      = "\x1b[38;5;30;4m"
	linkSGRLight     = "\x1b[38;5;36;4m"
	sgrReset         = "\x1b[0m"
)

// linkSGR caches the SGR prefixes the CURRENT markdown style emits for link
// labels (LinkText) and hrefs (Link). The opencode palette uses truecolor,
// so the stock constants above never matched it and links stayed inert;
// rendering a one-link probe through the live renderer learns the real
// prefixes for whatever style is active. invalidateMDRenderer resets it.
var linkSGR struct {
	mu          sync.Mutex
	valid       bool
	label, href string
}

func resetLinkSGRs() {
	linkSGR.mu.Lock()
	linkSGR.valid = false
	linkSGR.mu.Unlock()
}

// linkSGRs returns the label and href SGR prefixes for the active style
// ("" when the probe could not determine one).
func linkSGRs() (label, href string) {
	linkSGR.mu.Lock()
	defer linkSGR.mu.Unlock()
	if linkSGR.valid {
		return linkSGR.label, linkSGR.href
	}
	if r := mdRenderer(120); r != nil {
		if out, err := r.Render("[LinkLabelProbe](http://probe.invalid/p)"); err == nil {
			out = stripOSC8(bareSGR.Replace(out)) // the same normalization renderMarkdownAt applies
			label = sgrBefore(out, "LinkLabelProbe")
			href = sgrBefore(out, "http://probe.invalid/p")
		}
	}
	linkSGR.valid, linkSGR.label, linkSGR.href = true, label, href
	return label, href
}

// osc8RE matches one OSC 8 hyperlink open or close sequence (BEL or ST
// terminated).
var osc8RE = regexp.MustCompile("\x1b\\]8;[^\a\x1b]*(?:\a|\x1b\\\\)")

// stripOSC8 removes glamour v2's own hyperlinks from rendered markdown. whip
// re-links on its own terms below: the label becomes the only clickable text,
// hrefs stop printing, and file destinations become absolute file:// URIs
// only when the file exists.
func stripOSC8(s string) string { return osc8RE.ReplaceAllString(s, "") }

// sgrBefore returns the SGR sequence immediately preceding text in s.
func sgrBefore(s, text string) string {
	i := strings.Index(s, text)
	if i < 0 {
		return ""
	}
	j := strings.LastIndex(s[:i], "\x1b[")
	if j < 0 || !strings.HasSuffix(s[j:i], "m") || strings.ContainsAny(s[j+2:i-1], "\x1b") {
		return ""
	}
	return s[j:i]
}

// linkAtom is one parsed glamour word atom: its SGR span, visible text, and
// byte range in the source. text ends at an embedded newline (word wrap).
type linkAtom struct {
	start, end int // byte range of the whole atom (span + text + reset)
	sgr        string
	kind       byte // 't' label (LinkText), 'h' href (Link)
	text       string
}

// parseLinkAtoms extracts every link atom (label or href) in order.
func parseLinkAtoms(s string) []linkAtom {
	var atoms []linkAtom
	for i := 0; i < len(s); {
		sgr, kind := linkAtomAt(s[i:])
		if kind == 0 {
			i++
			continue
		}
		end, text := scanAtom(s, i, sgr)
		if end < 0 {
			i++
			continue
		}
		atoms = append(atoms, linkAtom{start: i, end: end, sgr: sgr, kind: kind, text: text})
		i = end
	}
	return atoms
}

// linkGroup is one logical link: the label atom(s) and the href atom(s) that
// follow them, with the byte range covering the whole group (so the gap can
// be dropped when the link is rewired).
type linkGroup struct {
	labels     []linkAtom
	hrefs      []linkAtom
	start, end int // from the first label atom to the last href atom
	hasLabel   bool
}

// groupLinkAtoms merges consecutive label atoms + following href atoms into
// logical links. Only whitespace/newlines/styling may separate the label from
// its href; any other visible text ends the group. A run of href atoms with
// no preceding label is one autolink (glamour splits a wrapped URL into
// several fragments and emits empty placeholder atoms around the break).
func groupLinkAtoms(s string, atoms []linkAtom) []linkGroup {
	var groups []linkGroup
	i := 0
	for i < len(atoms) {
		a := atoms[i]
		if a.kind == 'h' {
			// href run (autolink or wrap-split fragments): accumulate while
			// the gap stays whitespace/styling
			g := linkGroup{start: a.start}
			for i < len(atoms) && atoms[i].kind == 'h' &&
				(len(g.hrefs) == 0 || gapOK(s[g.hrefs[len(g.hrefs)-1].end:atoms[i].start])) {
				g.hrefs = append(g.hrefs, atoms[i])
				g.end = atoms[i].end
				i++
			}
			groups = append(groups, g)
			continue
		}
		// label run
		g := linkGroup{hasLabel: true, start: a.start}
		for i < len(atoms) && atoms[i].kind == 't' {
			g.labels = append(g.labels, atoms[i])
			i++
		}
		// href run after the label: same whitespace/styling gap rule
		prevEnd := g.labels[len(g.labels)-1].end
		for i < len(atoms) && atoms[i].kind == 'h' && gapOK(s[prevEnd:atoms[i].start]) {
			g.hrefs = append(g.hrefs, atoms[i])
			prevEnd = atoms[i].end
			i++
		}
		if len(g.hrefs) > 0 {
			g.end = g.hrefs[len(g.hrefs)-1].end
		} else {
			g.end = g.labels[len(g.labels)-1].end
		}
		groups = append(groups, g)
	}
	return groups
}

// gapOK reports whether the bytes between two atoms are only whitespace,
// newlines, and SGR spans — the gap glamour leaves inside one link.
func gapOK(gap string) bool {
	for i := 0; i < len(gap); {
		c := gap[i]
		if c == ' ' || c == '\n' || c == '\t' {
			i++
			continue
		}
		if c == 0x1b {
			// skip one SGR span
			j := i + 1
			for j < len(gap) && gap[j] != 'm' {
				j++
			}
			if j >= len(gap) {
				return false
			}
			i = j + 1
			continue
		}
		return false // visible text in the gap
	}
	return true
}

// hyperlinkGlamourLinks rewrites glamour's rendered links into OSC 8
// hyperlinks. A [label](href) group collapses to the clickable label (the
// href stops printing); a standalone href (autolink) becomes clickable in
// place. Hrefs that don't map to a clickable target (anchors, missing files)
// keep glamour's plain output.
func hyperlinkGlamourLinks(s string, exists func(string) bool) string {
	return hyperlinkGlamourLinksWith(s, exists, absFileURI)
}

func hyperlinkGlamourLinksWith(s string, exists func(string) bool, uri func(string, string) string) string {
	s = stripOSC8(bareSGR.Replace(s)) // accept raw glamour v2 output as well as renderMarkdownAt's
	atoms := parseLinkAtoms(s)
	if len(atoms) == 0 {
		return s
	}
	groups := groupLinkAtoms(s, atoms)

	// Decide each group's replacement and target before splicing.
	type repl struct {
		start, end int
		out        string
	}
	var repls []repl
	for _, g := range groups {
		if len(g.hrefs) == 0 {
			continue // label with no href (anchor-only link): glamour already
			// prints just the label; nothing to rewire
		}
		var sb strings.Builder
		for _, h := range g.hrefs {
			sb.WriteString(h.text)
		}
		hrefText := sb.String()
		target := targetURIWith(hrefText, exists, uri)
		if target == "" {
			continue // leave glamour's output untouched
		}
		if !g.hasLabel {
			// autolink: every fragment stays visible, wrapped in one target.
			// Glamour may have split the URL across lines; rejoin it so the
			// click target is the whole URL, not a fragment.
			var out strings.Builder
			for _, h := range g.hrefs {
				out.WriteString(hyperlink(target, h.sgr+h.text+sgrReset))
			}
			repls = append(repls, repl{g.start, g.end, out.String()})
			continue
		}
		// label+href: clickable label, href and gap dropped
		var label strings.Builder
		for _, l := range g.labels {
			label.WriteString(l.sgr + l.text + sgrReset)
		}
		repls = append(repls, repl{g.start, g.end, hyperlink(target, label.String())})
	}

	// Splice replacements back, copying untouched regions verbatim.
	var b strings.Builder
	b.Grow(len(s) + len(s)/4)
	last := 0
	for _, r := range repls {
		b.WriteString(s[last:r.start])
		b.WriteString(r.out)
		last = r.end
	}
	b.WriteString(s[last:])
	return b.String()
}

// linkAtomAt reports whether s starts with a link SGR span, returning the
// span and 't' (LinkText/label) or 'h' (Link/href).
func linkAtomAt(s string) (string, byte) {
	label, href := linkSGRs()
	for _, cand := range []struct {
		sgr  string
		kind byte
	}{
		{label, 't'},
		{href, 'h'},
		{linkTextSGRDark, 't'},
		{linkTextSGRLight, 't'},
		{linkSGRDark, 'h'},
		{linkSGRLight, 'h'},
	} {
		if cand.sgr == "" {
			continue
		}
		if strings.HasPrefix(s, cand.sgr) {
			return cand.sgr, cand.kind
		}
	}
	return "", 0
}

// scanAtom returns the end offset (exclusive) and visible text of the atom
// opened at s[start] with the given SGR span: a newline inside the atom ends
// the visible text (glamour's word wrap), and the atom closes at the first
// following reset.
func scanAtom(s string, start int, sgr string) (end int, text string) {
	body := start + len(sgr)
	if body > len(s) {
		return -1, ""
	}
	rest := s[body:]
	nl := strings.IndexByte(rest, '\n')
	rs := strings.Index(rest, sgrReset)
	if rs < 0 {
		return -1, ""
	}
	if nl >= 0 && nl < rs {
		return body + rs + len(sgrReset), rest[:nl]
	}
	return body + rs + len(sgrReset), rest[:rs]
}

// linkifyRenderedFilePaths is linkifyFilePaths for glamour's output: the
// renderer splits text into word atoms separated by SGR sequences, so the
// pre-render injection would wrap mid-sequence. Runs on the rendered string
// where every file ref appears contiguous inside one word atom. The same
// preceding-byte skips apply (ESC from styling, hrefs already handled by
// hyperlinkGlamourLinks).
func linkifyRenderedFilePaths(s string, exists func(string) bool) string {
	return linkifyRenderedFilePathsWith(s, exists, absFileURI)
}

func linkifyRenderedFilePathsWith(s string, exists func(string) bool, uri func(string, string) string) string {
	return replaceMatches(s, fileRefRE, func(m string, before byte) string {
		if before == 0x1b || strings.ContainsRune("([]/:;\"`m", rune(before)) {
			// ESC or 'm': inside an SGR/OSC 8 sequence (an atom's text starts
			// right after its span's closing 'm'). Others: markdown internals,
			// code spans, quotes.
			return m
		}
		path, line := splitLineRef(m)
		if !exists(path) {
			return m
		}
		return hyperlink(uri(path, line), m)
	})
}

// targetURI maps a link destination to a clickable URI: absolute URLs pass
// through; existing local files become file://; anything else (anchors,
// missing files) returns "" so the caller keeps the unlinked rendering.
func targetURI(dest string, exists func(string) bool) string {
	return targetURIWith(dest, exists, absFileURI)
}

func targetURIWith(dest string, exists func(string) bool, uri func(string, string) string) string {
	low := strings.ToLower(dest)
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") ||
		strings.HasPrefix(low, "mailto:") || strings.HasPrefix(low, "file://") {
		return dest
	}
	if strings.HasPrefix(dest, "#") {
		return ""
	}
	path, line := splitLineRef(dest)
	// Resolve in order: as written (absolute or CWD-relative), then — for a
	// leading "/" — glamour's normalization of "./x" as CWD-relative. The
	// model writes "./docs/features.md"; glamour renders the href "/docs/…",
	// so the dot-relative reading is what makes those clickable.
	candidates := []string{path}
	if strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") {
		candidates = append(candidates, "."+path)
	}
	for _, c := range candidates {
		if exists(c) {
			return uri(c, line)
		}
	}
	return ""
}
