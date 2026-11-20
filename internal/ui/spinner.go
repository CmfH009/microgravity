package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Spinner renders an animated loading indicator in the terminal.
type Spinner struct {
	mu       sync.Mutex
	writer   io.Writer
	label    string
	frames   []string
	interval time.Duration
	active   bool
	stopCh   chan struct{}
	doneCh   chan struct{}
	idx      int
}

// Default spinner frames (braille-based rotating arc).
var defaultFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠶", "⠦", "⠷", "⠿"}

// NewSpinner creates a spinner writing to the given writer.
func NewSpinner(writer io.Writer, label string) *Spinner {
	return &Spinner{
		writer:   writer,
		label:    label,
		frames:   defaultFrames,
		interval: 80 * time.Millisecond,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// SetLabel updates the spinner label.
func (s *Spinner) SetLabel(label string) {
	s.mu.Lock()
	s.label = label
	s.mu.Unlock()
}

// Start begins the animation loop.
func (s *Spinner) Start() {
	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		return
	}
	s.active = true
	s.mu.Unlock()

	go s.run()
}

// run renders frames until stopped.
func (s *Spinner) run() {
	defer close(s.doneCh)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.render()
		}
	}
}

func (s *Spinner) render() {
	s.mu.Lock()
	frame := s.frames[s.idx%len(s.frames)]
	s.idx++
	label := s.label
	active := s.active
	s.mu.Unlock()

	if !active {
		return
	}

	style := lipgloss.NewStyle().Foreground(RoleColor(RolePrimary)).Bold(true)
	fmt.Fprintf(s.writer, "\r%s %s", style.Render(frame), lipgloss.NewStyle().Foreground(RoleColor(RoleTextMuted)).Render(label))
}

// Stop halts the spinner and clears the line.
func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	close(s.stopCh)
	s.mu.Unlock()

	<-s.doneCh

	// Clear the line
	line := strings.Repeat(" ", len(s.label)+4)
	fmt.Fprintf(s.writer, "\r%s\r", line)
}

// Spin runs a function with a spinner displayed until it completes.
func Spin(writer io.Writer, label string, fn func() error) error {
	sp := NewSpinner(writer, label)
	sp.Start()
	err := fn()
	sp.Stop()
	return err
}

// Pulse renders a single pulse frame (non-animated) for static use.
func Pulse(label string, role ThemeRole) string {
	style := lipgloss.NewStyle().Foreground(RoleColor(role)).Bold(true)
	return fmt.Sprintf("%s %s", style.Render("⠋"), lipgloss.NewStyle().Foreground(RoleColor(RoleTextMuted)).Render(label))
}
