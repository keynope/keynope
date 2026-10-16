package main

import (
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"unicode"
)

type documentCommentLimits struct {
	Count           int
	Bytes, Expanded int64
}

var defaultDocumentCommentLimits = documentCommentLimits{Count: 100_000, Bytes: 256 << 20, Expanded: 512 << 20}

// A streaming preflight: do not allocate one match/slice per comment or inflate
// compressed archives into byte buffers merely to count their aggregate size.
// Fenced examples follow the parser's exact contract. Nested open markers are
// candidates too: the existing metadata codecs do not implement HTML blocks.
func validateDocumentCommentBudget(text string, limits documentCommentLimits) error {
	var rawBytes, expandedBytes int64
	count := 0
	var inflater *gzip.Reader
	fence := markdownFenceState{}
	charge := func(n int) error {
		if int64(n) > limits.Bytes-rawBytes {
			return fmt.Errorf("document metadata comments exceed %d bytes", limits.Bytes)
		}
		rawBytes += int64(n)
		if rawBytes > limits.Expanded-expandedBytes {
			return fmt.Errorf("document metadata comments exceed aggregate expanded budget of %d bytes", limits.Expanded)
		}
		return nil
	}
	scan := func(span string) error {
		additional := strings.Count(span, "<!--")
		if additional > limits.Count-count {
			return fmt.Errorf("document exceeds %d metadata comments", limits.Count)
		}
		count += additional
		for position := 0; position < len(span); {
			first := strings.Index(span[position:], "<!--")
			if first < 0 {
				break
			}
			first += position
			end := strings.Index(span[first+4:], "-->")
			closed := end >= 0
			if closed {
				end += first + 4
			} else {
				end = len(span)
			}
			last := first
			for search := first; search < end; {
				last = search
				next := strings.Index(span[search+4:end], "<!--")
				if next < 0 {
					break
				}
				search += 4 + next
			}
			stop := end
			if closed {
				stop += 3
			}
			if err := charge(stop - first); err != nil {
				return err
			}
			if !closed {
				break
			}
			// Only the last opener can begin a well-formed base64 envelope ending
			// at this closer: earlier candidates contain '<', outside its alphabet.
			n, err := expandedDocumentCommentBytes(span[last:stop], limits.Expanded-rawBytes-expandedBytes, &inflater)
			if err != nil {
				return err
			}
			expandedBytes += n
			position = stop
		}
		return nil
	}
	spanStart := 0
	for start := 0; start < len(text); {
		end := strings.IndexByte(text[start:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += start + 1
		}
		wasCode := fence.width > 0
		boundary := fence.boundary(text[start:end])
		if boundary && !wasCode {
			if err := scan(text[spanStart:start]); err != nil {
				return err
			}
		} else if boundary && fence.width == 0 {
			spanStart = end
		}
		start = end
	}
	if fence.width == 0 {
		return scan(text[spanStart:])
	}
	return nil
}

func expandedDocumentCommentBytes(comment string, remaining int64, inflater **gzip.Reader) (int64, error) {
	body := strings.TrimSpace(comment[4 : len(comment)-3])
	cut := strings.IndexFunc(body, unicode.IsSpace)
	if cut < 0 {
		return 0, nil
	}
	name, rest := body[:cut], strings.TrimSpace(body[cut:])
	var individual int64
	switch name {
	case "keynope-assets":
		individual = maxDeckAssetArchiveBytes
	case "keynope-objects":
		individual = maxStructuredObjectBytes
	case "keynope-engagement-results":
		individual = maxEngagementResultBytes
	default:
		return 0, nil
	}
	if !strings.HasPrefix(rest, "version=1") {
		return 0, nil
	}
	rest = rest[len("version=1"):]
	if rest == "" || !unicode.IsSpace(rune(rest[0])) {
		return 0, nil
	}
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, "base64:") {
		return 0, nil
	}
	encoded := strings.TrimSpace(rest[len("base64:"):])
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded))
	reader := *inflater
	var err error
	if reader == nil {
		reader, err = gzip.NewReader(decoder)
	} else {
		err = reader.Reset(decoder)
	}
	if err != nil {
		return 0, nil
	} // The owning codec supplies its precise format error.
	*inflater = reader
	defer reader.Close()
	limit := remaining
	if individual < limit {
		limit = individual
	}
	// Charge even partial output. Bad CRC/base64 diagnostics remain owned by
	// the codec; aggregate expansion/work still cannot bypass this preflight.
	n, _ := io.Copy(io.Discard, io.LimitReader(reader, limit+1))
	if n > remaining {
		return 0, fmt.Errorf("document metadata comments exceed aggregate expanded budget")
	}
	if n > individual {
		return 0, fmt.Errorf("%s metadata exceeds decompressed size limit", name)
	}
	return n, nil
}
