package agent

import (
	glamouransi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

// markdownStyles keeps Markdown on the terminal's own background. Color is
// reserved for headings, links and code; body text inherits the foreground.
func markdownStyles(theme ThemeVariant, dark bool) glamouransi.StyleConfig {
	cfg := styles.LightStyleConfig
	if dark {
		cfg = styles.DarkStyleConfig
	}
	cfg.Document.Color = nil
	cfg.Document.BlockPrefix = ""
	cfg.Document.BlockSuffix = ""
	cfg.Document.Margin = nil

	bold := true
	for _, h := range []*glamouransi.StyleBlock{
		&cfg.Heading, &cfg.H1, &cfg.H2, &cfg.H3, &cfg.H4, &cfg.H5, &cfg.H6,
	} {
		h.Color = &theme.Primary
		h.BackgroundColor = nil
		h.Bold = &bold
		h.Prefix = ""
		h.Suffix = ""
	}

	// Literal backticks distinguish code even without colors. No padded color
	// tiles: those fragmented sentences and wrapped table cells on light themes.
	cfg.Code = glamouransi.StyleBlock{StylePrimitive: glamouransi.StylePrimitive{
		Prefix: "`", Suffix: "`", Color: &theme.Accent,
	}}
	cfg.CodeBlock.Margin = nil
	cfg.Link.Color = &theme.Accent
	cfg.LinkText.Color = &theme.Accent
	cfg.HorizontalRule.Color = &theme.Muted

	// Keep the table structure light, with a clean intersection at the header.
	// Leave cell text in the terminal foreground instead of tinting whole rows.
	column, row, center := "│", "─", "┼"
	zero := uint(0)
	cfg.Table.ColumnSeparator = &column
	cfg.Table.RowSeparator = &row
	cfg.Table.CenterSeparator = &center
	cfg.Table.Margin = &zero
	return cfg
}
