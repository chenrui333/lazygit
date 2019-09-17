package gui

import (
	"io/ioutil"
	"strings"

	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazygit/pkg/git"
	"github.com/jesseduffield/lazygit/pkg/utils"
)

func (gui *Gui) refreshStagingPanel() error {
	file, err := gui.getSelectedFile(gui.g)
	if err != nil {
		if err != gui.Errors.ErrNoFiles {
			return err
		}
		return gui.handleStagingEscape(gui.g, nil)
	}

	// plan: have two tabs: one for working tree diff, one for index. If you select a new file and it's staged, we should select the index tab, otherwise the working tree tab. Once you've added everything to the working tree tab we should switch to the index tab. Ideally we would show everything that's important on the one screen but I doubt git would make that easy. Seeing the two side by side could be good but would require a lot of screen space

	gui.State.SplitMainPanel = true

	if !file.HasUnstagedChanges {
		return gui.handleStagingEscape(gui.g, nil)
	}

	// note for custom diffs, we'll need to send a flag here saying not to use the custom diff
	diff := gui.GitCommand.Diff(file, true, false)
	colorDiffCached := gui.GitCommand.Diff(file, false, true)

	if len(diff) < 2 {
		return gui.handleStagingEscape(gui.g, nil)
	}

	// parse the diff and store the line numbers of hunks and stageable lines
	// TODO: maybe instantiate this at application start
	p, err := git.NewPatchParser(gui.Log)
	if err != nil {
		return nil
	}
	hunkStarts, stageableLines, err := p.ParsePatch(diff)
	if err != nil {
		return nil
	}

	var selectedLine int
	if gui.State.Panels.Staging != nil {
		end := len(stageableLines) - 1
		if end < gui.State.Panels.Staging.SelectedLine {
			selectedLine = end
		} else {
			selectedLine = gui.State.Panels.Staging.SelectedLine
		}
	} else {
		selectedLine = 0
	}

	if len(stageableLines) == 0 {
		return gui.handleStagingEscape(gui.g, nil)
	}

	gui.State.Panels.Staging = &stagingPanelState{
		StageableLines: stageableLines,
		HunkStarts:     hunkStarts,
		SelectedLine:   selectedLine,
		FirstLine:      stageableLines[selectedLine],
		LastLine:       stageableLines[selectedLine],
		Diff:           diff,
	}

	if err := gui.refreshView(); err != nil {
		return err
	}

	mainRightView := gui.getMainRightView()
	mainRightView.Highlight = true
	mainRightView.Wrap = false

	gui.g.Update(func(*gocui.Gui) error {
		return gui.setViewContent(gui.g, gui.getMainRightView(), colorDiffCached)
	})

	return nil
}

func (gui *Gui) renderView(diff string, firstLine int, lastLine int) string {
	diffLines := strings.Split(diff, "\n")
	newLines := []string{}
	for index, line := range diffLines {
		var attr color.Attribute

		if len(line) == 0 {
			newLines = append(newLines, line)
			continue
		}

		switch line[:1] {
		case "+":
			attr = color.FgGreen
		case "-":
			attr = color.FgRed
		case "@":
			attr = color.FgCyan
		default:
			attr = color.FgWhite // TODO: use theme default here
		}

		var cl *color.Color
		if index >= firstLine && index <= lastLine {
			cl = color.New(attr, color.BgBlue)
		} else {
			cl = color.New(attr)
		}
		newLines = append(newLines, utils.ColoredStringDirect(line, cl))
	}
	return strings.Join(newLines, "\n")
}

func (gui *Gui) handleStagingEscape(g *gocui.Gui, v *gocui.View) error {
	gui.State.Panels.Staging = nil

	return gui.switchFocus(gui.g, nil, gui.getFilesView())
}

func (gui *Gui) handleStagingPrevLine(g *gocui.Gui, v *gocui.View) error {
	return gui.handleCycleLine(true)
}

func (gui *Gui) handleStagingNextLine(g *gocui.Gui, v *gocui.View) error {
	return gui.handleCycleLine(false)
}

