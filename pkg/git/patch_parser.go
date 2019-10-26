package git

import (
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/jesseduffield/lazygit/pkg/theme"
	"github.com/jesseduffield/lazygit/pkg/utils"
	"github.com/sirupsen/logrus"
)

const (
	PATCH_HEADER = iota
	HUNK_HEADER
	ADDITION
	DELETION
	CONTEXT
	NEWLINE_MESSAGE
)

// the job of this file is to parse a diff, find out where the hunks begin and end, which lines are stageable, and how to find the next hunk from the current position or the next stageable line from the current position.

type PatchLine struct {
	Kind    int
	Content string // something like '+ hello' (note the first character is not removed)
}

type PatchParser struct {
	Log            *logrus.Entry
	PatchLines     []*PatchLine
	HunkStarts     []int
	StageableLines []int // rename to mention we're talking about indexes
}

// NewPatchParser builds a new branch list builder
func NewPatchParser(log *logrus.Entry, patch string) (*PatchParser, error) {
	hunkStarts, stageableLines, patchLines, err := parsePatch(patch)
	if err != nil {
		return nil, err
	}

	return &PatchParser{
		Log:            log,
		HunkStarts:     hunkStarts,
		StageableLines: stageableLines,
		PatchLines:     patchLines,
	}, nil
}

func (l *PatchLine) render(selected bool) string {
	if len(l.Content) == 0 {
		return ""
	}

	// for hunk headers we need to start off cyan and then use white for the message
	if l.Kind == HUNK_HEADER {
		re := regexp.MustCompile("(@@.*?@@)(.*)")
		match := re.FindStringSubmatch(l.Content)
		return coloredString(color.FgCyan, match[1], selected) + coloredString(theme.DefaultTextColor, match[2], selected)
	}

	var colorAttr color.Attribute
	switch l.Kind {
	case PATCH_HEADER:
		colorAttr = color.Bold
	case ADDITION:
		colorAttr = color.FgGreen
	case DELETION:
		colorAttr = color.FgRed
	default:
		colorAttr = theme.DefaultTextColor
	}

	return coloredString(colorAttr, l.Content, selected)
}

func coloredString(colorAttr color.Attribute, str string, selected bool) string {
	var cl *color.Color
	if selected {
		cl = color.New(colorAttr, color.BgBlue)
	} else {
		cl = color.New(colorAttr)
	}
	return utils.ColoredStringDirect(str, cl)
}

func parsePatch(patch string) ([]int, []int, []*PatchLine, error) {
	lines := strings.Split(patch, "\n")
	hunkStarts := []int{}
	stageableLines := []int{}
	pastFirstHunkHeader := false
	patchLines := make([]*PatchLine, len(lines))
	var lineKind int
	var firstChar string
	for index, line := range lines {
		lineKind = PATCH_HEADER
		firstChar = " "
		if len(line) > 0 {
			firstChar = line[:1]
		}
		if firstChar == "@" {
			pastFirstHunkHeader = true
			hunkStarts = append(hunkStarts, index)
			lineKind = HUNK_HEADER
		} else if pastFirstHunkHeader {
			switch firstChar {
			case "-":
				lineKind = DELETION
				stageableLines = append(stageableLines, index)
			case "+":
				lineKind = ADDITION
				stageableLines = append(stageableLines, index)
			case "\\":
				lineKind = NEWLINE_MESSAGE
			case " ":
				lineKind = CONTEXT
			}
		}
		patchLines[index] = &PatchLine{Kind: lineKind, Content: line}
	}
	return hunkStarts, stageableLines, patchLines, nil
}

// Render returns the coloured string of the diff with any selected lines highlighted
func (p *PatchParser) Render(firstLineIndex int, lastLineIndex int) string {
	renderedLines := make([]string, len(p.PatchLines))
	for index, patchLine := range p.PatchLines {
		selected := index >= firstLineIndex && index <= lastLineIndex
		renderedLines[index] = patchLine.render(selected)
	}
	return strings.Join(renderedLines, "\n")
}
