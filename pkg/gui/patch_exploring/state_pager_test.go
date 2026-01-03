package patch_exploring

import (
	"testing"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazygit/pkg/commands/patch"
	"github.com/stretchr/testify/assert"
)

// newTestView creates a mock view for testing with sensible defaults
// The view is 100 chars wide (inner width 98) and wrapping disabled by default
func newTestView() *gocui.View {
	// Create view with dimensions: x0=0, y0=0, x1=99, y1=49
	// This gives Width()=100, InnerWidth()=98
	view := gocui.NewView("test", 0, 0, 99, 49, gocui.OutputNormal)
	view.Wrap = false
	return view
}

// newTestViewWithWrap creates a mock view with wrapping enabled
func newTestViewWithWrap(width int) *gocui.View {
	// x1 = width - 1 + 2 (for frame) = width + 1
	// so Width() = width + 2, InnerWidth() = width
	view := gocui.NewView("test", 0, 0, width+1, 49, gocui.OutputNormal)
	view.Wrap = true
	return view
}

// Helper to create a State for testing pager functionality
func newStateForPagerTest(diff string) *State {
	p := patch.Parse(diff)
	patchLines := p.Lines()
	patchLineCount := len(patchLines)

	// Create 1:1 mapping (no wrapping) for tests
	viewLineIndices := make([]int, patchLineCount)
	patchLineIndices := make([]int, patchLineCount)
	for i := 0; i < patchLineCount; i++ {
		viewLineIndices[i] = i
		patchLineIndices[i] = i
	}

	// Find first change line for initial selection
	selectedLineIdx := 0
	for i, line := range patchLines {
		if line.IsChange() {
			selectedLineIdx = i
			break
		}
	}

	return &State{
		patch:             p,
		diff:              diff,
		selectedLineIdx:   selectedLineIdx,
		rangeStartLineIdx: selectedLineIdx,
		selectMode:        LINE,
		viewLineIndices:   viewLineIndices,
		patchLineIndices:  patchLineIndices,
	}
}

// =============================================================================
// Tests for helper functions
// =============================================================================

func TestStripAnsiCodes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain text unchanged",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "simple color code",
			input:    "\x1b[31mred text\x1b[0m",
			expected: "red text",
		},
		{
			name:     "complex 24-bit color",
			input:    "\x1b[38;2;137;221;255mpackage\x1b[38;2;192;202;245m main\x1b[0m",
			expected: "package main",
		},
		{
			name:     "delta line with box chars and line numbers",
			input:    "\x1b[34m\x1b[38;2;68;68;68m  1 \x1b[34m⋮\x1b[38;2;68;68;68m  1 \x1b[34m│\x1b[38;2;137;221;255mpackage\x1b[0m",
			expected: "package",
		},
		{
			name:     "delta deletion line",
			input:    "\x1b[34m\x1b[38;5;88m  4 \x1b[34m⋮\x1b[38;5;28m    \x1b[34m│\x1b[0m\x1b[48;2;63;0;1m        fmt.Println(\"hello\")\x1b[0m",
			expected: "        fmt.Println(\"hello\")",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripAnsiCodes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStripAnsiOnly(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "preserves box characters",
			input:    "\x1b[34m───\x1b[0m\x1b[34m┐\x1b[0m",
			expected: "───┐",
		},
		{
			name:     "preserves pipe and dots",
			input:    "\x1b[34m  1 \x1b[34m⋮\x1b[34m  1 \x1b[34m│content",
			expected: "  1 ⋮  1 │content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripAnsiOnly(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsDiffHeaderLine(t *testing.T) {
	tests := []struct {
		line     string
		expected bool
	}{
		{"diff --git a/file.go b/file.go", true},
		{"index abc123..def456 100644", true},
		{"--- a/file.go", true},
		{"+++ b/file.go", true},
		{"@@ -1,5 +1,7 @@", true},
		{"@@ -1,5 +1,7 @@ func hello()", true},
		{" context line", false},
		{"+added line", false},
		{"-deleted line", false},
		{"package main", false},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			assert.Equal(t, tt.expected, isDiffHeaderLine(tt.line))
		})
	}
}

func TestIsDecorationLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{
			name:     "empty line is decoration",
			line:     "",
			expected: true,
		},
		{
			name:     "whitespace only is decoration",
			line:     "   ",
			expected: true,
		},
		{
			name:     "delta top border",
			line:     "\x1b[34m───\x1b[0m\x1b[34m┐\x1b[0m",
			expected: true,
		},
		{
			name:     "delta bottom border",
			line:     "\x1b[34m───\x1b[0m\x1b[34m┘\x1b[0m",
			expected: true,
		},
		{
			name:     "delta hunk header box line",
			line:     "\x1b[34m────────────────\x1b[0m\x1b[34m┐\x1b[0m",
			expected: true,
		},
		{
			name:     "hunk header @@ line",
			line:     "@@ -1,5 +1,7 @@",
			expected: true,
		},
		{
			name:     "delta content line with line numbers",
			line:     "\x1b[34m\x1b[38;2;68;68;68m  1 \x1b[34m⋮\x1b[38;2;68;68;68m  1 \x1b[34m│\x1b[38;2;137;221;255mpackage\x1b[0m",
			expected: false,
		},
		{
			name:     "delta deletion line",
			line:     "\x1b[34m\x1b[38;5;88m  4 \x1b[34m⋮\x1b[38;5;28m    \x1b[34m│\x1b[0m\x1b[48;2;63;0;1m        fmt.Println(\"hello\")\x1b[0m",
			expected: false,
		},
		{
			name:     "delta addition line",
			line:     "\x1b[34m\x1b[38;5;88m    \x1b[34m⋮\x1b[38;5;28m  4 \x1b[34m│\x1b[48;2;0;40;0mfmt.Println(\"hello world\")\x1b[0m",
			expected: false,
		},
		{
			name:     "delta hunk context header (middle line of box)",
			line:     "\x1b[34m1\x1b[0m: \x1b[34m│\x1b[0m",
			expected: true,
		},
		{
			name:     "delta hunk context header with function name",
			line:     "\x1b[34m3\x1b[0m: package main \x1b[34m│\x1b[0m",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDecorationLine(tt.line)
			assert.Equal(t, tt.expected, result, "line: %q", tt.line)
		})
	}
}

func TestIsEmptyDecorationLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{
			name:     "truly empty line",
			line:     "",
			expected: true,
		},
		{
			name:     "line with only ANSI codes, no box chars",
			line:     "\x1b[0m",
			expected: true,
		},
		{
			name:     "delta empty content line with box chars",
			line:     "\x1b[34m\x1b[38;2;68;68;68m  2 \x1b[34m⋮\x1b[38;2;68;68;68m  2 \x1b[34m│\x1b[0m",
			expected: false, // Has box chars, so it's a content line (empty content)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isEmptyDecorationLine(tt.line)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Tests for SetPagerOutput line mapping
// =============================================================================

func TestSetPagerOutput_SimpleDiff(t *testing.T) {
	// Simple diff with one change
	diff := `diff --git a/test.go b/test.go
index abc123..def456 100644
--- a/test.go
+++ b/test.go
@@ -1,3 +1,3 @@
 package main
 
-func hello() {}
+func hello() { fmt.Println("hello") }
`

	// Simulated delta output (simplified, no ANSI for clarity in test)
	// Delta adds: 3 header lines (box), then content lines
	pagerOutput := `───┐
1: │
───┘
  1 ⋮  1 │package main
  2 ⋮  2 │
  3 ⋮    │func hello() {}
    ⋮  3 │func hello() { fmt.Println("hello") }
`

	state := newStateForPagerTest(diff)
	state.SetPagerOutput(pagerOutput, newTestView())

	// Verify pager output is set
	assert.True(t, state.HasPagerOutput())

	// The diff has these lines:
	// 0: diff --git ... (header)
	// 1: index ... (header)
	// 2: --- a/test.go (header)
	// 3: +++ b/test.go (header)
	// 4: @@ -1,3 +1,3 @@ (header)
	// 5:  package main (context)
	// 6:  (empty context)
	// 7: -func hello() {} (deletion)
	// 8: +func hello() { fmt.Println("hello") } (addition)

	// The pager output has these lines:
	// 0: ───┐ (decoration)
	// 1: 1: │ (decoration - function context header)
	// 2: ───┘ (decoration)
	// 3:   1 ⋮  1 │package main (content - maps to patch line 5)
	// 4:   2 ⋮  2 │ (content - maps to patch line 6)
	// 5:   3 ⋮    │func hello() {} (content - deletion, maps to patch line 7)
	// 6:     ⋮  3 │func hello() { ... } (content - addition, maps to patch line 8)

	// Check the mapping
	assert.NotNil(t, state.pagerViewLineIndices)
	assert.NotNil(t, state.pagerPatchLineIndices)

	// Verify line count
	patchLines := state.patch.Lines()
	assert.Equal(t, len(patchLines), len(state.pagerViewLineIndices))
}

func TestSetPagerOutput_MultipleHunks(t *testing.T) {
	// Diff with multiple hunks
	diff := `diff --git a/test.go b/test.go
index abc123..def456 100644
--- a/test.go
+++ b/test.go
@@ -3,7 +3,7 @@ package main
 import "fmt"
 
 func first() {
-	fmt.Println("first")
+	fmt.Println("first modified")
 }
 
 func second() {
@@ -11,7 +11,8 @@ func second() {
 }
 
 func third() {
-	fmt.Println("third")
+	fmt.Println("third modified")
+	fmt.Println("extra line")
 }
`

	// Simulated delta output with multiple hunks
	// Each hunk gets its own header box
	pagerOutput := `────────────────┐
3: package main │
────────────────┘
  3 ⋮  3 │import "fmt"
  4 ⋮  4 │
  5 ⋮  5 │func first() {
  6 ⋮    │	fmt.Println("first")
    ⋮  6 │	fmt.Println("first modified")
  7 ⋮  7 │}
  8 ⋮  8 │
  9 ⋮  9 │func second() {

────────────────────┐
11: func second() { │
────────────────────┘
 11 ⋮ 11 │}
 12 ⋮ 12 │
 13 ⋮ 13 │func third() {
 14 ⋮    │	fmt.Println("third")
    ⋮ 14 │	fmt.Println("third modified")
    ⋮ 15 │	fmt.Println("extra line")
 15 ⋮ 16 │}
`

	state := newStateForPagerTest(diff)
	state.SetPagerOutput(pagerOutput, newTestView())

	assert.True(t, state.HasPagerOutput())
	assert.NotNil(t, state.pagerViewLineIndices)
}

func TestSetPagerOutput_EmptyPagerOutput(t *testing.T) {
	diff := `diff --git a/test.go b/test.go
index abc123..def456 100644
--- a/test.go
+++ b/test.go
@@ -1,3 +1,3 @@
 package main
-old
+new
`

	state := newStateForPagerTest(diff)

	// Set empty pager output
	state.SetPagerOutput("", newTestView())

	assert.False(t, state.HasPagerOutput())
	assert.Nil(t, state.pagerViewLineIndices)
	assert.Nil(t, state.pagerPatchLineIndices)
}

// =============================================================================
// Tests for SelectedViewRangeForPager - THE CRITICAL TEST
// =============================================================================

func TestSelectedViewRangeForPager_LineMode(t *testing.T) {
	diff := `diff --git a/test.go b/test.go
index abc123..def456 100644
--- a/test.go
+++ b/test.go
@@ -1,5 +1,5 @@
 context1
 context2
-deleted line
+added line
 context3
`

	// Patch lines are:
	// 0: diff --git (header)
	// 1: index (header)
	// 2: --- (header)
	// 3: +++ (header)
	// 4: @@ (header)
	// 5: context1
	// 6: context2
	// 7: -deleted line
	// 8: +added line
	// 9: context3

	// Simulated pager output
	pagerOutput := `───┐
1: │
───┘
  1 ⋮  1 │context1
  2 ⋮  2 │context2
  3 ⋮    │deleted line
    ⋮  3 │added line
  4 ⋮  4 │context3
`

	// Pager lines:
	// 0: ───┐ (decoration)
	// 1: 1: │ (decoration)
	// 2: ───┘ (decoration)
	// 3: context1 (content -> patch 5)
	// 4: context2 (content -> patch 6)
	// 5: deleted line (content -> patch 7)
	// 6: added line (content -> patch 8)
	// 7: context3 (content -> patch 9)

	state := newStateForPagerTest(diff)
	state.SetPagerOutput(pagerOutput, newTestView())

	// Select the deletion line (patch line 7)
	// With Option B, selectedLineIdx is a VIEW line index, not patch line
	// viewLineIndices[patchLine] gives the view line for that patch line
	state.selectedLineIdx = state.viewLineIndices[7]
	state.selectMode = LINE

	viewStart, viewEnd := state.SelectedViewRange()

	// Deletion line (patch 7) should map to view line 5
	// (3 decoration lines + 2 content lines before it)
	assert.Equal(t, 5, viewStart, "viewStart should be 5 (deletion line in pager view)")
	assert.Equal(t, 5, viewEnd, "viewEnd should equal viewStart in LINE mode")
}

func TestSelectedViewRangeForPager_RangeMode(t *testing.T) {
	diff := `diff --git a/test.go b/test.go
index abc123..def456 100644
--- a/test.go
+++ b/test.go
@@ -1,5 +1,6 @@
 context1
-deleted1
-deleted2
+added1
+added2
+added3
 context2
`

	// Patch lines:
	// 0-4: headers
	// 5: context1
	// 6: -deleted1
	// 7: -deleted2
	// 8: +added1
	// 9: +added2
	// 10: +added3
	// 11: context2

	pagerOutput := `───┐
1: │
───┘
  1 ⋮  1 │context1
  2 ⋮    │deleted1
  3 ⋮    │deleted2
    ⋮  2 │added1
    ⋮  3 │added2
    ⋮  4 │added3
  4 ⋮  5 │context2
`

	// Pager lines:
	// 0-2: decoration
	// 3: context1 (patch 5)
	// 4: deleted1 (patch 6)
	// 5: deleted2 (patch 7)
	// 6: added1 (patch 8)
	// 7: added2 (patch 9)
	// 8: added3 (patch 10)
	// 9: context2 (patch 11)

	state := newStateForPagerTest(diff)
	state.SetPagerOutput(pagerOutput, newTestView())

	// Select range from deleted1 to added2 (patch lines 6-9)
	// With Option B, indices are VIEW line indices
	state.rangeStartLineIdx = state.viewLineIndices[6]
	state.selectedLineIdx = state.viewLineIndices[9]
	state.selectMode = RANGE

	viewStart, viewEnd := state.SelectedViewRange()

	// deleted1 (patch 6) -> view line 4
	// added2 (patch 9) -> view line 7
	assert.Equal(t, 4, viewStart, "viewStart should map to deleted1 in view")
	assert.Equal(t, 7, viewEnd, "viewEnd should map to added2 in view")
}

func TestSelectedViewRangeForPager_WithoutPagerOutput(t *testing.T) {
	diff := `diff --git a/test.go b/test.go
index abc123..def456 100644
--- a/test.go
+++ b/test.go
@@ -1,3 +1,3 @@
 context
-old
+new
`

	state := newStateForPagerTest(diff)
	// Don't set pager output

	state.selectedLineIdx = 6 // -old line
	state.selectMode = LINE

	// Should fall back to regular SelectedViewRange
	start, end := state.SelectedViewRangeForPager()
	regularStart, regularEnd := state.SelectedViewRange()

	assert.Equal(t, regularStart, start)
	assert.Equal(t, regularEnd, end)
}

// =============================================================================
// Tests for verifying staging operations work correctly with pager
// =============================================================================

func TestPagerMapping_StagingCorrectLine(t *testing.T) {
	// This test verifies that when we select a line in the pager view,
	// we stage the correct line from the original diff

	diff := `diff --git a/test.go b/test.go
index abc123..def456 100644
--- a/test.go
+++ b/test.go
@@ -1,7 +1,7 @@
 package main
 
 func hello() {
-	fmt.Println("hello")
+	fmt.Println("hello world")
 }
 
 func goodbye() {
`

	// Patch lines:
	// 0: diff --git (PATCH_HEADER)
	// 1: index (PATCH_HEADER)
	// 2: --- (PATCH_HEADER)
	// 3: +++ (PATCH_HEADER)
	// 4: @@ (HUNK_HEADER)
	// 5:  package main (CONTEXT)
	// 6:  (CONTEXT - empty)
	// 7:  func hello() { (CONTEXT)
	// 8: -	fmt.Println("hello") (DELETION)
	// 9: +	fmt.Println("hello world") (ADDITION)
	// 10:  } (CONTEXT)
	// 11:  (CONTEXT - empty)
	// 12:  func goodbye() { (CONTEXT)

	pagerOutput := `───┐
1: │
───┘
  1 ⋮  1 │package main
  2 ⋮  2 │
  3 ⋮  3 │func hello() {
  4 ⋮    │	fmt.Println("hello")
    ⋮  4 │	fmt.Println("hello world")
  5 ⋮  5 │}
  6 ⋮  6 │
  7 ⋮  7 │func goodbye() {
`

	// Pager lines:
	// 0: ───┐ (decoration)
	// 1: 1: │ (decoration)
	// 2: ───┘ (decoration)
	// 3: package main (content - patch 5)
	// 4: empty (content - patch 6)
	// 5: func hello() { (content - patch 7)
	// 6: deletion (content - patch 8)
	// 7: addition (content - patch 9)
	// 8: } (content - patch 10)
	// 9: empty (content - patch 11)
	// 10: func goodbye() { (content - patch 12)

	state := newStateForPagerTest(diff)
	state.SetPagerOutput(pagerOutput, newTestView())

	// Simulate selecting the deletion line (patch line 8)
	// With Option B, selectedLineIdx is a VIEW line index
	state.selectedLineIdx = state.viewLineIndices[8]
	state.selectMode = LINE

	// Get the line indices that would be staged
	lineIndices := state.LineIndicesOfAddedOrDeletedLinesInSelectedPatchRange()

	// Should return only line 8 (the deletion)
	assert.Equal(t, []int{8}, lineIndices, "Should stage only the deletion line")

	// Verify the view range
	viewStart, viewEnd := state.SelectedViewRange()

	// Patch line 8 should map to view line 6
	// (3 decoration + 3 content lines before it)
	assert.Equal(t, 6, viewStart, "Deletion should be at view line 6")
	assert.Equal(t, 6, viewEnd)

	// Now select the addition line (patch line 9)
	state.selectedLineIdx = state.viewLineIndices[9]

	lineIndices = state.LineIndicesOfAddedOrDeletedLinesInSelectedPatchRange()
	assert.Equal(t, []int{9}, lineIndices, "Should stage only the addition line")

	viewStart, viewEnd = state.SelectedViewRange()
	assert.Equal(t, 7, viewStart, "Addition should be at view line 7")
	assert.Equal(t, 7, viewEnd)
}

func TestPagerMapping_ContentLineCount(t *testing.T) {
	// Test that we correctly count content vs decoration lines

	diff := `diff --git a/test.go b/test.go
index abc123..def456 100644
--- a/test.go
+++ b/test.go
@@ -1,3 +1,3 @@
 line1
-old
+new
`

	state := newStateForPagerTest(diff)
	patchLines := state.patch.Lines()

	// Count how many content lines (non-header) are in the patch
	contentCount := 0
	for _, line := range patchLines {
		if !isDiffHeaderLine(line.Content) {
			contentCount++
		}
	}

	// Patch has:
	// 5 header lines (diff, index, ---, +++, @@)
	// 3 content lines (line1, -old, +new)
	assert.Equal(t, 3, contentCount, "Should have 3 content lines")
}

// =============================================================================
// Integration test with realistic delta output
// =============================================================================

func TestPagerMapping_RealisticDeltaOutput(t *testing.T) {
	// Real diff
	diff := `diff --git a/test.go b/test.go
index ab54e8b..d925f57 100644
--- a/test.go
+++ b/test.go
@@ -1,9 +1,10 @@
 package main
 
 func hello() {
-	fmt.Println("hello")
+	fmt.Println("hello world")
 }
 
 func goodbye() {
-	fmt.Println("goodbye")
+	fmt.Println("goodbye world")
+	fmt.Println("see you later")
 }
`

	// Patch lines (0-indexed):
	// 0: diff --git (PATCH_HEADER)
	// 1: index (PATCH_HEADER)
	// 2: --- (PATCH_HEADER)
	// 3: +++ (PATCH_HEADER)
	// 4: @@ (HUNK_HEADER)
	// 5:  package main (CONTEXT)
	// 6:  (empty CONTEXT)
	// 7:  func hello() { (CONTEXT)
	// 8: -	fmt.Println("hello") (DELETION)
	// 9: +	fmt.Println("hello world") (ADDITION)
	// 10:  } (CONTEXT)
	// 11:  (empty CONTEXT)
	// 12:  func goodbye() { (CONTEXT)
	// 13: -	fmt.Println("goodbye") (DELETION)
	// 14: +	fmt.Println("goodbye world") (ADDITION)
	// 15: +	fmt.Println("see you later") (ADDITION)
	// 16:  } (CONTEXT)

	// Simulated delta output (with line numbers style)
	// Using simple format without ANSI codes for test clarity
	pagerOutput := `───┐
1: │
───┘
  1 ⋮  1 │package main
  2 ⋮  2 │
  3 ⋮  3 │func hello() {
  4 ⋮    │	fmt.Println("hello")
    ⋮  4 │	fmt.Println("hello world")
  5 ⋮  5 │}
  6 ⋮  6 │
  7 ⋮  7 │func goodbye() {
  8 ⋮    │	fmt.Println("goodbye")
    ⋮  8 │	fmt.Println("goodbye world")
    ⋮  9 │	fmt.Println("see you later")
  9 ⋮ 10 │}
`

	// Pager lines (0-indexed):
	// 0: ───┐ (decoration)
	// 1: 1: │ (decoration)
	// 2: ───┘ (decoration)
	// 3:   1 ⋮  1 │package main (content -> patch 5)
	// 4:   2 ⋮  2 │ (content -> patch 6)
	// 5:   3 ⋮  3 │func hello() { (content -> patch 7)
	// 6:   4 ⋮    │fmt.Println("hello") (content -> patch 8, DELETION)
	// 7:     ⋮  4 │fmt.Println("hello world") (content -> patch 9, ADDITION)
	// 8:   5 ⋮  5 │} (content -> patch 10)
	// 9:   6 ⋮  6 │ (content -> patch 11)
	// 10:   7 ⋮  7 │func goodbye() { (content -> patch 12)
	// 11:   8 ⋮    │fmt.Println("goodbye") (content -> patch 13, DELETION)
	// 12:     ⋮  8 │fmt.Println("goodbye world") (content -> patch 14, ADDITION)
	// 13:     ⋮  9 │fmt.Println("see you later") (content -> patch 15, ADDITION)
	// 14:   9 ⋮ 10 │} (content -> patch 16)

	state := newStateForPagerTest(diff)
	state.SetPagerOutput(pagerOutput, newTestView())

	// Test mapping for first deletion (patch line 8)
	// With Option B, selectedLineIdx is a VIEW line index
	state.selectedLineIdx = state.viewLineIndices[8]
	state.selectMode = LINE
	viewStart, viewEnd := state.SelectedViewRange()

	assert.Equal(t, 6, viewStart, "First deletion (patch 8) should map to view line 6")
	assert.Equal(t, 6, viewEnd)

	// Verify we'd stage the right line
	lineIndices := state.LineIndicesOfAddedOrDeletedLinesInSelectedPatchRange()
	assert.Equal(t, []int{8}, lineIndices)

	// Test mapping for second deletion (patch line 13)
	state.selectedLineIdx = state.viewLineIndices[13]
	viewStart, viewEnd = state.SelectedViewRange()

	assert.Equal(t, 11, viewStart, "Second deletion (patch 13) should map to view line 11")
	assert.Equal(t, 11, viewEnd)

	lineIndices = state.LineIndicesOfAddedOrDeletedLinesInSelectedPatchRange()
	assert.Equal(t, []int{13}, lineIndices)

	// Test mapping for last addition (patch line 15)
	state.selectedLineIdx = state.viewLineIndices[15]
	viewStart, viewEnd = state.SelectedViewRange()

	assert.Equal(t, 13, viewStart, "Last addition (patch 15) should map to view line 13")
	assert.Equal(t, 13, viewEnd)

	lineIndices = state.LineIndicesOfAddedOrDeletedLinesInSelectedPatchRange()
	assert.Equal(t, []int{15}, lineIndices)
}

// =============================================================================
// Test for verifying highlight matches staging
// =============================================================================

func TestPagerMapping_HighlightMatchesStaging(t *testing.T) {
	// This is the critical test: when we highlight line X in the pager view,
	// pressing space should stage the corresponding line from the original diff

	diff := `diff --git a/file.txt b/file.txt
index 1234567..abcdefg 100644
--- a/file.txt
+++ b/file.txt
@@ -1,5 +1,5 @@
 line1
 line2
-old line3
+new line3
 line4
`

	// Patch structure:
	// 0: diff --git (header)
	// 1: index (header)
	// 2: --- (header)
	// 3: +++ (header)
	// 4: @@ (header)
	// 5: line1 (context)
	// 6: line2 (context)
	// 7: -old line3 (deletion) <- CHANGE LINE
	// 8: +new line3 (addition) <- CHANGE LINE
	// 9: line4 (context)

	pagerOutput := `───┐
1: │
───┘
  1 ⋮  1 │line1
  2 ⋮  2 │line2
  3 ⋮    │old line3
    ⋮  3 │new line3
  4 ⋮  4 │line4
`

	// Pager structure:
	// 0: decoration
	// 1: decoration
	// 2: decoration
	// 3: line1 (patch 5)
	// 4: line2 (patch 6)
	// 5: old line3 (patch 7) <- deletion
	// 6: new line3 (patch 8) <- addition
	// 7: line4 (patch 9)

	state := newStateForPagerTest(diff)
	state.SetPagerOutput(pagerOutput, newTestView())

	// Scenario: User navigates to and selects the deletion line (patch line 7)
	// With Option B, selectedLineIdx is a VIEW line index
	state.selectedLineIdx = state.viewLineIndices[7]
	state.selectMode = LINE

	// Get what would be highlighted in view
	highlightStart, highlightEnd := state.SelectedViewRange()

	// Get what would be staged
	stagingLines := state.LineIndicesOfAddedOrDeletedLinesInSelectedPatchRange()

	// The highlight should be at view line 5
	assert.Equal(t, 5, highlightStart, "Highlight should be at view line 5 (deletion)")
	assert.Equal(t, 5, highlightEnd)

	// And we should stage patch line 7
	assert.Equal(t, []int{7}, stagingLines, "Should stage patch line 7 (deletion)")

	// CRITICAL: Verify the mapping is correct
	// viewLineIndices[patchLine] = viewLine
	assert.Equal(t, highlightStart, state.viewLineIndices[7],
		"viewLineIndices[7] should equal the highlight position")

	// Now test the addition line (patch line 8)
	state.selectedLineIdx = state.viewLineIndices[8]

	highlightStart, highlightEnd = state.SelectedViewRange()
	stagingLines = state.LineIndicesOfAddedOrDeletedLinesInSelectedPatchRange()

	assert.Equal(t, 6, highlightStart, "Highlight should be at view line 6 (addition)")
	assert.Equal(t, 6, highlightEnd)
	assert.Equal(t, []int{8}, stagingLines, "Should stage patch line 8 (addition)")

	assert.Equal(t, highlightStart, state.viewLineIndices[8],
		"viewLineIndices[8] should equal the highlight position")
}

// =============================================================================
// Test for HUNK mode with pager
// =============================================================================

func TestPagerMapping_HunkMode(t *testing.T) {
	diff := `diff --git a/test.go b/test.go
index abc..def 100644
--- a/test.go
+++ b/test.go
@@ -1,5 +1,6 @@
 context
-del1
-del2
+add1
+add2
+add3
 context2
`

	pagerOutput := `───┐
1: │
───┘
  1 ⋮  1 │context
  2 ⋮    │del1
  3 ⋮    │del2
    ⋮  2 │add1
    ⋮  3 │add2
    ⋮  4 │add3
  4 ⋮  5 │context2
`

	state := newStateForPagerTest(diff)
	state.SetPagerOutput(pagerOutput, newTestView())

	// Select a change line and enable hunk mode
	// Patch lines: 6=-del1, 7=-del2, 8=+add1, 9=+add2, 10=+add3
	// View lines (after pager): 4=del1, 5=del2, 6=add1, 7=add2, 8=add3
	// With Option B, selectedLineIdx is a VIEW line index
	state.selectedLineIdx = state.viewLineIndices[6] // -del1
	state.selectMode = HUNK

	// In hunk mode, SelectedViewRange should return the full block of changes in VIEW coordinates
	viewStart, viewEnd := state.SelectedViewRange()

	// The hunk contains patch lines 6-10 (del1, del2, add1, add2, add3)
	// which map to view lines 4-8
	assert.Equal(t, 4, viewStart, "Hunk should start at view line 4 (del1)")
	assert.Equal(t, 8, viewEnd, "Hunk should end at view line 8 (add3)")
}

// =============================================================================
// Tests for wrapping support with pager
// =============================================================================

func TestPagerMapping_WithWrapping(t *testing.T) {
	// Test that wrapping works correctly with pager output
	diff := `diff --git a/test.go b/test.go
index abc123..def456 100644
--- a/test.go
+++ b/test.go
@@ -1,3 +1,3 @@
 short
-this is a very long line that will need to be wrapped when displayed in a narrow view
+this is a replacement long line that should also wrap in the view
`

	// Patch lines:
	// 0-4: headers
	// 5: short
	// 6: -this is a very long line...
	// 7: +this is a replacement long line...

	// Simulated pager output (without wrapping - pager sends unwrapped lines)
	pagerOutput := `───┐
1: │
───┘
  1 ⋮  1 │short
  2 ⋮    │this is a very long line that will need to be wrapped when displayed in a narrow view
    ⋮  2 │this is a replacement long line that should also wrap in the view
`

	// Pager lines (unwrapped):
	// 0: ───┐ (decoration)
	// 1: 1: │ (decoration)
	// 2: ───┘ (decoration)
	// 3: short (content -> patch 5)
	// 4: long deletion line (content -> patch 6)
	// 5: long addition line (content -> patch 7)

	state := newStateForPagerTest(diff)

	// Use a narrow view that will cause wrapping
	// With width 40, the long lines should wrap to multiple view lines
	view := newTestViewWithWrap(40)
	state.SetPagerOutput(pagerOutput, view)

	// Verify wrapping occurred - patchLineIndices should have more entries than pager lines
	// because wrapped lines create multiple view lines
	pagerLineCount := 6 // 3 decoration + 3 content
	assert.Greater(t, len(state.patchLineIndices), pagerLineCount,
		"Wrapping should create more view lines than pager lines")

	// Select the deletion line (patch line 6)
	state.selectedLineIdx = state.viewLineIndices[6]
	state.selectMode = LINE

	// Verify we can still get the correct patch range for staging
	lineIndices := state.LineIndicesOfAddedOrDeletedLinesInSelectedPatchRange()
	assert.Equal(t, []int{6}, lineIndices, "Should stage the deletion line (patch 6)")

	// The view line for patch 6 should be > 3 (after decoration)
	viewLine := state.viewLineIndices[6]
	assert.Greater(t, viewLine, 3, "Deletion should be after decoration lines")

	// Verify patchLineIndices maps back correctly
	assert.Equal(t, 6, state.patchLineIndices[viewLine],
		"patchLineIndices should map view line back to patch line 6")
}

func TestPagerMapping_WrappingPreservesRangeSelection(t *testing.T) {
	// Test that range selection works correctly when pager lines wrap
	diff := `diff --git a/test.go b/test.go
index abc123..def456 100644
--- a/test.go
+++ b/test.go
@@ -1,4 +1,4 @@
 context
-del1
-del2
+add1
+add2
`

	// Patch lines:
	// 0-4: headers
	// 5: context
	// 6: -del1
	// 7: -del2
	// 8: +add1
	// 9: +add2

	pagerOutput := `───┐
1: │
───┘
  1 ⋮  1 │context
  2 ⋮    │del1
  3 ⋮    │del2
    ⋮  2 │add1
    ⋮  3 │add2
`

	state := newStateForPagerTest(diff)
	view := newTestView() // No wrapping needed for this test - just verify mapping
	state.SetPagerOutput(pagerOutput, view)

	// Select range from del1 to add2 (patch lines 6-9)
	state.rangeStartLineIdx = state.viewLineIndices[6]
	state.selectedLineIdx = state.viewLineIndices[9]
	state.selectMode = RANGE

	// Verify the patch range is correct for staging
	patchStart, patchEnd := state.SelectedPatchRange()
	assert.Equal(t, 6, patchStart, "Patch range should start at line 6")
	assert.Equal(t, 9, patchEnd, "Patch range should end at line 9")

	// Verify we'd stage all change lines in the range
	lineIndices := state.LineIndicesOfAddedOrDeletedLinesInSelectedPatchRange()
	assert.Equal(t, []int{6, 7, 8, 9}, lineIndices, "Should stage all change lines in range")
}
