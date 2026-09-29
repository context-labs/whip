package tui

import (
	"strings"
	"time"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// selPos is one endpoint of a transcript drag, in rendered display cells.
type selPos struct {
	row, col int // col is a display cell (wide runes count 2)
}

// selection is the in-flight (dragging) or last-completed selection. It
// survives release so the highlight stays on screen until any keypress or a
// new press clears it.
type selection struct {
	anchor, cur selPos
	done        bool // button released; highlight stays until cleared
}

// selOrder returns the selection endpoints top-to-bottom.
func selOrder(s selection) (lo, hi selPos) {
	lo, hi = s.anchor, s.cur
	if lo.row > hi.row || (lo.row == hi.row && lo.col > hi.col) {
		lo, hi = hi, lo
	}
	return lo, hi
}

// selCols is the [start, end) cell range selected on row r.
func selCols(lo, hi selPos, r, lineWidth int) (int, int) {
	start, end := 0, lineWidth
	if r == lo.row {
		start = lo.col
	}
	if r == hi.row {
		end = hi.col
	}
	return start, max(end, start)
}

// cellSlice returns the cells [off, off+n) of s (already ANSI-stripped).
func cellSlice(s string, off, n int) string {
	var b strings.Builder
	col := 0
	for _, r := range s {
		w := runewidth.RuneWidth(r)
		if col+w > off && col < off+n {
			b.WriteRune(r)
		}
		col += w
		if col >= off+n {
			break
		}
	}
	return b.String()
}

// clickMark remembers the last press for multi-click detection.
type clickMark struct {
	at   time.Time
	x, y int
	n    int
}

// multiClickWindow is how quickly presses on the same cell chain into a
// double or triple click.
const multiClickWindow = 400 * time.Millisecond

// wordBounds returns the [start, end) cell range of the word under cell col:
// a run of non-space cells.
func wordBounds(line string, col int) (int, int) {
	cells := make([]int, 0, len(line)) // start cell of every rune
	widths := make([]int, 0, len(line))
	c := 0
	for _, r := range line {
		cells = append(cells, c)
		w := runewidth.RuneWidth(r)
		widths = append(widths, w)
		c += w
	}
	runes := []rune(line)
	i := 0
	for i < len(runes) && cells[i]+widths[i] <= col {
		i++
	}
	if i >= len(runes) || unicode.IsSpace(runes[i]) {
		return 0, 0
	}
	lo, hi := i, i
	for lo > 0 && !unicode.IsSpace(runes[lo-1]) {
		lo--
	}
	for hi+1 < len(runes) && !unicode.IsSpace(runes[hi+1]) {
		hi++
	}
	return cells[lo], cells[hi] + widths[hi]
}
