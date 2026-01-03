package utils

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// SplitLines takes a multiline string and splits it on newlines
// currently we are also stripping \r's which may have adverse effects for
// windows users (but no issues have been raised yet)
func SplitLines(multilineString string) []string {
	multilineString = strings.ReplaceAll(multilineString, "\r", "")
	if multilineString == "" || multilineString == "\n" {
		return make([]string, 0)
	}
	lines := strings.Split(multilineString, "\n")
	if lines[len(lines)-1] == "" {
		return lines[:len(lines)-1]
	}
	return lines
}

func SplitNul(str string) []string {
	if str == "" {
		return make([]string, 0)
	}
	str = strings.TrimSuffix(str, "\x00")
	return strings.Split(str, "\x00")
}

// NormalizeLinefeeds - Removes all Windows and Mac style line feeds
func NormalizeLinefeeds(str string) string {
	str = strings.ReplaceAll(str, "\r\n", "\n")
	str = strings.ReplaceAll(str, "\r", "")
	return str
}

// EscapeSpecialChars - Replaces all special chars like \n with \\n
func EscapeSpecialChars(str string) string {
	return strings.NewReplacer(
		"\n", "\\n",
		"\r", "\\r",
		"\t", "\\t",
		"\b", "\\b",
		"\f", "\\f",
		"\v", "\\v",
	).Replace(str)
}

func dropCR(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\r' {
		return data[0 : len(data)-1]
	}
	return data
}

// ScanLinesAndTruncateWhenLongerThanBuffer returns a split function that can be
// used with bufio.Scanner.Split(). It is very similar to bufio.ScanLines,
// except that it will truncate lines that are longer than the scanner's read
// buffer (whereas bufio.ScanLines will return an error in that case, which is
// often difficult to handle).
//
// If you are using your own buffer for the scanner, you must set maxBufferSize
// to the same value as the max parameter that you passed to scanner.Buffer().
// Otherwise, maxBufferSize must be set to bufio.MaxScanTokenSize.
func ScanLinesAndTruncateWhenLongerThanBuffer(maxBufferSize int) func(data []byte, atEOF bool) (int, []byte, error) {
	skipOverRemainderOfLongLine := false

	return func(data []byte, atEOF bool) (int, []byte, error) {
		if atEOF && len(data) == 0 {
			// Done
			return 0, nil, nil
		}
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			if skipOverRemainderOfLongLine {
				skipOverRemainderOfLongLine = false
				return i + 1, nil, nil
			}
			return i + 1, dropCR(data[0:i]), nil
		}
		if atEOF {
			if skipOverRemainderOfLongLine {
				return len(data), nil, nil
			}

			return len(data), dropCR(data), nil
		}

		// Buffer is full, so we can't get more data
		if len(data) >= maxBufferSize {
			if skipOverRemainderOfLongLine {
				return len(data), nil, nil
			}

			skipOverRemainderOfLongLine = true
			return len(data), data, nil
		}

		// Request more data.
		return 0, nil, nil
	}
}

