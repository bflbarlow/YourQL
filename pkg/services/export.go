package services

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"

	"YourQL/pkg/models"

	htmltok "golang.org/x/net/html"
)

// ── HTML Export ──────────────────────────────────────────────────────────────

// BuildConversationHTML generates a standalone HTML document from conversation
// messages. The output is self-contained with embedded CSS (light-mode theme).
func BuildConversationHTML(
	conversation *models.Conversation,
	messages []*models.ConversationMessage,
	providerName string,
	dataSourceName string,
) string {
	title := "Untitled"
	if conversation.Title != nil && *conversation.Title != "" {
		title = *conversation.Title
	}

	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>`)
	sb.WriteString(html.EscapeString("YourQL \u2014 " + title))
	sb.WriteString(`</title>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
    font-size: 13px;
    line-height: 1.6;
    color: #000;
    background: #fff;
    max-width: 900px;
    margin: 0 auto;
    padding: 2rem 1.5rem;
  }
  .export-header {
    border-bottom: 2px solid #e0e0e0;
    padding-bottom: 1rem;
    margin-bottom: 2rem;
  }
  .export-header h1 { font-size: 1.5rem; font-weight: 600; margin-bottom: 0.25rem; }
  .export-header p { font-size: 0.85rem; color: #666; }
  .message { margin-bottom: 1.5rem; }
  .message-user { display: flex; justify-content: flex-end; }
  .message-user .msg-bubble {
    background: #0288d1 !important;
    color: #fff !important;
    padding: 0.75rem 1.25rem;
    border-radius: 6px;
    max-width: 85%;
    white-space: pre-wrap;
  }
  .message-assistant .msg-bubble {
    background: #f9f9f9;
    border: 1px solid #e0e0e0;
    padding: 1.25rem 1.5rem;
    border-radius: 6px;
  }
  .message-system .msg-bubble {
    text-align: center;
    color: #999;
    font-size: 0.8rem;
    padding: 0.25rem 0;
  }
  .message-time { font-size: 0.75rem; color: #999; margin-top: 0.25rem; padding: 0 0.25rem; }
  .message-user .message-time { text-align: right; }
  .msg-bubble h2 { font-size: 1.2rem; margin: 1rem 0 0.5rem; }
  .msg-bubble h3 { font-size: 1.05rem; margin: 0.75rem 0 0.5rem; }
  .msg-bubble p { margin-bottom: 0.5rem; }
  .msg-bubble ul, .msg-bubble ol { padding-left: 1.5rem; margin: 0.5rem 0; }
  .msg-bubble li { margin-bottom: 0.25rem; }
  .msg-bubble strong { font-weight: 600; }
  .msg-bubble em { font-style: italic; }
  .msg-bubble hr { border: none; border-top: 1px solid #e0e0e0; margin: 1rem 0; }
  .msg-bubble a { color: #0288d1; }
  .msg-bubble pre {
    background: #1e1e1e;
    color: #d4d4d4;
    padding: 1rem;
    border-radius: 6px;
    overflow-x: auto;
    font-family: 'Courier New', monospace;
    font-size: 0.85em;
    margin: 0.5rem 0;
  }
  .msg-bubble code {
    background: #f0f0f0;
    padding: 2px 6px;
    border-radius: 4px;
    font-family: 'Courier New', monospace;
    font-size: 0.9em;
  }
  .msg-bubble table {
    border-collapse: collapse;
    width: 100%;
    margin: 0.5rem 0;
    font-size: 0.85em;
  }
  .msg-bubble th, .msg-bubble td {
    border: 1px solid #e0e0e0;
    padding: 8px 12px;
    text-align: left;
  }
  .msg-bubble th { background: #f5f5f5; font-weight: 600; }
  .results-details { margin-top: 0.5rem; }
  .results-details summary {
    cursor: pointer;
    color: #666;
    font-size: 0.85rem;
    padding: 4px 8px;
    background: #f5f5f5;
    border-radius: 4px;
    display: inline-block;
  }
  .results-toolbar {
    display: flex; align-items: center; gap: 0.5rem;
    padding: 6px 10px;
    background: #f8f9fa;
    border: 1px solid #e0e0e0;
    border-radius: 6px;
    margin-bottom: 0.5rem;
  }
  .row-count { font-size: 0.85rem; color: #666; font-weight: 500; }
  .chart-placeholder {
    background: #f5f5f5;
    border: 1px dashed #ccc;
    border-radius: 6px;
    padding: 1rem;
    margin-top: 0.5rem;
    color: #666;
    font-size: 0.85rem;
    text-align: center;
  }
  @media print {
    body { font-size: 12px; }
    .message { break-inside: avoid; }
  }
</style>
</head>
<body>
`)

	// Header
	dateStr := conversation.CreatedAt.Format("January 2, 2006")
	sb.WriteString(`<div class="export-header">`)
	sb.WriteString(fmt.Sprintf("<h1>%s</h1>\n", html.EscapeString(title)))
	metaParts := []string{dateStr}
	if providerName != "" {
		metaParts = append(metaParts, providerName)
	}
	if dataSourceName != "" {
		metaParts = append(metaParts, dataSourceName)
	}
	sb.WriteString(fmt.Sprintf("<p>%s</p>\n", html.EscapeString(strings.Join(metaParts, " \u00b7 "))))
	sb.WriteString(`</div>`)
	sb.WriteString("\n")

	// Messages — filter out tech-detail rounds when TechDetails is off
	for _, msg := range messages {
		role := msg.Role

		// Skip exploration messages (tech-detail rounds) unless TechDetails is enabled
		if role == "exploration" && !conversation.TechDetails {
			continue
		}
		if role == "exploration" {
			role = "assistant"
		}

		content := msg.Content
		if role == "assistant" && strings.TrimSpace(content) != "" {
			content = replaceCSSVars(content)
			content = processContentForExport(content, msg.Metadata)
		}

		timeStr := msg.CreatedAt.Format("Jan 2, 2006 3:04 PM")

		switch role {
		case "user":
			sb.WriteString(`<div class="message message-user">`)
			sb.WriteString("<div>")
			sb.WriteString(fmt.Sprintf(`<div class="msg-bubble">%s</div>`, html.EscapeString(content)))
			sb.WriteString(fmt.Sprintf(`<div class="message-time">%s</div>`, html.EscapeString(timeStr)))
			sb.WriteString("</div></div>\n")
		case "assistant":
			sb.WriteString(`<div class="message message-assistant">`)
			sb.WriteString("<div>")
			sb.WriteString(fmt.Sprintf(`<div class="msg-bubble">%s</div>`, content))
			sb.WriteString(fmt.Sprintf(`<div class="message-time">%s</div>`, html.EscapeString(timeStr)))
			sb.WriteString("</div></div>\n")
		case "system":
			sb.WriteString(`<div class="message message-system">`)
			sb.WriteString(fmt.Sprintf(`<div class="msg-bubble">%s</div>`, html.EscapeString(content)))
			sb.WriteString("</div>\n")
		}
	}

	sb.WriteString("\n</body>\n</html>\n")
	return sb.String()
}

// processContentForExport handles chart placeholders in exported HTML content.
func processContentForExport(contentHTML string, metadata *string) string {
	chartType := extractChartType(metadata)
	if chartType == "" {
		return contentHTML
	}
	return contentHTML + fmt.Sprintf(
		`<div class="chart-placeholder">📊 Chart: %s — view in the app for the interactive visualization</div>`,
		html.EscapeString(chartType),
	)
}

// ── Markdown Export ──────────────────────────────────────────────────────────

// BuildConversationMarkdown generates a Markdown document from conversation messages.
func BuildConversationMarkdown(
	conversation *models.Conversation,
	messages []*models.ConversationMessage,
	providerName string,
	dataSourceName string,
) string {
	title := "Untitled"
	if conversation.Title != nil && *conversation.Title != "" {
		title = *conversation.Title
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# YourQL Conversation: %s\n\n", title))

	dateStr := conversation.CreatedAt.Format("January 2, 2006")
	metaParts := []string{dateStr}
	if providerName != "" {
		metaParts = append(metaParts, providerName)
	}
	if dataSourceName != "" {
		metaParts = append(metaParts, dataSourceName)
	}
	sb.WriteString("**" + strings.Join(metaParts, " \u00b7 ") + "**\n\n---\n\n")

	for _, msg := range messages {
		// Skip exploration messages (tech-detail rounds) unless TechDetails is enabled
		if msg.Role == "exploration" && !conversation.TechDetails {
			continue
		}
		switch msg.Role {
		case "user":
			sb.WriteString("## User\n")
			lines := strings.Split(msg.Content, "\n")
			for _, line := range lines {
				sb.WriteString("> " + line + "\n")
			}
			sb.WriteString("\n")
		case "assistant", "exploration":
			sb.WriteString("## Assistant\n")
			md := convertHTMLToMarkdown(msg.Content, msg.Metadata)
			sb.WriteString(md)
			sb.WriteString("\n")
		case "system":
			sb.WriteString(fmt.Sprintf("*%s*\n\n", msg.Content))
		}
	}

	return sb.String()
}

// ── HTML-to-Markdown Converter ───────────────────────────────────────────────

// convertHTMLToMarkdown converts YourQL's known HTML to Markdown via a tokenizer.
func convertHTMLToMarkdown(htmlContent string, metadata *string) string {
	if htmlContent == "" {
		return ""
	}

	chartType := extractChartType(metadata)
	if !strings.Contains(htmlContent, "<") {
		return htmlContent
	}

	// Pre-process: strip wrapper divs that obscure inner content.
	content := regexp.MustCompile(`<div class="markdown-content">\s*`).ReplaceAllString(htmlContent, "")
	content = regexp.MustCompile(`</div>\s*$`).ReplaceAllString(content, "")
	content = regexp.MustCompile(`<div class="results-toolbar"[^>]*>.*?</div>`).ReplaceAllString(content, "")
	content = regexp.MustCompile(`<div class="results-card"[^>]*>`).ReplaceAllString(content, "")
	content = regexp.MustCompile(`<div class="table-container"[^>]*>`).ReplaceAllString(content, "")

	var sb strings.Builder
	tokenizer := htmltok.NewTokenizer(strings.NewReader(content))

	var (
		inPre         bool
		inTable       bool
		inThead       bool
		tableCols     []string
		tableRows     [][]string
		paraBuf       strings.Builder
		listStack     []int
		listCounter   []int
		currentCells  []string
		summaryBuf    strings.Builder
		inSummary     bool
		linkURL       string
		codeBlockBuf  strings.Builder
		codeBlockLang string
		cellBuf       strings.Builder
	)

	flushPara := func() {
		t := strings.TrimSpace(paraBuf.String())
		if t != "" {
			sb.WriteString(t)
			sb.WriteString("\n\n")
		}
		paraBuf.Reset()
	}

	flushCodeBlock := func() {
		code := strings.TrimRight(codeBlockBuf.String(), "\n")
		if code != "" {
			sb.WriteString("```")
			if codeBlockLang != "" {
				sb.WriteString(codeBlockLang)
			}
			sb.WriteString("\n")
			sb.WriteString(code)
			sb.WriteString("\n```\n\n")
		}
		codeBlockBuf.Reset()
		codeBlockLang = ""
	}

	flushTable := func() {
		if len(tableCols) > 0 || len(tableRows) > 0 {
			writeMarkdownTable(&sb, tableCols, tableRows)
		}
		tableCols = nil
		tableRows = nil
	}

	addCell := func() {
		text := strings.TrimSpace(cellBuf.String())
		if inThead {
			tableCols = append(tableCols, text)
		} else {
			currentCells = append(currentCells, text)
		}
		cellBuf.Reset()
	}

	addRow := func() {
		// Flush last cell if not yet flushed
		text := strings.TrimSpace(cellBuf.String())
		if text != "" || len(currentCells) > 0 {
			if text != "" {
				currentCells = append(currentCells, text)
			}
			cellBuf.Reset()
		}
		if len(currentCells) > 0 {
			if inThead {
				// In thead, cells are header cells
				tableCols = append(tableCols, currentCells...)
			} else {
				tableRows = append(tableRows, currentCells)
			}
		}
		currentCells = nil
	}

	for {
		tt := tokenizer.Next()
		switch tt {
		case htmltok.ErrorToken:
			flushPara()
			flushTable()
			flushCodeBlock()
			if chartType != "" {
				sb.WriteString(fmt.Sprintf("*[Chart: %s — view in the app to see the interactive visualization]*\n\n", chartType))
			}
			return strings.TrimSpace(sb.String())

		case htmltok.TextToken:
			text := string(tokenizer.Text())
			if inPre {
				codeBlockBuf.WriteString(text)
			} else if inSummary {
				summaryBuf.WriteString(text)
			} else if inTable {
				cellBuf.WriteString(text)
			} else {
				paraBuf.WriteString(text)
			}

		case htmltok.StartTagToken, htmltok.SelfClosingTagToken:
			tn, hasAttr := tokenizer.TagName()
			tag := string(tn)

			switch tag {
			case "h1":
				flushPara()
				sb.WriteString("# ")
			case "h2":
				flushPara()
				sb.WriteString("## ")
			case "h3":
				flushPara()
				sb.WriteString("### ")
			case "h4":
				flushPara()
				sb.WriteString("#### ")
			case "p":
				flushPara()
			case "br":
				paraBuf.WriteString("\n")
			case "strong", "b":
				paraBuf.WriteString("**")
			case "em", "i":
				paraBuf.WriteString("*")
			case "a":
				if hasAttr {
					for {
						key, val, more := tokenizer.TagAttr()
						if string(key) == "href" {
							linkURL = string(val)
						}
						if !more {
							break
						}
					}
				}
				paraBuf.WriteString("[")
			case "ul":
				flushPara()
				listStack = append(listStack, len(listStack)+1)
			case "ol":
				flushPara()
				level := len(listStack) + 1
				listStack = append(listStack, -level)
				listCounter = append(listCounter, 1)
			case "li":
				depth := len(listStack)
				indent := ""
				if depth > 1 {
					indent = strings.Repeat("  ", depth-1)
				}
				if len(listStack) > 0 {
					last := listStack[len(listStack)-1]
					if last > 0 {
						sb.WriteString(indent + "- ")
					} else {
						idx := -last - 1
						if idx < len(listCounter) {
							sb.WriteString(fmt.Sprintf("%s%d. ", indent, listCounter[idx]))
							listCounter[idx]++
						} else {
							sb.WriteString(indent + "1. ")
						}
					}
				} else {
					sb.WriteString("- ")
				}
			case "code":
				if !inPre {
					paraBuf.WriteString("`")
				} else if hasAttr {
					for {
						key, val, more := tokenizer.TagAttr()
						if string(key) == "class" {
							cls := string(val)
							if strings.Contains(cls, "sql") {
								codeBlockLang = "sql"
							}
						}
						if !more {
							break
						}
					}
				}
			case "pre":
				flushPara()
				inPre = true
				codeBlockBuf.Reset()
				codeBlockLang = ""
			case "table":
				flushPara()
				inTable = true
				tableCols = nil
				tableRows = nil
			case "thead":
				inThead = true
			case "tr":
				addRow()
			case "th", "td":
				addCell()
			case "hr":
				flushPara()
				sb.WriteString("---\n\n")
			case "details":
				flushPara()
				inSummary = false
				summaryBuf.Reset()
			case "summary":
				inSummary = true
			case "img":
				if hasAttr {
					var alt string
					for {
						key, val, more := tokenizer.TagAttr()
						if string(key) == "alt" {
							alt = string(val)
						}
						if !more {
							break
						}
					}
					if alt != "" {
						paraBuf.WriteString("[Image: " + alt + "]")
					}
				}
			}

		case htmltok.EndTagToken:
			tn, _ := tokenizer.TagName()
			tag := string(tn)

			switch tag {
			case "h1", "h2", "h3", "h4", "h5", "h6":
				flushPara()
			case "p":
				flushPara()
			case "strong", "b":
				paraBuf.WriteString("**")
			case "em", "i":
				paraBuf.WriteString("*")
			case "a":
				if linkURL != "" {
					paraBuf.WriteString("](" + linkURL + ")")
					linkURL = ""
				} else {
					paraBuf.WriteString("]()")
				}
			case "code":
				if !inPre {
					paraBuf.WriteString("`")
				}
			case "pre":
				flushCodeBlock()
				inPre = false
			case "ul", "ol":
				if len(listStack) > 0 {
					listStack = listStack[:len(listStack)-1]
				}
				if len(listStack) == 0 {
					sb.WriteString("\n")
				}
			case "li":
				sb.WriteString("\n")
			case "table":
				flushTable()
				inTable = false
			case "thead":
				inThead = false
			case "tr":
				addRow()
			case "th", "td":
				addCell()
			case "details":
				if summaryBuf.Len() > 0 {
					sb.WriteString("> **" + strings.TrimSpace(summaryBuf.String()) + "**\n>\n")
				}
				inSummary = false
			case "summary":
				inSummary = false
			}
		}
	}
}

// writeMarkdownTable writes a GitHub-flavored markdown pipe table.
func writeMarkdownTable(sb *strings.Builder, cols []string, rows [][]string) {
	if len(cols) == 0 && len(rows) == 0 {
		return
	}
	if len(cols) == 0 && len(rows) > 0 {
		cols = rows[0]
		rows = rows[1:]
	}
	if len(cols) == 0 {
		return
	}

	sb.WriteString("| ")
	sb.WriteString(strings.Join(cols, " | "))
	sb.WriteString(" |\n")

	sb.WriteString("|")
	for range cols {
		sb.WriteString(" --- |")
	}
	sb.WriteString("\n")

	for _, row := range rows {
		for len(row) < len(cols) {
			row = append(row, "")
		}
		if len(row) > len(cols) {
			row = row[:len(cols)]
		}
		sb.WriteString("| ")
		sb.WriteString(strings.Join(row, " | "))
		sb.WriteString(" |\n")
	}
	sb.WriteString("\n")
}

// ── CSS Variable Translation ─────────────────────────────────────────────────

// cssVarMap translates YourQL's light-mode CSS custom properties to fixed values
// for standalone HTML export (where the app's stylesheet is not present).
var cssVarMap = map[string]string{
	"--bg-primary":         "#ffffff",
	"--bg-secondary":       "#f5f5f5",
	"--bg-tertiary":        "#f9f9f9",
	"--bg-surface":         "#f8f9fa",
	"--text-primary":       "#000000",
	"--text-secondary":     "#666666",
	"--text-tertiary":      "#999999",
	"--border-primary":     "#e0e0e0",
	"--border-secondary":   "#f0f0f0",
	"--color-accent":       "#0288d1",
	"--color-accent-hover": "#0277bd",
	"--color-accent-light": "rgba(2,136,209,0.1)",
	"--color-accent-border": "rgba(2,136,209,0.3)",
	"--color-success":      "#4caf50",
	"--color-danger":       "#ef5350",
	"--color-error":        "#ef4444",
	"--color-warning":      "#ff9800",
}

var cssVarRe = regexp.MustCompile(`var\((--[a-z-]+)(?:\s*,\s*[^)]+)?\)`)

// replaceCSSVars replaces CSS custom property references (var(--xxx) and
// var(--xxx, fallback)) with their light-mode fixed values so standalone
// HTML exports render correctly without the app's stylesheet.
func replaceCSSVars(htmlContent string) string {
	return cssVarRe.ReplaceAllStringFunc(htmlContent, func(match string) string {
		// match looks like "var(--prop)" or "var(--prop, fallback)"
		// Strip "var(" prefix (4 chars) and trailing ")"
		inner := match[4 : len(match)-1]
		// If there's a comma, use only the part before it
		if idx := strings.IndexByte(inner, ','); idx >= 0 {
			inner = inner[:idx]
		}
		propName := strings.TrimSpace(inner)
		if val, ok := cssVarMap[propName]; ok {
			return val
		}
		return match
	})
}

// ── Chart Metadata ───────────────────────────────────────────────────────────

// extractChartType reads chart_config from message metadata and returns a
// human-readable chart type string. Returns "" if no chart config is present.
func extractChartType(metadata *string) string {
	if metadata == nil || *metadata == "" {
		return ""
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(*metadata), &meta); err != nil {
		return ""
	}
	cfg, ok := meta["chart_config"]
	if !ok {
		return ""
	}
	var chartMap map[string]any
	switch v := cfg.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &chartMap); err != nil {
			return ""
		}
	case map[string]any:
		chartMap = v
	default:
		return ""
	}
	if t, ok := chartMap["type"].(string); ok {
		return t
	}
	return ""
}