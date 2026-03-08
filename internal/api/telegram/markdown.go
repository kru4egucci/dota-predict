package telegram

import (
	"regexp"
	"strings"
)

var (
	reCodeBlock   = regexp.MustCompile("(?s)```\\w*\n?(.*?)```")
	reInlineCode  = regexp.MustCompile("`([^`]+)`")
	reBold        = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reItalic      = regexp.MustCompile(`(?:^|[^*])\*([^*]+?)\*(?:[^*]|$)`)
	reUnderItalic = regexp.MustCompile(`(?:^|\s)_([^_]+?)_(?:\s|$|[.,!?])`)
	reHeading     = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	reHR          = regexp.MustCompile(`(?m)^---+\s*$`)
)

// MDToTelegramHTML converts Markdown text to Telegram-compatible HTML.
// Handles: headings, bold, italic, inline code, code blocks, HTML escaping.
func MDToTelegramHTML(md string) string {
	// 1. Escape HTML entities first (before we add our own tags).
	s := strings.ReplaceAll(md, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")

	// 2. Code blocks (``` ... ```) → <pre>
	s = reCodeBlock.ReplaceAllString(s, "<pre>$1</pre>")

	// 3. Inline code → <code>
	s = reInlineCode.ReplaceAllString(s, "<code>$1</code>")

	// 4. Bold **text** → <b>text</b>
	s = reBold.ReplaceAllString(s, "<b>$1</b>")

	// 5. Headings # ... → bold line
	s = reHeading.ReplaceAllString(s, "<b>$1</b>")

	// 6. Italic *text* (single asterisk, not inside bold)
	s = reItalic.ReplaceAllStringFunc(s, func(match string) string {
		// Preserve leading/trailing chars that aren't the asterisks.
		start := 0
		end := len(match)
		if match[0] != '*' {
			start = 1
		}
		if match[end-1] != '*' {
			end = end - 1
		}
		inner := match[start+1 : end-1]
		prefix := match[:start]
		suffix := match[end:]
		return prefix + "<i>" + inner + "</i>" + suffix
	})

	// 7. _italic_ → <i>
	s = reUnderItalic.ReplaceAllStringFunc(s, func(match string) string {
		start := 0
		end := len(match)
		if match[0] != '_' {
			start = 1
		}
		for end > 0 && match[end-1] != '_' {
			end--
		}
		inner := match[start+1 : end-1]
		prefix := match[:start]
		suffix := match[end:]
		return prefix + "<i>" + inner + "</i>" + suffix
	})

	// 8. Remove horizontal rules.
	s = reHR.ReplaceAllString(s, "")

	return strings.TrimSpace(s)
}
