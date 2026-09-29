package sitecat

import (
	"os"
	"testing"
)

// Unit tests never ask the real DuckDuckGo; tests that need the engine
// point EngineEndpoint at a fixture server.
func TestMain(m *testing.M) {
	EngineEndpoint = ""
	os.Exit(m.Run())
}
