// JJK-themed chrome for the CLI: the wordmark on `domain help` and the small
// technique banners printed at the top of each `domain expansion:` command.
// Colors are ANSI, applied only when isColorTerminal says the output stream
// wants them — plain terminals and piped output still get the plain art.
package main

import (
	"fmt"
	"strings"
)

// ANSI colors for the banners, kept separate from diag's diagnostic palette
// (diag.go) since these paint CLI chrome, not error output.
const (
	jjkReset   = "\x1b[0m"
	jjkBold    = "\x1b[1m"
	jjkBlue    = "\x1b[38;5;69m"  // Gojo — Limitless / Infinity
	jjkCyan    = "\x1b[38;5;51m"  // Six Eyes
	jjkPurple  = "\x1b[38;5;135m" // Hollow Purple (blue + red)
	jjkCrimson = "\x1b[38;5;197m" // Sukuna — Malevolent Shrine
	jjkGreen   = "\x1b[38;5;84m"  // Reverse Cursed Technique
	jjkYellow  = "\x1b[38;5;220m" // cursed energy sensing
	jjkWhite   = "\x1b[1;97m"     // Black Flash
)

func paintJJK(color bool, code, s string) string {
	if !color {
		return s
	}
	return code + s + jjkReset
}

// domainLogo is the "DOMAIN" wordmark.
var domainLogo = []string{
	`██████╗  ██████╗ ███╗   ███╗ █████╗ ██╗███╗   ██╗`,
	`██╔══██╗██╔═══██╗████╗ ████║██╔══██╗██║████╗  ██║`,
	`██║  ██║██║   ██║██╔████╔██║███████║██║██╔██╗ ██║`,
	`██║  ██║██║   ██║██║╚██╔╝██║██╔══██║██║██║╚██╗██║`,
	`██████╔╝╚██████╔╝██║ ╚═╝ ██║██║  ██║██║██║ ╚████║`,
	`╚═════╝  ╚═════╝ ╚═╝     ╚═╝╚═╝  ╚═╝╚═╝╚═╝  ╚═══╝`,
}

// domainLogoGradient runs blue (Limitless) through red (Sukuna) into purple —
// a nod to Hollow Purple, which is Gojo's blue and red merged into one.
var domainLogoGradient = []string{jjkBlue, jjkCyan, jjkCrimson, jjkCrimson, jjkPurple, jjkPurple}

// domainBanner renders the wordmark plus the JJK tagline for `domain help`.
func domainBanner(color bool) string {
	var b strings.Builder
	for i, line := range domainLogo {
		b.WriteString(paintJJK(color, domainLogoGradient[i], line))
		b.WriteByte('\n')
	}
	b.WriteString(paintJJK(color, jjkBold, "      「 領域展開 — Domain Expansion 」"))
	b.WriteString("\n\n")
	return b.String()
}

// technique is the flavor text for one `expansion:` command's banner.
type technique struct {
	kanji string
	name  string
	tag   string
	color string
}

// expansionTechniques maps each expansion command to the JJK technique that
// best matches what it does.
var expansionTechniques = map[string]technique{
	"diagnosis":       {"六眼", "SIX EYES", "nothing escapes perception.", jjkCyan},
	"lint":            {"呪力感知", "CURSED ENERGY SENSING", "even the faintest flaw radiates.", jjkYellow},
	"fix":             {"反転術式", "REVERSE CURSED TECHNIQUE", "negative turned positive — wounds close.", jjkGreen},
	"optimize":        {"黒閃", "BLACK FLASH", "struck within 0.000001 seconds of convergence.", jjkWhite},
	"maximum compile": {"領域展開", "DOMAIN EXPANSION", "guaranteed hit.", jjkCrimson},
	"documentation":   {"呪術高専", "JUJUTSU HIGH ARCHIVES", "everything a sorcerer needs to know.", jjkPurple},
}

// expansionBanner renders the small technique banner for one expansion
// command. Unknown commands render nothing.
func expansionBanner(cmd string, color bool) string {
	t, ok := expansionTechniques[cmd]
	if !ok {
		return ""
	}
	rule := strings.Repeat("━", 56)
	var b strings.Builder
	b.WriteString(paintJJK(color, t.color, rule))
	b.WriteByte('\n')
	b.WriteString(paintJJK(color, jjkBold+t.color, fmt.Sprintf("  %s・%s", t.kanji, t.name)))
	b.WriteByte('\n')
	b.WriteString(paintJJK(color, t.color, "  "+t.tag))
	b.WriteByte('\n')
	b.WriteString(paintJJK(color, t.color, rule))
	b.WriteByte('\n')
	return b.String()
}
