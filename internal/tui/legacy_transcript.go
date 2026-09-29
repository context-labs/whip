package tui

import "sort"

// contentRow returns the styled transcript row y: blank pad rows first (the
// transcript is bottom-anchored), then each block's cached rows with one
// blank separator row between blocks.
func (m *model) contentRow(y int) string {
	y -= m.contentPad()
	b := m.blockAt(y)
	if b == nil || y-b.y0 >= len(b.rows) {
		return ""
	}
	return b.rows[y-b.y0]
}

// blockAt finds the block whose rows cover content row y (nil on a separator
// row or outside the content).
func (m *model) blockAt(y int) *block {
	if y < 0 || len(m.blocks) == 0 {
		return nil
	}
	i := sort.Search(len(m.blocks), func(i int) bool { return m.blocks[i].y1 >= y })
	if i == len(m.blocks) || m.blocks[i].y0 > y {
		return nil
	}
	return &m.blocks[i]
}
