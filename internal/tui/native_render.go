package tui

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/protocol"
)

const (
	nativeRenderInput = 64 << 10
	nativeRenderBody  = 256 << 10
	nativeRenderBytes = 16 << 20
	nativeRenderRows  = 65536
)

type nativeRenderCache struct {
	width, theme, bytes int
	expanded            bool
	blocks              map[protocol.ID][]string
	blockExpanded       map[protocol.ID]bool
}

func (c *nativeRenderCache) prepare(messages []protocol.Message, width int, expanded bool) {
	theme := themeGeneration()
	if c.width != width || c.theme != theme || c.expanded != expanded || c.blocks == nil {
		*c = nativeRenderCache{width: width, theme: theme, expanded: expanded, blocks: make(map[protocol.ID][]string), blockExpanded: make(map[protocol.ID]bool)}
	}
	keep := make(map[protocol.ID]bool, len(messages))
	for _, message := range messages {
		keep[message.ID] = true
	}
	for id, rows := range c.blocks {
		if !keep[id] {
			c.bytes -= nativeRowBytes(rows)
			delete(c.blocks, id)
			delete(c.blockExpanded, id)
		}
	}
}

func (c *nativeRenderCache) message(message protocol.Message) []string {
	return c.messageExpanded(message, c.expanded)
}

func (c *nativeRenderCache) messageExpanded(message protocol.Message, expanded bool) []string {
	if rows, ok := c.blocks[message.ID]; ok {
		if c.blockExpanded[message.ID] == expanded {
			return rows
		}
		c.bytes -= nativeRowBytes(rows)
		delete(c.blocks, message.ID)
		delete(c.blockExpanded, message.ID)
	}
	rows := renderNativeMessage(message, c.width, expanded)
	if size := nativeRowBytes(rows); c.bytes+size <= nativeRenderBytes {
		c.blocks[message.ID] = rows
		c.blockExpanded[message.ID] = expanded
		c.bytes += size
	}
	return rows
}

func nativeRowBytes(rows []string) int {
	size := 0
	for _, row := range rows {
		size += len(row) + 1
	}
	return size
}

// Only presentation is decoded here. Tool results never supply identity,
// authority, attachment ownership, or execution state to this renderer.
func renderNativeMessage(message protocol.Message, width int, expanded bool) []string {
	label := fmt.Sprintf("%s · #%d", message.Role, message.Sequence)
	if message.Source != nil {
		label += " · imported"
	}
	rows := []string{nativeDisplayText(label)}
	remaining := nativeRenderInput
	for i, part := range message.Parts {
		var text string
		plain, collapse := false, false
		switch part.Type {
		case "text":
			text = part.Text
		case "content":
			text, plain = "[attachment "+string(part.ReferenceID)+"]", true
		case "tool_call":
			if part.Call == nil {
				continue
			}
			rows = append(rows, nativeDisplayText(part.Call.Name+" · "+string(part.Call.ID)))
			text, plain, collapse = string(part.Call.Arguments), true, !expanded
			var args struct {
				Code *string `json:"code"`
			}
			if part.Call.Name == "execute" && len(text) <= nativeRenderInput && json.Unmarshal(part.Call.Arguments, &args) == nil && args.Code != nil {
				text = *args.Code
			}
		case "tool_result":
			if part.Result == nil {
				continue
			}
			kind := "result"
			if part.Result.IsError {
				kind = "failed result"
			}
			rows = append(rows, nativeDisplayText(kind+" · "+string(part.Result.CallID)))
			text, plain, collapse = nativeResultText(part.Result.Output), true, !expanded
		}
		bounded, cut := nativeTextPrefix(text, remaining)
		remaining -= len(bounded)
		if plain {
			rows = append(rows, nativePlainRows(bounded, width, collapse)...)
		} else {
			rows = append(rows, strings.Split(nativeMarkdown(bounded, width), "\n")...)
		}
		if cut {
			rows = append(rows, "Display text limited to 64 KiB per message; the complete body remains in host history.")
		}
		if remaining == 0 {
			if i < len(message.Parts)-1 {
				rows = append(rows, "Additional parts are outside this display limit; the complete message remains in host history.")
			}
			break
		}
		if len(rows) >= 4096 || nativeRowBytes(rows) >= nativeRenderBody {
			rows = append(rows, "Display limit reached; complete message bodies remain in host history.")
			break
		}
	}
	return boundNativeRows(rows, nativeRenderBody, 4096)
}

func nativeTextPrefix(text string, size int) (string, bool) {
	if len(text) <= size {
		return text, false
	}
	for size > 0 && !utf8.RuneStart(text[size]) {
		size--
	}
	return text[:size], true
}

func nativePlainRows(text string, width int, collapse bool) []string {
	rows := strings.Split(ansi.Hardwrap(nativeDisplayText(text), max(width, 1), true), "\n")
	if collapse && len(rows) > 8 {
		rows = append(rows[:8], "… /tools expand shows more of this output.")
	}
	return rows
}

func nativeMarkdown(text string, width int) string {
	text = nativeDisplayText(text)
	// Markdown's parser and syntax highlighter are deliberately kept off very
	// large bodies. The bounded text remains readable without blocking the
	// terminal on a large generated table, fence, or single paragraph.
	if len(text) > 16<<10 {
		return ansi.Hardwrap(text, max(width, 1), true) + "\nLong message shown as plain text."
	}
	renderer := mdRenderer(max(width, 8))
	if renderer == nil {
		return ansi.Hardwrap(text, max(width, 1), true)
	}
	value, err := renderer.Render(text)
	if err != nil {
		return ansi.Hardwrap(text, max(width, 1), true)
	}
	// Host paths never become client-local file links. In particular, do not
	// call renderMarkdownAt: it probes the client filesystem for path labels.
	return wrapWideLines(hyperlinkGlamourLinksTo(stripLinePadding(strings.Trim(value, "\n")), nativeWebLink), max(width, 1))
}

// Only explicit web URLs become terminal hyperlinks. The host's directory is
// not proof of a client-local path; raw terminal escape sequences are removed
// before rendering, and renderer-provided hyperlinks are rebuilt by policy.
func nativeWebLink(value string) string {
	if strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Opaque != "" || parsed.User != nil || parsed.Hostname() == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.String()
}

func nativeResultText(output string) string {
	if len(output) > nativeRenderInput {
		return output
	}
	var value struct {
		Result *struct {
			FormatVersion int             `json:"format_version"`
			Output        string          `json:"output"`
			HasValue      bool            `json:"has_value"`
			Value         json.RawMessage `json:"value"`
		} `json:"result"`
		Error *string `json:"error"`
	}
	if json.Unmarshal([]byte(output), &value) != nil || value.Result == nil || value.Result.FormatVersion <= 0 {
		return output
	}
	var parts []string
	if value.Result.Output != "" {
		parts = append(parts, value.Result.Output)
	}
	if value.Result.HasValue {
		parts = append(parts, "Value: "+string(value.Result.Value))
	}
	if value.Error != nil {
		parts = append(parts, "Error: "+*value.Error)
	}
	if len(parts) == 0 {
		return "Completed without stdout or a displayed value."
	}
	return strings.Join(parts, "\n")
}

func boundNativeRows(rows []string, bytes, count int) []string {
	size := 0
	for i, row := range rows {
		size += len(row) + 1
		if i >= count || size > bytes {
			return append(rows[:i], "Display limit reached; complete message bodies remain in host history.")
		}
	}
	return rows
}
