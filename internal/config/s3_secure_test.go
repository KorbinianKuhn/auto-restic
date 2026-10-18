package config

import "testing"

func TestS3SecureDefault(t *testing.T) {
	setup(t, "")
	c, err := Get()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.S3.Secure {
		t.Fatal("expected s3.secure to default to true")
	}
}

func TestS3SecureOverride(t *testing.T) {
	setup(t, "s3:\n  secure: false\n")
	c, err := Get()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.S3.Secure {
		t.Fatal("expected s3.secure false from config")
	}

	setup(t, "")
	t.Setenv("S3_SECURE", "false")
	c, err = Get()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.S3.Secure {
		t.Fatal("expected s3.secure false from S3_SECURE")
	}
}
