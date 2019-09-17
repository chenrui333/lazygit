package git

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

var headerRegexp = regexp.MustCompile("(?m)^@@ -(\\d+)[^\\+]+\\+(\\d+)[^@]+@@(.*)$")

type patchHunk struct {
	oldStart       int
	oldLength      int
	newStart       int
	newLength      int
	heading        string
	firstLineIndex int
	lastLineIndex  int
	bodyLines      []string
}

func newHunk(header string, body string, firstLineIndex int) *patchHunk {
	match := headerRegexp.FindStringSubmatch(header)
	oldStart := mustConvertToInt(match[1])
	newStart := mustConvertToInt(match[2])
	heading := match[3]

	bodyLines := withoutEmptyStrings(strings.SplitAfter(header+body, "\n"))[1:] // dropping the header line

	return &patchHunk{
		oldStart:       oldStart,
		newStart:       newStart,
		heading:        heading,
		firstLineIndex: firstLineIndex,
		lastLineIndex:  firstLineIndex + len(bodyLines),
		bodyLines:      bodyLines,
	}
}

func (hunk *patchHunk) updateLinesForRange(reverse bool, firstLineIndex int, lastLineIndex int) {
	skippedIndex := -1
	newLines := []string{}

	lineIndex := hunk.firstLineIndex
	for _, line := range hunk.bodyLines {
		lineIndex++ // incrementing here because our lines don't include the header line
		firstChar, content := line[:1], line[1:]

		if firstChar == " " || (firstChar == "\\" && skippedIndex != lineIndex) {
			newLines = append(newLines, line)
			continue
		}

		newFirstChar := firstChar
		if reverse {
			if firstChar == "+" {
				newFirstChar = "-"
			} else if firstChar == "-" {
				newFirstChar = "+"
			}
		}

		isLineInsideRange := (firstLineIndex <= lineIndex && lineIndex <= lastLineIndex)
		if isLineInsideRange {
			newLines = append(newLines, newFirstChar+content)
			continue
		}

		if newFirstChar == "+" {
			// we don't want to include the 'newline at end of file' line if it involves an addition we're not including
			skippedIndex = lineIndex + 1
		} else if newFirstChar == "-" {
			// because we're not deleting this line anymore we'll include it as context
			newLines = append(newLines, " "+content)
		}
	}

	// overwrite old lines with new ones
	hunk.bodyLines = newLines
}

func (hunk *patchHunk) formatHeader(oldLength int, newLength int) string {
	return fmt.Sprintf("@@ -%d,%d +%d,%d @@%s\n", hunk.oldStart, oldLength, hunk.newStart, newLength, hunk.heading)
}

func (hunk *patchHunk) updatedHeader(startOffset int, reverse bool) (int, string, bool) {
	additions := 0
	deletions := 0
	contexts := 0

	for _, line := range hunk.bodyLines {
		switch line[:1] {
		case "+":
			additions++
		case "-":
			deletions++
		case " ":
			contexts++
		}
	}

	if additions == 0 && deletions == 0 {
		// if nothing has changed we just return nothing
		return startOffset, "", false
	}

	if reverse {
		hunk.oldStart = hunk.newStart
	}

	oldLength := contexts + deletions
	newLength := contexts + additions

	var newStartOffset int
	// if the hunk went from zero to positive length, we need to increment the starting point by one
	// if the hunk went from positive to zero length, we need to decrement the starting point by one
	if oldLength == 0 {
		newStartOffset = 1
	} else if newLength == 0 {
		newStartOffset = -1
	} else {
		newStartOffset = 0
	}

	hunk.newStart = hunk.oldStart + startOffset + newStartOffset

	startOffset += newLength - oldLength
	formattedHeader := hunk.formatHeader(oldLength, newLength)
	return startOffset, formattedHeader, true
}

func withoutEmptyStrings(strs []string) []string {
	output := []string{}
	for _, str := range strs {
		if str != "" {
			output = append(output, str)
		}
	}
	return output
}

func mustConvertToInt(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		panic(err)
	}
	return i
}

func getHunksFromDiff(diff string) []*patchHunk {
	headers := headerRegexp.FindAllString(diff, -1)
	bodies := headerRegexp.Split(diff, -1)[1:] // discarding top bit

	headerFirstLineIndices := []int{}
	for lineIndex, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "@@ -") {
			headerFirstLineIndices = append(headerFirstLineIndices, lineIndex)
		}
	}

	hunks := make([]*patchHunk, len(headers))
	for index, header := range headers {
		hunks[index] = newHunk(header, bodies[index], headerFirstLineIndices[index])
	}

	return hunks
}

type PatchGenerator struct {
	Log      *logrus.Entry
	filename string
	hunks    []*patchHunk
}

func NewPatchGenerator(log *logrus.Entry, filename string, diffText string) *PatchGenerator {
	return &PatchGenerator{
		Log:      log,
		filename: filename,
		hunks:    getHunksFromDiff(diffText),
	}
}

func (d *PatchGenerator) GeneratePatch(firstLineIndex int, lastLineIndex int, reverse bool) string {
	// step one is getting only those hunks which we care about
	hunksInRange := []*patchHunk{}
	for _, hunk := range d.hunks {
		if hunk.lastLineIndex >= firstLineIndex && hunk.firstLineIndex <= lastLineIndex {
			hunksInRange = append(hunksInRange, hunk)
		}
	}

	// step two is updating the contents of the hunks and maintaining the additions/deletions in the hunks
	for _, hunk := range hunksInRange {
		hunk.updateLinesForRange(reverse, firstLineIndex, lastLineIndex)
	}

	// step 3 is collecting all the hunks with new headers
	startOffset := 0
	formattedHunks := ""
	var formattedHeader string
	var ok bool
	for _, hunk := range hunksInRange {
		startOffset, formattedHeader, ok = hunk.updatedHeader(startOffset, reverse)
		if ok {
			formattedHunks += formattedHeader + strings.Join(hunk.bodyLines, "")
		}
	}

	if formattedHunks == "" {
		return ""
	}

	fileHeader := fmt.Sprintf("--- a/%s\n+++ b/%s\n", d.filename, d.filename)

	return fileHeader + formattedHunks
}

func GeneratePatchFromDiff(log *logrus.Entry, filename string, diffText string, firstLineIndex int, lastLineIndex int, reverse bool) string {
	p := NewPatchGenerator(log, filename, diffText)
	return p.GeneratePatch(firstLineIndex, lastLineIndex, reverse)
}
