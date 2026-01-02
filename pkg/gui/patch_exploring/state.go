package patch_exploring

import (
	"fmt"
	"os"
	"strings"

	"github.com/jesseduffield/generics/set"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazygit/pkg/commands/patch"
	"github.com/jesseduffield/lazygit/pkg/utils"
	"github.com/samber/lo"
)

// State represents the current state of the patch explorer context i.e. when
// you're staging a file or you're building a patch from an existing commit
// this struct holds the info about the diff you're interacting with and what's currently selected.
type State struct {
	// These are in terms of view lines (wrapped), not patch lines
	selectedLineIdx   int
	rangeStartLineIdx int
	// If a range is sticky, it means we expand the range when we move up or down.
	// Otherwise, we cancel the range when we move up or down.
	rangeIsSticky bool
	diff          string
	patch         *patch.Patch
	selectMode    selectMode

	// Array of indices of the wrapped lines indexed by a patch line index
	viewLineIndices []int
	// Array of indices of the original patch lines indexed by a wrapped view line index
	patchLineIndices []int

	// whether the user has switched to hunk mode manually; if hunk mode is on
	// but this is false, then hunk mode was enabled because the config makes it
	// on by default.
	// this makes a difference for whether we want to escape out of hunk mode
	userEnabledHunkMode bool

	// pagerOutput holds the pager-rendered diff for display purposes.
	// When set, this is used for rendering instead of the internal colored diff.
	pagerOutput string
	// pagerViewLineIndices maps pager output view lines to patch lines
	pagerViewLineIndices []int
	// pagerPatchLineIndices maps patch lines to pager output view lines
	pagerPatchLineIndices []int
}

// these represent what select mode we're in
type selectMode int

const (
	LINE selectMode = iota
	RANGE
	HUNK
)

func NewState(diff string, selectedLineIdx int, view *gocui.View, oldState *State, useHunkModeByDefault bool) *State {
	if oldState != nil && diff == oldState.diff && selectedLineIdx == -1 {
		// if we're here then we can return the old state. If selectedLineIdx was not -1
		// then that would mean we were trying to click and potentially drag a range, which
		// is why in that case we continue below
		return oldState
	}

	patch := patch.Parse(diff)

	if !patch.ContainsChanges() {
		return nil
	}

	viewLineIndices, patchLineIndices := wrapPatchLines(diff, view)

	rangeStartLineIdx := 0
	if oldState != nil {
		rangeStartLineIdx = oldState.rangeStartLineIdx
	}

	selectMode := LINE
	if useHunkModeByDefault && !patch.IsSingleHunkForWholeFile() {
		selectMode = HUNK
	}

	userEnabledHunkMode := false
	if oldState != nil {
		userEnabledHunkMode = oldState.userEnabledHunkMode
	}

	// if we have clicked from the outside to focus the main view we'll pass in a non-negative line index so that we can instantly select that line
	if selectedLineIdx >= 0 {
		// Clamp to the number of wrapped view lines; index might be out of
		// bounds if a custom pager is being used which produces more lines
		selectedLineIdx = min(selectedLineIdx, len(viewLineIndices)-1)

		selectMode = RANGE
		rangeStartLineIdx = selectedLineIdx
	} else if oldState != nil {
		// if we previously had a selectMode of RANGE, we want that to now be line again (or hunk, if that's the default)
		if oldState.selectMode != RANGE {
			selectMode = oldState.selectMode
		}
		selectedLineIdx = viewLineIndices[patch.GetNextChangeIdx(oldState.patchLineIndices[oldState.selectedLineIdx])]
	} else {
		selectedLineIdx = viewLineIndices[patch.GetNextChangeIdx(0)]
	}

	return &State{
		patch:               patch,
		selectedLineIdx:     selectedLineIdx,
		selectMode:          selectMode,
		rangeStartLineIdx:   rangeStartLineIdx,
		rangeIsSticky:       false,
		diff:                diff,
		viewLineIndices:     viewLineIndices,
		patchLineIndices:    patchLineIndices,
		userEnabledHunkMode: userEnabledHunkMode,
	}
}