func (gui *Gui) handleStagingPrevHunk(g *gocui.Gui, v *gocui.View) error {
	return gui.handleCycleHunk(true)
}

func (gui *Gui) handleStagingNextHunk(g *gocui.Gui, v *gocui.View) error {
	return gui.handleCycleHunk(false)
}

func (gui *Gui) handleCycleHunk(prev bool) error {
	state := gui.State.Panels.Staging
	lineNumbers := state.StageableLines
	currentLine := lineNumbers[state.SelectedLine]
	currentHunkIndex := utils.PrevIndex(state.HunkStarts, currentLine)
	var newHunkIndex int
	if prev {
		if currentHunkIndex == 0 {
			return nil
		}
		newHunkIndex = currentHunkIndex - 1
	} else {
		if currentHunkIndex == len(state.HunkStarts)-1 {
			return nil
		}
		newHunkIndex = currentHunkIndex + 1
	}

	state.SelectedLine = utils.NextIndex(lineNumbers, state.HunkStarts[newHunkIndex])

	return gui.refreshView()
}

func (gui *Gui) handleCycleLine(prev bool) error {
	state := gui.State.Panels.Staging

	if state.SelectingHunk {
		if prev {
			if state.FirstLine == state.HunkStarts[0] {
				return nil
			}
			state.FirstLine, state.LastLine = gui.getHunkRange(state.FirstLine - 1)
		} else {
			if state.LastLine >= gui.diffLength() {
				return nil
			}
			// state.LastLine is the last line in the hunk so we'll just get the next line after that
			state.FirstLine, state.LastLine = gui.getHunkRange(state.LastLine + 1)
		}

		// get first stageable line in hunk to set as the selectedLine
		for index, lineIdx := range state.StageableLines {
			if lineIdx >= state.FirstLine {
				state.SelectedLine = index
				break
			}
		}

		return gui.refreshView()
	}

	lineNumbers := state.StageableLines
	currentLine := lineNumbers[state.SelectedLine]
	var newIndex int
	if prev {
		newIndex = utils.PrevIndex(lineNumbers, currentLine)
	} else {
		newIndex = utils.NextIndex(lineNumbers, currentLine)
	}
	state.SelectedLine = newIndex
	newCurrentLine := lineNumbers[state.SelectedLine]

	if state.SelectingLineRange {
		if newCurrentLine < state.FirstLine {
			state.FirstLine = newCurrentLine
		} else {
			state.LastLine = newCurrentLine
		}
	} else {
		state.LastLine = newCurrentLine
		state.FirstLine = newCurrentLine
	}

	return gui.refreshView()
}

func (gui *Gui) diffLength() int {
	state := gui.State.Panels.Staging

	return strings.Count(state.Diff, "\n")
}

func (gui *Gui) refreshView() error {
	state := gui.State.Panels.Staging

	colorDiff := gui.renderView(state.Diff, state.FirstLine, state.LastLine)

	mainView := gui.getMainView()
	mainView.Highlight = true
	mainView.Wrap = false

	gui.g.Update(func(*gocui.Gui) error {
		return gui.setViewContent(gui.g, gui.getMainView(), colorDiff)
	})

	return gui.focusLineAndHunk()
}

