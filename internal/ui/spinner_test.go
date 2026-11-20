package ui

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSpinnerStartStop(t *testing.T) {
	buf := new(bytes.Buffer)
	sp := NewSpinner(buf, "Testing...")
	sp.Start()
	time.Sleep(200 * time.Millisecond)
	sp.Stop()

	out := buf.String()
	assert.Contains(t, out, "Testing")
}

func TestSpin(t *testing.T) {
	buf := new(bytes.Buffer)
	err := Spin(buf, "Working", func() error {
		time.Sleep(300 * time.Millisecond)
		return nil
	})
	assert.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "Working")
}

func TestPulse(t *testing.T) {
	out := Pulse("loading", RolePrimary)
	assert.Contains(t, out, "loading")
	assert.NotEmpty(t, out)
}