func (s *State) OnViewWidthChanged(view *gocui.View) {
	if !view.Wrap {
		return
	}

	selectedPatchLineIdx := s.patchLineIndices[s.selectedLineIdx]
	var rangeStartPatchLineIdx int
	if s.selectMode == RANGE {
		rangeStartPatchLineIdx = s.patchLineIndices[s.rangeStartLineIdx]
	}
	s.viewLineIndices, s.patchLineIndices = wrapPatchLines(s.diff, view)
	s.selectedLineIdx = s.viewLineIndices[selectedPatchLineIdx]
	if s.selectMode == RANGE {
		s.rangeStartLineIdx = s.viewLineIndices[rangeStartPatchLineIdx]
	}
}

func (s *State) GetSelectedPatchLineIdx() int {
	return s.patchLineIndices[s.selectedLineIdx]
}

func (s *State) GetSelectedViewLineIdx() int {
	return s.selectedLineIdx
}

func (s *State) GetDiff() string {
	return s.diff
}

func (s *State) ToggleSelectHunk() {
	if s.selectMode == HUNK {
		s.selectMode = LINE
	} else {
		s.selectMode = HUNK
		s.userEnabledHunkMode = true

		// If we are not currently on a change line, select the next one (or the
		// previous one if there is no next one):
		s.selectedLineIdx = s.viewLineIndices[s.patch.GetNextChangeIdx(
			s.patchLineIndices[s.selectedLineIdx])]
	}
}

func (s *State) ToggleStickySelectRange() {
	s.ToggleSelectRange(true)
}

func (s *State) ToggleSelectRange(sticky bool) {
	if s.SelectingRange() {
		s.selectMode = LINE
	} else {
		s.selectMode = RANGE
		s.rangeStartLineIdx = s.selectedLineIdx
		s.rangeIsSticky = sticky
	}
}

func (s *State) SetRangeIsSticky(value bool) {
	s.rangeIsSticky = value
}

func (s *State) SelectingHunk() bool {
	return s.selectMode == HUNK
}

func (s *State) SelectingHunkEnabledByUser() bool {
	return s.selectMode == HUNK && s.userEnabledHunkMode
}

func (s *State) SelectingRange() bool {
	return s.selectMode == RANGE && (s.rangeIsSticky || s.rangeStartLineIdx != s.selectedLineIdx)
}

func (s *State) SelectingLine() bool {
	return s.selectMode == LINE
}

func (s *State) SetLineSelectMode() {
	s.selectMode = LINE
}

func (s *State) DismissHunkSelectMode() {
	if s.SelectingHunk() {
		s.selectMode = LINE
	}
}

// For when you move the cursor without holding shift (meaning if we're in
// a non-sticky range select, we'll cancel it)
func (s *State) SelectLine(newSelectedLineIdx int) {
	if s.selectMode == RANGE && !s.rangeIsSticky {
		s.selectMode = LINE
	}

	s.selectLineWithoutRangeCheck(newSelectedLineIdx)
}

func (s *State) clampLineIdx(lineIdx int) int {
	return lo.Clamp(lineIdx, 0, len(s.patchLineIndices)-1)
}

// This just moves the cursor without caring about range select
func (s *State) selectLineWithoutRangeCheck(newSelectedLineIdx int) {
	s.selectedLineIdx = s.clampLineIdx(newSelectedLineIdx)
}

func (s *State) SelectNewLineForRange(newSelectedLineIdx int) {
	s.rangeStartLineIdx = s.clampLineIdx(newSelectedLineIdx)

	s.selectMode = RANGE

	s.selectLineWithoutRangeCheck(newSelectedLineIdx)
}

func (s *State) DragSelectLine(newSelectedLineIdx int) {
	s.selectMode = RANGE

	s.selectLineWithoutRangeCheck(newSelectedLineIdx)
}

func (s *State) CycleSelection(forward bool) {
	if s.SelectingHunk() {
		if forward {
			s.SelectNextHunk()
		} else {
			s.SelectPreviousHunk()
		}
	} else {
		s.CycleLine(forward)
	}
}

