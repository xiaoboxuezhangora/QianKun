package injection

import (
	"fmt"
	"strings"
)

const (
	StartMarker = "<!-- QIANKUN:START -->"
	EndMarker   = "<!-- QIANKUN:END -->"
)

type Zone struct {
	Content      string
	Start        int
	End          int
	ContentStart int
	ContentEnd   int
}

func ExtractZones(input string) ([]Zone, error) {
	var zones []Zone
	cursor := 0

	for cursor < len(input) {
		nextStart := strings.Index(input[cursor:], StartMarker)
		nextEnd := strings.Index(input[cursor:], EndMarker)

		if nextStart == -1 {
			if nextEnd != -1 {
				return nil, fmt.Errorf("qiankun injection end marker at byte %d has no matching start marker", cursor+nextEnd)
			}
			return zones, nil
		}

		if nextEnd != -1 && nextEnd < nextStart {
			return nil, fmt.Errorf("qiankun injection end marker at byte %d appears before the next start marker", cursor+nextEnd)
		}

		start := cursor + nextStart
		contentStart := start + len(StartMarker)
		endOffset := strings.Index(input[contentStart:], EndMarker)
		if endOffset == -1 {
			return nil, fmt.Errorf("qiankun injection zone starting at byte %d is missing end marker %q", start, EndMarker)
		}

		contentEnd := contentStart + endOffset
		end := contentEnd + len(EndMarker)
		zones = append(zones, Zone{
			Content:      input[contentStart:contentEnd],
			Start:        start,
			End:          end,
			ContentStart: contentStart,
			ContentEnd:   contentEnd,
		})
		cursor = end
	}

	return zones, nil
}