// focusLineAndHunk works out the best focus for the staging panel given the
// selected line and size of the hunk
func (gui *Gui) focusLineAndHunk() error {
	stagingView := gui.getMainView()
	state := gui.State.Panels.Staging

	lineNumber := state.StageableLines[state.SelectedLine]

	_, viewHeight := stagingView.Size()
	bufferHeight := viewHeight - 1
	_, origin := stagingView.Origin()

	var newOrigin int
	if lineNumber-origin < 3 {
		newOrigin = lineNumber - 3
	} else if lineNumber-origin > bufferHeight-3 {
		newOrigin = lineNumber - bufferHeight + 3
	} else {
		newOrigin = origin
	}

	if err := stagingView.SetOrigin(0, newOrigin); err != nil {
		return err
	}

	if err := stagingView.SetCursor(0, lineNumber-newOrigin); err != nil {
		return err
	}

	return nil

	// // we want the bottom line of the view buffer to ideally be the bottom line
	// // of the hunk, but if the hunk is too big we'll just go three lines beyond
	// // the currently selected line so that the user can see the context
	// var bottomLine int
	// nextHunkStartIndex := utils.NextIndex(state.HunkStarts, lineNumber)
	// if nextHunkStartIndex == 0 {
	// 	// for now linesHeight is an efficient means of getting the number of lines
	// 	// in the patch. However if we introduce word wrap we'll need to update this
	// 	bottomLine = stagingView.LinesHeight() - 1
	// } else {
	// 	bottomLine = state.HunkStarts[nextHunkStartIndex] - 1
	// }

	// hunkStartIndex := utils.PrevIndex(state.HunkStarts, lineNumber)
	// hunkStart := state.HunkStarts[hunkStartIndex]
	// // if it's the first hunk we'll also show the diff header
	// if hunkStartIndex == 0 {
	// 	hunkStart = 0
	// }

	// _, height := stagingView.Size()
	// // if this hunk is too big, we will just ensure that the user can at least
	// // see three lines of context below the cursor
	// if bottomLine-hunkStart > height {
	// 	bottomLine = lineNumber + 3
	// }

	// return gui.generalFocusLine(lineNumber, bottomLine, stagingView)
}

func (gui *Gui) handleStageSelection(g *gocui.Gui, v *gocui.View) error {
	return gui.applySelection(false)
}

func (gui *Gui) handleResetSelection(g *gocui.Gui, v *gocui.View) error {
	return gui.applySelection(true)
}

func (gui *Gui) applySelection(reverse bool) error {
	state := gui.State.Panels.Staging

	state.SelectingLineRange = false
	state.SelectingHunk = false

	file, err := gui.getSelectedFile(gui.g)
	if err != nil {
		return err
	}

	diffParser := git.NewPatchGenerator(gui.Log, file.Name, state.Diff)
	patch := diffParser.GeneratePatch(state.FirstLine, state.LastLine, reverse)

	// for logging purposes
	ioutil.WriteFile("patch.diff", []byte(patch), 0600)

	// apply the patch then refresh this panel
	// create a new temp file with the patch, then call git apply with that patch
	_, err = gui.GitCommand.ApplyPatch(patch, false, !reverse)
	if err != nil {
		return err
	}

	if err := gui.refreshFiles(); err != nil {
		return err
	}
	if err := gui.refreshStagingPanel(); err != nil {
		return err
	}
	return nil
}

func (gui *Gui) handleToggleSelectRange(g *gocui.Gui, v *gocui.View) error {
	state := gui.State.Panels.Staging
	lineNumber := state.StageableLines[state.SelectedLine]
	state.SelectingLineRange = !state.SelectingLineRange
	state.SelectingHunk = false
	state.FirstLine = lineNumber
	state.LastLine = lineNumber

	return gui.refreshView()
}

func (gui *Gui) handleToggleSelectHunk(g *gocui.Gui, v *gocui.View) error {
	state := gui.State.Panels.Staging

	state.SelectingHunk = !state.SelectingHunk
	state.SelectingLineRange = false
	lineNumber := state.StageableLines[state.SelectedLine]

	// if we're no longer selecting a hunk, reset the line number and refresh
	if !state.SelectingHunk {
		state.FirstLine, state.LastLine = lineNumber, lineNumber
	} else {
		state.FirstLine, state.LastLine = gui.getHunkRange(lineNumber)
	}

	return gui.refreshView()
}

func (gui *Gui) getHunkRange(lineNumber int) (int, int) {
	state := gui.State.Panels.Staging

	hunkStart := -1
	hunkEnd := -1
	for index, start := range state.HunkStarts {
		if lineNumber < start {
			continue
		} else {
			hunkStart = start
			if len(state.HunkStarts) > index+1 {
				hunkEnd = state.HunkStarts[index+1] - 1
			} else {
				hunkEnd = gui.diffLength()
			}
		}
	}

	return hunkStart, hunkEnd
}