func (s *State) SelectPreviousHunk() {
	patchLines := s.patch.Lines()
	patchLineIdx := s.patchLineIndices[s.selectedLineIdx]
	nextNonChangeLine := patchLineIdx
	for nextNonChangeLine >= 0 && patchLines[nextNonChangeLine].IsChange() {
		nextNonChangeLine--
	}
	nextChangeLine := nextNonChangeLine
	for nextChangeLine >= 0 && !patchLines[nextChangeLine].IsChange() {
		nextChangeLine--
	}
	if nextChangeLine >= 0 {
		// Now we found a previous hunk, but we're on its last line. Skip to the beginning.
		for nextChangeLine > 0 && patchLines[nextChangeLine-1].IsChange() {
			nextChangeLine--
		}
		s.selectedLineIdx = s.viewLineIndices[nextChangeLine]
	}
}

func (s *State) SelectNextHunk() {
	patchLines := s.patch.Lines()
	patchLineIdx := s.patchLineIndices[s.selectedLineIdx]
	nextNonChangeLine := patchLineIdx
	for nextNonChangeLine < len(patchLines) && patchLines[nextNonChangeLine].IsChange() {
		nextNonChangeLine++
	}
	nextChangeLine := nextNonChangeLine
	for nextChangeLine < len(patchLines) && !patchLines[nextChangeLine].IsChange() {
		nextChangeLine++
	}
	if nextChangeLine < len(patchLines) {
		s.selectedLineIdx = s.viewLineIndices[nextChangeLine]
	}
}

func (s *State) CycleLine(forward bool) {
	change := 1
	if !forward {
		change = -1
	}

	s.SelectLine(s.selectedLineIdx + change)
}

// This is called when we use shift+arrow to expand the range (i.e. a non-sticky
// range)
func (s *State) CycleRange(forward bool) {
	if !s.SelectingRange() {
		s.ToggleSelectRange(false)
	}

	s.SetRangeIsSticky(false)

	change := 1
	if !forward {
		change = -1
	}

	s.selectLineWithoutRangeCheck(s.selectedLineIdx + change)
}

// returns first and last patch line index of current hunk
func (s *State) CurrentHunkBounds() (int, int) {
	hunkIdx := s.patch.HunkContainingLine(s.patchLineIndices[s.selectedLineIdx])
	start := s.patch.HunkStartIdx(hunkIdx)
	end := s.patch.HunkEndIdx(hunkIdx)
	return start, end
}

func (s *State) selectionRangeForCurrentBlockOfChanges() (int, int) {
	patchLines := s.patch.Lines()
	patchLineIdx := s.patchLineIndices[s.selectedLineIdx]

	patchStart := patchLineIdx
	for patchStart > 0 && patchLines[patchStart-1].IsChange() {
		patchStart--
	}

	patchEnd := patchLineIdx
	for patchEnd < len(patchLines)-1 && patchLines[patchEnd+1].IsChange() {
		patchEnd++
	}

	viewStart, viewEnd := s.viewLineIndices[patchStart], s.viewLineIndices[patchEnd]

	// Increase viewEnd in case the last patch line is wrapped to more than one view line.
	for viewEnd < len(s.patchLineIndices)-1 && s.patchLineIndices[viewEnd] == s.patchLineIndices[viewEnd+1] {
		viewEnd++
	}

	return viewStart, viewEnd
}

func (s *State) SelectedViewRange() (int, int) {
	switch s.selectMode {
	case HUNK:
		return s.selectionRangeForCurrentBlockOfChanges()
	case RANGE:
		if s.rangeStartLineIdx > s.selectedLineIdx {
			return s.selectedLineIdx, s.rangeStartLineIdx
		}
		return s.rangeStartLineIdx, s.selectedLineIdx
	case LINE:
		return s.selectedLineIdx, s.selectedLineIdx
	default:
		// should never happen
		return 0, 0
	}
}

func (s *State) SelectedPatchRange() (int, int) {
	start, end := s.SelectedViewRange()
	return s.patchLineIndices[start], s.patchLineIndices[end]
}

// Returns the line indices of the selected patch range that are changes (i.e. additions or deletions)
func (s *State) LineIndicesOfAddedOrDeletedLinesInSelectedPatchRange() []int {
	viewStart, viewEnd := s.SelectedViewRange()
	patchStart, patchEnd := s.patchLineIndices[viewStart], s.patchLineIndices[viewEnd]
	lines := s.patch.Lines()
	indices := []int{}
	for i := patchStart; i <= patchEnd; i++ {
		if lines[i].IsChange() {
			indices = append(indices, i)
		}
	}
	return indices
}

