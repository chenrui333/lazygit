package gui

import (
	"io/ioutil"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazygit/pkg/git"
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

	patchParser, err := git.NewPatchParser(gui.Log, diff)
	if err != nil {
		return nil
	}

	if len(patchParser.StageableLines) == 0 {
		return gui.handleStagingEscape(gui.g, nil)
	}

	var selectedLine int
	var firstLine int
	var lastLine int
	selectingHunk := false
	state := gui.State.Panels.Staging
	if state != nil {
		if state.SelectingHunk {
			// this is tricky: we need to find out which hunk we just staged based on our old `state.PatchParser` (as opposed to the new `patchParser`)
			// we do this by getting the first line index of the original hunk, then
			// finding the next stageable line, then getting its containing hunk
			// in the new diff
			selectingHunk = true
			prevNewHunk := state.PatchParser.GetHunkContainingLine(state.SelectedLine, 0)
			selectedLine = patchParser.GetNextStageableLineIndex(prevNewHunk.FirstLineIndex)
			newHunk := patchParser.GetHunkContainingLine(selectedLine, 0)
			firstLine, lastLine = newHunk.FirstLineIndex, newHunk.LastLineIndex
		} else {
			selectedLine = patchParser.GetNextStageableLineIndex(state.SelectedLine)
			firstLine, lastLine = selectedLine, selectedLine
		}
	} else {
		selectedLine = patchParser.StageableLines[0]
		firstLine, lastLine = selectedLine, selectedLine
	}

	gui.State.Panels.Staging = &stagingPanelState{
		PatchParser:        patchParser,
		SelectedLine:       selectedLine,
		SelectingLineRange: false,
		SelectingHunk:      selectingHunk,
		FirstLine:          firstLine,
		LastLine:           lastLine,
		Diff:               diff,
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

func (gui *Gui) handleStagingEscape(g *gocui.Gui, v *gocui.View) error {
	gui.State.Panels.Staging = nil

	return gui.switchFocus(gui.g, nil, gui.getFilesView())
}

func (gui *Gui) handleStagingPrevLine(g *gocui.Gui, v *gocui.View) error {
	return gui.handleCycleLine(-1)
}

func (gui *Gui) handleStagingNextLine(g *gocui.Gui, v *gocui.View) error {
	return gui.handleCycleLine(1)
}

func (gui *Gui) handleStagingPrevHunk(g *gocui.Gui, v *gocui.View) error {
	return gui.handleCycleHunk(-1)
}

func (gui *Gui) handleStagingNextHunk(g *gocui.Gui, v *gocui.View) error {
	return gui.handleCycleHunk(1)
}

func (gui *Gui) handleCycleHunk(change int) error {
	state := gui.State.Panels.Staging
	newHunk := state.PatchParser.GetHunkContainingLine(state.SelectedLine, change)
	state.SelectedLine = state.PatchParser.GetNextStageableLineIndex(newHunk.FirstLineIndex)
	if state.SelectingHunk {
		state.FirstLine, state.LastLine = newHunk.FirstLineIndex, newHunk.LastLineIndex
	} else {
		state.FirstLine, state.LastLine = state.SelectedLine, state.SelectedLine
	}

	if err := gui.refreshView(); err != nil {
		return err
	}

	return gui.focusSelection(true)
}

func (gui *Gui) handleCycleLine(change int) error {
	state := gui.State.Panels.Staging

	if state.SelectingHunk {
		return gui.handleCycleHunk(change)
	}

	newSelectedLine := state.SelectedLine + change
	if newSelectedLine < 0 {
		newSelectedLine = 0
	} else if newSelectedLine > len(state.PatchParser.PatchLines)-1 {
		newSelectedLine = len(state.PatchParser.PatchLines) - 1
	}

	state.SelectedLine = newSelectedLine

	if state.SelectingLineRange {
		if state.SelectedLine < state.FirstLine {
			state.FirstLine = state.SelectedLine
		} else {
			state.LastLine = state.SelectedLine
		}
	} else {
		state.LastLine = state.SelectedLine
		state.FirstLine = state.SelectedLine
	}

	if err := gui.refreshView(); err != nil {
		return err
	}

	return gui.focusSelection(false)
}

func (gui *Gui) refreshView() error {
	state := gui.State.Panels.Staging

	colorDiff := state.PatchParser.Render(state.FirstLine, state.LastLine)

	mainView := gui.getMainView()
	mainView.Highlight = true
	mainView.Wrap = false

	gui.g.Update(func(*gocui.Gui) error {
		return gui.setViewContent(gui.g, gui.getMainView(), colorDiff)
	})

	return nil
}

// focusSelection works out the best focus for the staging panel given the
// selected line and size of the hunk
func (gui *Gui) focusSelection(includeCurrentHunk bool) error {
	stagingView := gui.getMainView()
	state := gui.State.Panels.Staging

	_, viewHeight := stagingView.Size()
	bufferHeight := viewHeight - 1
	_, origin := stagingView.Origin()

	firstLine := state.SelectedLine
	lastLine := state.SelectedLine

	if includeCurrentHunk {
		hunk := state.PatchParser.GetHunkContainingLine(state.SelectedLine, 0)
		firstLine = hunk.FirstLineIndex
		lastLine = hunk.LastLineIndex
	}

	margin := 0 // we may want to have a margin in place to show context  but right now I'm thinking we keep this at zero

	var newOrigin int
	if firstLine-origin < margin {
		newOrigin = firstLine - margin
	} else if lastLine-origin > bufferHeight-margin {
		newOrigin = lastLine - bufferHeight + margin
	} else {
		newOrigin = origin
	}

	gui.g.Update(func(*gocui.Gui) error {
		if err := stagingView.SetOrigin(0, newOrigin); err != nil {
			return err
		}

		return stagingView.SetCursor(0, state.SelectedLine-newOrigin)
	})

	return nil
}

func (gui *Gui) handleStageSelection(g *gocui.Gui, v *gocui.View) error {
	return gui.applySelection(false)
}

func (gui *Gui) handleResetSelection(g *gocui.Gui, v *gocui.View) error {
	return gui.applySelection(true)
}

func (gui *Gui) applySelection(reverse bool) error {
	state := gui.State.Panels.Staging

	file, err := gui.getSelectedFile(gui.g)
	if err != nil {
		return err
	}

	patch := git.GeneratePatchFromDiff(gui.Log, file.Name, state.Diff, state.FirstLine, state.LastLine, reverse)

	// for logging purposes
	ioutil.WriteFile("patch.diff", []byte(patch), 0600)

	if patch == "" {
		return nil
	}

	// apply the patch then refresh this panel
	// create a new temp file with the patch, then call git apply with that patch
	_, err = gui.GitCommand.ApplyPatch(patch, false, !reverse)
	if err != nil {
		return err
	}

	state.SelectingLineRange = false

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
	state.SelectingLineRange = !state.SelectingLineRange
	state.SelectingHunk = false
	state.FirstLine = state.SelectedLine
	state.LastLine = state.SelectedLine

	return gui.refreshView()
}

func (gui *Gui) handleToggleSelectHunk(g *gocui.Gui, v *gocui.View) error {
	state := gui.State.Panels.Staging

	state.SelectingHunk = !state.SelectingHunk
	state.SelectingLineRange = false

	// if we're no longer selecting a hunk, reset the line number and refresh
	if !state.SelectingHunk {
		state.FirstLine, state.LastLine = state.SelectedLine, state.SelectedLine
	} else {
		selectedHunk := state.PatchParser.GetHunkContainingLine(state.SelectedLine, 0)
		state.FirstLine, state.LastLine = selectedHunk.FirstLineIndex, selectedHunk.LastLineIndex
	}

	if err := gui.refreshView(); err != nil {
		return err
	}

	return gui.focusSelection(state.SelectingHunk)
}
