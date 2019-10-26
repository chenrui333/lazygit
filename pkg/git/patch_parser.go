package git

import (
	"strings"

	"github.com/fatih/color"
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

type PatchParser struct {
	Log *logrus.Entry
}

// NewPatchParser builds a new branch list builder
func NewPatchParser(log *logrus.Entry) (*PatchParser, error) {
	return &PatchParser{
		Log: log,
	}, nil
}

type line struct {
	kind    int
	content string
}

func (l *line) coloured(selected bool) string {
	if len(l.content) == 0 {
		return ""
	}

	var attr color.Attribute
	switch l.kind {
	case PATCH_HEADER:
		attr = color.FgHiWhite
	case HUNK_HEADER:
		attr = color.FgCyan
	case ADDITION:
		attr = color.FgGreen
	case DELETION:
		attr = color.FgRed
	case CONTEXT:
		attr = color.FgWhite
	case NEWLINE_MESSAGE:
		attr = color.FgWhite // TODO: use theme default here
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

func (p *PatchParser) ParsePatch(patch string) ([]int, []int, error) {
	lines := strings.Split(patch, "\n")
	hunkStarts := []int{}
	stageableLines := []int{}
	pastHeader := false
	for index, line := range lines {
		if strings.HasPrefix(line, "@@") {
			pastHeader = true
			hunkStarts = append(hunkStarts, index)
		}
		if pastHeader && (strings.HasPrefix(line, "-") || strings.HasPrefix(line, "+")) {
			stageableLines = append(stageableLines, index)
		}
	}
	p.Log.WithField("staging", "staging").Info(stageableLines)
	return hunkStarts, stageableLines, nil
}