func (s *State) CurrentLineNumber() int {
	return s.patch.LineNumberOfLine(s.patchLineIndices[s.selectedLineIdx])
}

func (s *State) AdjustSelectedLineIdx(change int) {
	s.DismissHunkSelectMode()
	s.SelectLine(s.selectedLineIdx + change)
}

func (s *State) RenderForLineIndices(includedLineIndices []int) string {
	// If pager output is available, use it for display
	if s.pagerOutput != "" {
		return s.pagerOutput
	}

	includedLineIndicesSet := set.NewFromSlice(includedLineIndices)
	return s.patch.FormatView(patch.FormatViewOpts{
		IncLineIndices: includedLineIndicesSet,
	})
}

// SetPagerOutput sets the pager-rendered diff output for display.
// This builds a line mapping between pager output and the original diff.
// The approach: match patch content lines to pager lines by their actual content.
func (s *State) SetPagerOutput(pagerOutput string) {
	s.pagerOutput = pagerOutput

	if pagerOutput == "" {
		s.pagerViewLineIndices = nil
		s.pagerPatchLineIndices = nil
		return
	}

	pagerLines := strings.Split(strings.TrimSuffix(pagerOutput, "\n"), "\n")
	patchLines := s.patch.Lines()
	patchLineCount := len(patchLines)
	pagerLineCount := len(pagerLines)

	// DEBUG: Log the mapping setup
	debugFile, _ := os.OpenFile("/tmp/pager_mapping_debug.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if debugFile != nil {
		fmt.Fprintf(debugFile, "\n=== SetPagerOutput called ===\n")
		fmt.Fprintf(debugFile, "patchLineCount=%d pagerLineCount=%d\n", patchLineCount, pagerLineCount)
		defer debugFile.Close()
	}

	// pagerViewLineIndices[patchLineIdx] = corresponding pagerLineIdx
	s.pagerViewLineIndices = make([]int, patchLineCount)
	// pagerPatchLineIndices[pagerLineIdx] = corresponding patchLineIdx (-1 if decoration)
	s.pagerPatchLineIndices = make([]int, pagerLineCount)

	// Initialize pagerPatchLineIndices to -1 (no match)
	for i := range s.pagerPatchLineIndices {
		s.pagerPatchLineIndices[i] = -1
	}

	// Strip ANSI codes from all pager lines for matching
	strippedPagerLines := make([]string, pagerLineCount)
	for i, line := range pagerLines {
		strippedPagerLines[i] = stripAnsiCodes(line)
	}

	// For each patch content line, find its corresponding pager line by content matching
	// We search forward from the last matched position to handle duplicates correctly
	lastMatchedPagerIdx := 0
	firstContentPagerLine := -1

	for patchIdx, patchLine := range patchLines {
		if isDiffHeaderLine(patchLine.Content) {
			// Header lines will be mapped later
			continue
		}

		// Get the patch line content (without the leading +/- / space for change lines)
		patchContent := patchLine.Content

		// Search for this content in pager output, starting from last match
		found := false
		for pagerIdx := lastMatchedPagerIdx; pagerIdx < pagerLineCount; pagerIdx++ {
			strippedPager := strippedPagerLines[pagerIdx]

			// Check if the pager line contains the patch content
			// The pager might add prefixes (line numbers) or styling, but the core content should match
			if contentMatches(patchContent, strippedPager) {
				s.pagerViewLineIndices[patchIdx] = pagerIdx
				s.pagerPatchLineIndices[pagerIdx] = patchIdx
				lastMatchedPagerIdx = pagerIdx + 1

				if firstContentPagerLine == -1 {
					firstContentPagerLine = pagerIdx
				}

				if debugFile != nil && patchIdx < 30 {
					fmt.Fprintf(debugFile, "MATCH: patch[%d] %q -> pager[%d]\n", patchIdx, truncate(patchContent, 40), pagerIdx)
				}
				found = true
				break
			}
		}

		if !found {
			// No match found - map to the last matched pager line or 0
			if lastMatchedPagerIdx > 0 {
				s.pagerViewLineIndices[patchIdx] = lastMatchedPagerIdx - 1
			}
			if debugFile != nil && patchIdx < 30 {
				fmt.Fprintf(debugFile, "NO MATCH: patch[%d] %q\n", patchIdx, truncate(patchContent, 40))
			}
		}
	}

	// For header lines, map to the first content pager line
	if firstContentPagerLine == -1 {
		firstContentPagerLine = 0
	}
	for i, line := range patchLines {
		if isDiffHeaderLine(line.Content) {
			s.pagerViewLineIndices[i] = firstContentPagerLine
		}
	}

	// Rebuild viewLineIndices and patchLineIndices to be 1:1 mappings (no wrapping)
	// since the pager handles its own formatting
	oldSelectedPatchLine := 0
	oldRangeStartPatchLine := 0
	if s.selectedLineIdx < len(s.patchLineIndices) {
		oldSelectedPatchLine = s.patchLineIndices[s.selectedLineIdx]
	}
	if s.rangeStartLineIdx < len(s.patchLineIndices) {
		oldRangeStartPatchLine = s.patchLineIndices[s.rangeStartLineIdx]
	}

	s.viewLineIndices = make([]int, patchLineCount)
	s.patchLineIndices = make([]int, patchLineCount)
	for i := 0; i < patchLineCount; i++ {
		s.viewLineIndices[i] = i
		s.patchLineIndices[i] = i
	}

	if oldSelectedPatchLine < patchLineCount {
		s.selectedLineIdx = oldSelectedPatchLine
	}
	if oldRangeStartPatchLine < patchLineCount {
		s.rangeStartLineIdx = oldRangeStartPatchLine
	}
}

