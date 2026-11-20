package ui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// ProgressBar renders a real-time download/update progress bar in the terminal.
type ProgressBar struct {
	mu        sync.Mutex
	label     string
	status    string
	total     int64
	current   int64
	startTime time.Time
	lastTime  time.Time
	lastBytes int64
	speed     float64 // bytes per second (smoothed)
	finished  bool
}

// gradient colors from cyan → purple → pink (matching Microgravity palette)
var barGradient = []lipgloss.Color{"39", "39", "63", "63", "99", "99", "135", "135", "170", "205"}

var (
	barLabelStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	barPercentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	barStatsStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	barDoneStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	barEmptyChar     = lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render("░")
)

// NewProgressBar creates a new progress bar with a label and known total bytes.
func NewProgressBar(label string, total int64) *ProgressBar {
	now := time.Now()
	return &ProgressBar{
		label:     label,
		total:     total,
		startTime: now,
		lastTime:  now,
	}
}

// SetStatus sets an optional status suffix (e.g. "verifying...").
func (p *ProgressBar) SetStatus(status string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status = status
	p.render()
}

// Label returns the progress bar's label text.
func (p *ProgressBar) Label() string {
	return p.label
}

// Update sets the current byte count and re-renders.
func (p *ProgressBar) Update(current int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(p.lastTime).Seconds()

	if elapsed > 0.3 { // smooth speed calculation every 300ms
		instantSpeed := float64(current-p.lastBytes) / elapsed
		if p.speed == 0 {
			p.speed = instantSpeed
		} else {
			p.speed = p.speed*0.7 + instantSpeed*0.3 // exponential moving average
		}
		p.lastBytes = current
		p.lastTime = now
	}

	p.current = current
	p.render()
}

// Finish marks the bar as 100% complete and prints the final state.
func (p *ProgressBar) Finish() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current = p.total
	p.finished = true
	p.render()
	fmt.Println() // move to next line
}

func (p *ProgressBar) render() {
	if p.total <= 0 {
		// indeterminate mode — just show status
		line := fmt.Sprintf("\r  %s %s",
			barLabelStyle.Render("⬇ "+p.label),
			barStatsStyle.Render(p.status),
		)
		fmt.Fprint(os.Stderr, line+"\033[K") // \033[K clears to end of line
		return
	}

	termWidth := 80
	fd := int(os.Stdout.Fd())
	if term.IsTerminal(fd) {
		if w, _, err := term.GetSize(fd); err == nil && w > 0 {
			termWidth = w
		}
	}

	fraction := float64(p.current) / float64(p.total)
	if fraction > 1 {
		fraction = 1
	}
	percent := int(fraction * 100)

	// Build the bar
	barWidth := termWidth - 60 // leave room for label, stats
	if barWidth < 10 {
		barWidth = 10
	}
	if barWidth > 40 {
		barWidth = 40
	}

	filled := int(fraction * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	// gradient-colored filled portion
	var bar strings.Builder
	for i := 0; i < filled; i++ {
		colorIdx := i * len(barGradient) / barWidth
		if colorIdx >= len(barGradient) {
			colorIdx = len(barGradient) - 1
		}
		style := lipgloss.NewStyle().Foreground(barGradient[colorIdx])
		bar.WriteString(style.Render("█"))
	}
	// empty portion
	for i := filled; i < barWidth; i++ {
		bar.WriteString(barEmptyChar)
	}

	// Stats
	currentStr := humanBytes(p.current)
	totalStr := humanBytes(p.total)
	speedStr := humanBytes(int64(p.speed)) + "/s"

	etaStr := ""
	if p.speed > 0 && !p.finished {
		remaining := float64(p.total-p.current) / p.speed
		if remaining < 60 {
			etaStr = fmt.Sprintf("ETA %ds", int(remaining))
		} else {
			etaStr = fmt.Sprintf("ETA %dm%ds", int(remaining)/60, int(remaining)%60)
		}
	}

	if p.finished {
		elapsed := time.Since(p.startTime)
		etaStr = barDoneStyle.Render(fmt.Sprintf("✓ done in %s", elapsed.Round(time.Millisecond*100)))
		speedStr = ""
	}

	percentStr := barPercentStyle.Render(fmt.Sprintf("%3d%%", percent))
	statsStr := barStatsStyle.Render(fmt.Sprintf("%s / %s", currentStr, totalStr))

	icon := "⬇"
	if p.finished {
		icon = "✓"
	}

	parts := []string{
		"  ",
		barLabelStyle.Render(icon + " " + p.label),
		"  ",
		bar.String(),
		"  ",
		percentStr,
		"  ",
		statsStr,
	}
	if speedStr != "" {
		parts = append(parts, "  ", barStatsStyle.Render(speedStr))
	}
	if etaStr != "" {
		parts = append(parts, "  ", etaStr)
	}

	line := strings.Join(parts, "")
	fmt.Fprintf(os.Stderr, "\r%s\033[K", line)
}

// humanBytes converts bytes to a human-readable string.
func humanBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// HumanBytes is the exported version for use by other packages.
func HumanBytes(b int64) string {
	return humanBytes(b)
}

// MultiProgress manages rendering of multiple concurrent progress bars.
type MultiProgress struct {
	mu   sync.Mutex
	bars []*ProgressBar
}

// NewMultiProgress creates a multi-progress tracker.
func NewMultiProgress() *MultiProgress {
	return &MultiProgress{}
}

// Add creates and registers a new progress bar, returning it.
func (mp *MultiProgress) Add(label string, total int64) *ProgressBar {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	bar := NewProgressBar(label, total)
	mp.bars = append(mp.bars, bar)
	fmt.Fprintln(os.Stderr) // reserve a line
	return bar
}

// AllDone returns true if every bar has finished.
func (mp *MultiProgress) AllDone() bool {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	for _, b := range mp.bars {
		b.mu.Lock()
		done := b.finished
		b.mu.Unlock()
		if !done {
			return false
		}
	}
	return true
}
