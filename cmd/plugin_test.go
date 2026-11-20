package cmd

import (
	"testing"
)

func TestPluginCommandRegistration(t *testing.T) {
	if pluginCmd.Name() != "plugin" {
		t.Errorf("expected plugin command name 'plugin', got %s", pluginCmd.Name())
	}
	commands := pluginCmd.Commands()
	if len(commands) < 7 {
		t.Errorf("expected at least 7 subcommands for plugin, got %d", len(commands))
	}
}