// stripAnsiCodes removes ANSI escape sequences and pager decorations from a string
func stripAnsiCodes(s string) string {
	// First strip ANSI escape sequences: ESC[ followed by parameters and a letter
	// Use runes to preserve UTF-8 characters
	var result strings.Builder
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		if i+1 < len(runes) && runes[i] == '\x1b' && runes[i+1] == '[' {
			// Skip until we find the terminating letter
			j := i + 2
			for j < len(runes) && (runes[j] == ';' || (runes[j] >= '0' && runes[j] <= '9')) {
				j++
			}
			if j < len(runes) {
				j++ // skip the terminating letter
			}
			i = j
		} else {
			result.WriteRune(runes[i])
			i++
		}
	}

	stripped := result.String()

	// Strip delta's box-drawing decorations and line numbers if present
	// Delta format with --line-numbers: "│  57 │ 57 │    actual code"
	// We want to extract just "    actual code"
	// Without --line-numbers: just the content (possibly with leading +/- stripped by delta)
	lastPipe := strings.LastIndex(stripped, "│")
	if lastPipe != -1 && lastPipe < len(stripped)-1 {
		stripped = stripped[lastPipe+len("│"):]
	}

	return stripped
}

// contentMatches checks if a patch line content matches a pager line content
// The pager might transform the content (add/remove leading +/- markers, etc.)
func contentMatches(patchContent, pagerContent string) bool {
	// Normalize both strings for comparison
	patchNorm := normalizeForMatch(patchContent)
	pagerNorm := normalizeForMatch(pagerContent)

	// Exact match after normalization
	if patchNorm == pagerNorm {
		return true
	}

	// For change lines, the pager might strip the leading +/- marker
	// So also check if patch content without marker matches
	if len(patchContent) > 0 && (patchContent[0] == '+' || patchContent[0] == '-' || patchContent[0] == ' ') {
		patchWithoutMarker := normalizeForMatch(patchContent[1:])
		if patchWithoutMarker == pagerNorm {
			return true
		}
	}

	// Check if pager content contains the patch content (for cases where pager adds prefixes)
	if len(patchNorm) > 0 && strings.Contains(pagerNorm, patchNorm) {
		return true
	}

	return false
}

// normalizeForMatch normalizes a string for content matching
func normalizeForMatch(s string) string {
	// Trim leading/trailing whitespace but preserve internal structure
	return strings.TrimSpace(s)
}