// Wrap lines to a given width, and return:
// - the wrapped lines
// - the line indices of the wrapped lines, indexed by the original line indices
// - the line indices of the original lines, indexed by the wrapped line indices
// If wrap is false, the text is returned as is.
// This code needs to behave the same as `gocui.lineWrap` does.
// It handles ANSI escape sequences by not counting them toward the display width.
func WrapViewLinesToWidth(wrap bool, editable bool, text string, width int, tabWidth int) ([]string, []int, []int) {
	if !editable {
		text = strings.TrimSuffix(text, "\n")
	}
	lines := strings.Split(text, "\n")
	if !wrap {
		indices := make([]int, len(lines))
		for i := range lines {
			indices[i] = i
		}
		return lines, indices, indices
	}

	wrappedLines := make([]string, 0, len(lines))
	wrappedLineIndices := make([]int, 0, len(lines))
	originalLineIndices := make([]int, 0, len(lines))

	if tabWidth < 1 {
		tabWidth = 4
	}

	for originalLineIdx, line := range lines {
		wrappedLineIndices = append(wrappedLineIndices, len(wrappedLines))

		// Parse the line into cells (visible characters with their positions)
		cells := parseLineCells(line, tabWidth)

		appendWrappedLine := func(startIdx, endIdx int) {
			// Build the wrapped line from cells
			var sb strings.Builder
			for i := startIdx; i < endIdx; i++ {
				sb.WriteString(cells[i].text)
			}
			wrappedLines = append(wrappedLines, sb.String())
			originalLineIndices = append(originalLineIndices, originalLineIdx)
		}

		// Wrap using the same algorithm as gocui.lineWrap but with cells
		n := 0
		offset := 0
		lastWhitespaceIndex := -1

		for i := 0; i < len(cells); i++ {
			cell := cells[i]
			if !cell.visible {
				// ANSI sequences don't contribute to width
				continue
			}

			rw := cell.width
			n += rw

			if n > width {
				currChr := cell.chr
				if currChr == " " {
					// Break at space, omit the space
					appendWrappedLine(offset, i)
					offset = i + 1
					n = 0
				} else if currChr == "-" {
					// Break before hyphen
					appendWrappedLine(offset, i)
					offset = i
					n = rw
				} else if lastWhitespaceIndex != -1 {
					// Break at last whitespace
					if cells[lastWhitespaceIndex].chr == "-" {
						// Keep the hyphen
						appendWrappedLine(offset, lastWhitespaceIndex+1)
					} else {
						// Omit the space
						appendWrappedLine(offset, lastWhitespaceIndex)
					}
					offset = lastWhitespaceIndex + 1
					// Recalculate n from offset to current position (inclusive)
					n = 0
					for j := offset; j <= i; j++ {
						if cells[j].visible {
							n += cells[j].width
						}
					}
				} else {
					// Break mid-word
					appendWrappedLine(offset, i)
					offset = i
					n = rw
				}
				lastWhitespaceIndex = -1
			} else if cell.chr == " " || cell.chr == "-" {
				lastWhitespaceIndex = i
			}
		}

		// Append remaining content
		appendWrappedLine(offset, len(cells))
	}

	return wrappedLines, wrappedLineIndices, originalLineIndices
}

// lineCell represents either a visible character or an ANSI escape sequence
type lineCell struct {
	text    string // the actual text (character or escape sequence)
	chr     string // for visible cells, the character; empty for ANSI sequences
	width   int    // display width (0 for ANSI sequences)
	visible bool   // false for ANSI sequences
}

// parseLineCells splits a line into cells, where each cell is either
// a visible character or an ANSI escape sequence.
// Tabs are expanded to spaces.
func parseLineCells(line string, tabWidth int) []lineCell {
	var cells []lineCell
	displayCol := 0 // track display column for tab expansion

	i := 0
	for i < len(line) {
		if line[i] == '\x1b' && i+1 < len(line) && line[i+1] == '[' {
			// Found start of CSI escape sequence
			j := i + 2 // skip ESC [
			for j < len(line) && ((line[j] >= '0' && line[j] <= '9') || line[j] == ';') {
				j++
			}
			if j < len(line) {
				j++ // include the terminating character (e.g., 'm', 'K')
			}

			cells = append(cells, lineCell{
				text:    line[i:j],
				visible: false,
				width:   0,
			})
			i = j
		} else if line[i] == '\x1b' && i+1 < len(line) && line[i+1] == ']' {
			// Found start of OSC escape sequence (e.g., hyperlinks)
			j := i + 2
			for j < len(line) {
				if line[j] == 0x07 {
					j++
					break
				}
				if line[j] == '\x1b' && j+1 < len(line) && line[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}

			cells = append(cells, lineCell{
				text:    line[i:j],
				visible: false,
				width:   0,
			})
			i = j
		} else if line[i] == '\t' {
			// Expand tab to spaces
			numSpaces := tabWidth - (displayCol % tabWidth)
			for s := 0; s < numSpaces; s++ {
				cells = append(cells, lineCell{
					text:    " ",
					chr:     " ",
					width:   1,
					visible: true,
				})
			}
			displayCol += numSpaces
			i++
		} else {
			// Regular character - need to handle multi-byte UTF-8
			// Use range to get proper rune boundaries
			r, size := rune(line[i]), 1
			for j := i; j < len(line); {
				r, size = utf8.DecodeRuneInString(line[j:])
				break
			}
			chr := string(r)
			w := uniseg.StringWidth(chr)
			cells = append(cells, lineCell{
				text:    line[i : i+size],
				chr:     chr,
				width:   w,
				visible: true,
			})
			displayCol += w
			i += size
		}
	}

	return cells
}
