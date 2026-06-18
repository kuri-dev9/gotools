package tui

import (
	"fmt"
	"regexp"
)

type Align int

const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
)

type Table struct {
	Headers        []string
	Widths         []int
	FixedWidth     []int
	Aligns         []Align
	HeaderAligns   []Align

	Separators     map[int]bool
	Indents        map[int]int
	SeparatorColor string
	HeaderColor    string
}

func NewTable(headers []string) *Table {
	n := len(headers)

	t := &Table{
		Headers:      headers,
		Widths:       make([]int, n),
		FixedWidth:   make([]int, n),
		Aligns:       make([]Align, n),
		HeaderAligns: make([]Align, n),
		Separators:   make(map[int]bool),
		Indents:      make(map[int]int),
	}

	for i := 0; i < n; i++ {
		t.Aligns[i] = AlignLeft
		t.HeaderAligns[i] = AlignLeft
	}

	return t
}

func (t *Table) SetAlign(col int, align Align) {
	t.Aligns[col] = align
}

func (t *Table) SetHeaderAlign(col int, align Align) {
	t.HeaderAligns[col] = align
}

func (t *Table) SetWidth(col int, width int) {
	t.FixedWidth[col] = width
}

func (t *Table) SetIndent(col int, indent int) {
	t.Indents[col] = indent
}

func (t *Table) SetSeparator(col int, enable bool) {
	t.Separators[col] = enable
}

var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func visibleLen(s string) int {
	return len(ansiRegexp.ReplaceAllString(s, ""))
}

func (t *Table) calcWidths() {
	for i := range t.Widths {
		if t.FixedWidth[i] > 0 {
			t.Widths[i] = t.FixedWidth[i]
			continue
		}
		t.Widths[i] = visibleLen(t.Headers[i])
	}
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%*s", n, "")
}

func truncateString(s string, width int) string {
	raw := ansiRegexp.ReplaceAllString(s, "")
	runes := []rune(raw)

	if len(runes) <= width {
		return s
	}
	return string(runes[:width-2]) + ".."
}

func (t *Table) renderCell(colIdx int, s string, width int, align Align) string {
	if visibleLen(s) > width {
		return truncateString(s, width)
	}

	l := visibleLen(s)
	pad := width - l

	indent := t.Indents[colIdx]
	if indent > pad {
		indent = pad
	}

	switch align {
	case AlignRight:
		return spaces(pad-indent) + s + spaces(indent)
	case AlignCenter:
		left := pad / 2
		right := pad - left
		return spaces(left) + s + spaces(right)
	default:
		return spaces(indent) + s + spaces(pad-indent)
	}
}

func (t *Table) EnsureWidths() {
	t.calcWidths()
}

func (t *Table) PrintHeader() {
	for i, h := range t.Headers {
		if t.HeaderColor != "" {
			h = t.HeaderColor + h + Reset
		}

		fmt.Print(t.renderCell(i, h, t.Widths[i], t.HeaderAligns[i]))
		if t.Separators[i] {
			if t.SeparatorColor != "" {
				fmt.Print(t.SeparatorColor + "|" + Reset)
			} else {
				fmt.Print("|")
			}
		} else {
			fmt.Print(" ")
		}
	}
	fmt.Println()
}

func (t *Table) PrintHeaderIfNeeded(lineCount int, interval int) {
	if lineCount == 0 || lineCount%interval == 0 {
		t.PrintHeader()
	}
}

func (t *Table) PrintRow(row []string) {
	for i := range t.Headers {
		col := ""
		if i < len(row) {
			col = row[i]
		}
		fmt.Print(t.renderCell(i, col, t.Widths[i], t.Aligns[i]))
		if t.Separators[i] {
			if t.SeparatorColor != "" {
				fmt.Print(t.SeparatorColor + "|" + Reset)
			} else {
				fmt.Print("|")
			}
		} else {
			fmt.Print(" ")
		}
	}
	fmt.Println()
}