// truncate shortens a string for debug output
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// isDecorationLine checks if a pager line is a decoration (hunk header, separator, etc.)
// rather than actual diff content
func isDecorationLine(pagerLine string) bool {
	// Strip ANSI codes first
	stripped := stripAnsiOnly(pagerLine)
	trimmed := strings.TrimSpace(stripped)

	// Empty lines after stripping are decorations
	if trimmed == "" {
		return true
	}

	// Check for separator lines that are only box-drawing characters
	// This includes delta's header box borders (───┐, ───┘) and line decorations
	isOnlyBoxDrawing := true
	for _, r := range trimmed {
		// Include all common box-drawing characters used by delta:
		// ─ (horizontal), │ (vertical), ┐┘┌└ (corners), ⋮ (vertical dots)
		// ═ (double horizontal), ╭╮╰╯ (rounded corners), ┼┬┴ (intersections)
		if r != '─' && r != '═' && r != '│' && r != '⋮' &&
			r != '┼' && r != '┬' && r != '┴' &&
			r != '╭' && r != '╮' && r != '╰' && r != '╯' &&
			r != '┐' && r != '┘' && r != '┌' && r != '└' &&
			r != ' ' {
			isOnlyBoxDrawing = false
			break
		}
	}
	if isOnlyBoxDrawing {
		return true
	}

	// Delta hunk headers without line numbers look like: "@@ -1,5 +1,7 @@" or similar
	// These start with @@ and are decorations
	if strings.HasPrefix(trimmed, "@@") {
		return true
	}

	// Delta file headers might look like paths or have special formatting
	// But we can't reliably detect these without more context

	// If line has ⋮ (delta line number separator), it's a content line
	// This includes empty content lines like "  2 ⋮  2 │" which represent blank lines in the diff
	if strings.Contains(stripped, "⋮") {
		return false // Content line (may be empty but still represents a diff line)
	}

	// Lines with │ but no ⋮ might be file/hunk header decorations (e.g., "1: │")
	// Check if there's actual content after the pipe
	if strings.Contains(stripped, "│") {
		lastPipe := strings.LastIndex(stripped, "│")
		if lastPipe != -1 && lastPipe < len(stripped)-1 {
			afterPipe := strings.TrimSpace(stripped[lastPipe+len("│"):])
			if afterPipe == "" {
				return true // No content after pipe and no line numbers = decoration
			}
		}
		// If just "│" alone or "N: │" pattern, it's decoration
		if lastPipe == len(stripped)-len("│") {
			return true
		}
		return false // Has content after pipe
	}

	// Without box characters, assume it's content
	return false
}

// stripAnsiOnly removes only ANSI escape sequences, not box characters
func stripAnsiOnly(s string) string {
	var result strings.Builder
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		if i+1 < len(runes) && runes[i] == '\x1b' && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) && (runes[j] == ';' || (runes[j] >= '0' && runes[j] <= '9')) {
				j++
			}
			if j < len(runes) {
				j++
			}
			i = j
		} else {
			result.WriteRune(runes[i])
			i++
		}
	}
	return result.String()
}

// isEmptyDecorationLine checks if a pager line is a delta file/hunk header
// (e.g., "66: func..." context lines) that have no actual diff content.
// These are different from empty content lines which have ⋮ between line numbers.
func isEmptyDecorationLine(pagerLine string) bool {
	stripped := stripAnsiCodes(pagerLine)
	if strings.TrimSpace(stripped) != "" {
		return false // Has content after stripping, not empty
	}

	// If the original line (with ANSI) has box characters, it's a content row
	// that happens to have empty content (blank line in the diff)
	if strings.Contains(pagerLine, "│") || strings.Contains(pagerLine, "⋮") {
		// Check if there's actual structure (line numbers) - if so, it's a content line
		// that just has empty content
		return false
	}

	return true // Truly empty = file/hunk header decoration
}

// isDiffHeaderLine checks if a patch line is a diff header (not actual content)
func isDiffHeaderLine(line string) bool {
	return strings.HasPrefix(line, "diff --git") ||
		strings.HasPrefix(line, "index ") ||
		strings.HasPrefix(line, "--- ") ||
		strings.HasPrefix(line, "+++ ") ||
		strings.HasPrefix(line, "@@")
}

