package config

import "testing"

func TestAPIEnabledByDefault(t *testing.T) {
	setup(t, "")
	t.Setenv("API_ENABLED", "")
	c, err := Get()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.API.Enabled {
		t.Fatal("expected api to be enabled by default")
	}
}

func TestAPIDisable(t *testing.T) {
	setup(t, "api:\n  enabled: false\n")
	t.Setenv("API_ENABLED", "")
	c, err := Get()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.API.Enabled {
		t.Fatal("expected api.enabled false from config")
	}

	setup(t, "")
	t.Setenv("API_ENABLED", "false")
	c, err = Get()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.API.Enabled {
		t.Fatal("expected api.enabled false from API_ENABLED")
	}
}
