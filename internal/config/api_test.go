package config

import (
	"strings"
	"testing"
)

// clearAPIEnv keeps values from the developer's environment out of the tests
func clearAPIEnv(t *testing.T) {
	t.Setenv("API_LISTEN", "")
	t.Setenv("API_TOKEN", "")
}

func TestAPIDisabledByDefault(t *testing.T) {
	setup(t, "")
	clearAPIEnv(t)
	c, err := Get()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.API.Listen != "" {
		t.Fatalf("expected api to be disabled, got listen %q", c.API.Listen)
	}
}

func TestAPIRequiresToken(t *testing.T) {
	setup(t, "api:\n  listen: \":2113\"\n")
	clearAPIEnv(t)
	_, err := Get()
	if err == nil || !strings.Contains(err.Error(), "API_TOKEN") {
		t.Fatalf("expected missing token error, got %v", err)
	}

	t.Setenv("API_TOKEN", "too-short")
	_, err = Get()
	if err == nil || !strings.Contains(err.Error(), "API_TOKEN") {
		t.Fatalf("expected short token error, got %v", err)
	}

	t.Setenv("API_TOKEN", "0123456789abcdef")
	c, err := Get()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.API.Listen != ":2113" || c.API.Token != "0123456789abcdef" {
		t.Fatalf("unexpected api config: %+v", c.API)
	}
}