// HasPagerOutput returns true if pager output is available for display
func (s *State) HasPagerOutput() bool {
	return s.pagerOutput != ""
}

// SelectedViewRangeForPager returns the view range translated to pager coordinates
// when pager output is active. Falls back to regular SelectedViewRange otherwise.
func (s *State) SelectedViewRangeForPager() (int, int) {
	if !s.HasPagerOutput() || s.pagerViewLineIndices == nil {
		return s.SelectedViewRange()
	}

	// Get patch line indices directly and translate to pager coordinates
	patchStart, patchEnd := s.SelectedPatchRange()

	// DEBUG: Log the translation
	debugFile, _ := os.OpenFile("/tmp/pager_nav_debug.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if debugFile != nil {
		viewStart, viewEnd := s.SelectedViewRange()
		fmt.Fprintf(debugFile, "NAV: viewRange=[%d,%d] patchRange=[%d,%d] ", viewStart, viewEnd, patchStart, patchEnd)
		defer debugFile.Close()
	}

	// Bounds check before indexing
	if patchStart >= len(s.pagerViewLineIndices) || patchEnd >= len(s.pagerViewLineIndices) {
		if debugFile != nil {
			fmt.Fprintf(debugFile, "FALLBACK (out of bounds)\n")
		}
		return s.SelectedViewRange()
	}

	pagerStart := s.pagerViewLineIndices[patchStart]
	pagerEnd := s.pagerViewLineIndices[patchEnd]

	if debugFile != nil {
		fmt.Fprintf(debugFile, "pagerRange=[%d,%d]\n", pagerStart, pagerEnd)
	}

	return pagerStart, pagerEnd
}

// CalculateOriginForPager calculates the origin adjusted for pager output
func (s *State) CalculateOriginForPager(currentOrigin int, bufferHeight int, numLines int) int {
	if !s.HasPagerOutput() || s.pagerViewLineIndices == nil {
		return s.CalculateOrigin(currentOrigin, bufferHeight, numLines)
	}

	firstLineIdx, lastLineIdx := s.SelectedViewRangeForPager()

	// Get selected patch line and translate to pager coordinates
	_, selectedPatchLineIdx := s.SelectedPatchRange()
	if selectedPatchLineIdx >= len(s.pagerViewLineIndices) {
		return s.CalculateOrigin(currentOrigin, bufferHeight, numLines)
	}
	selectedLineIdx := s.pagerViewLineIndices[selectedPatchLineIdx]

	return calculateOrigin(currentOrigin, bufferHeight, numLines, firstLineIdx, lastLineIdx, selectedLineIdx, s.selectMode)
}

func (s *State) PlainRenderSelected() string {
	firstLineIdx, lastLineIdx := s.SelectedPatchRange()
	return s.patch.FormatRangePlain(firstLineIdx, lastLineIdx)
}

func (s *State) SelectBottom() {
	s.DismissHunkSelectMode()
	s.SelectLine(len(s.patchLineIndices) - 1)
}

func (s *State) SelectTop() {
	s.DismissHunkSelectMode()
	s.SelectLine(0)
}

func (s *State) CalculateOrigin(currentOrigin int, bufferHeight int, numLines int) int {
	firstLineIdx, lastLineIdx := s.SelectedViewRange()

	return calculateOrigin(currentOrigin, bufferHeight, numLines, firstLineIdx, lastLineIdx, s.GetSelectedViewLineIdx(), s.selectMode)
}

func wrapPatchLines(diff string, view *gocui.View) ([]int, []int) {
	_, viewLineIndices, patchLineIndices := utils.WrapViewLinesToWidth(
		view.Wrap, view.Editable, strings.TrimSuffix(diff, "\n"), view.InnerWidth(), view.TabWidth)
	return viewLineIndices, patchLineIndices
}

func (s *State) SelectNextStageableLineOfSameIncludedState(includedLines []int, included bool) {
	_, lastLineIdx := s.SelectedPatchRange()
	patchLineIdx, found := s.patch.GetNextChangeIdxOfSameIncludedState(lastLineIdx+1, includedLines, included)
	if found {
		s.SelectLine(s.viewLineIndices[patchLineIdx])
	}
}
