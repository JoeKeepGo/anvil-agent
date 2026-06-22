package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadIdentityCreatesStableUUID(t *testing.T) {
	dir := t.TempDir()

	identity, err := LoadIdentity(dir)
	if err != nil {
		t.Fatalf("LoadIdentity first run returned error: %v", err)
	}
	if !isUUID(identity.ID) {
		t.Fatalf("identity id = %q, want uuid", identity.ID)
	}
	if identity.StateSchemaVersion != 1 {
		t.Fatalf("state schema version = %d, want 1", identity.StateSchemaVersion)
	}
	if identity.CreatedAt.IsZero() {
		t.Fatal("created at is zero")
	}

	reloaded, err := LoadIdentity(dir)
	if err != nil {
		t.Fatalf("LoadIdentity second run returned error: %v", err)
	}
	if reloaded.ID != identity.ID {
		t.Fatalf("reloaded id = %q, want %q", reloaded.ID, identity.ID)
	}
	if !reloaded.CreatedAt.Equal(identity.CreatedAt) {
		t.Fatalf("reloaded created at = %s, want %s", reloaded.CreatedAt, identity.CreatedAt)
	}
}

func TestLoadIdentityRejectsMalformedIdentityFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "identity.json"), []byte(`{"id":`), 0o600); err != nil {
		t.Fatalf("write malformed identity: %v", err)
	}

	identity, err := LoadIdentity(dir)
	if err == nil {
		t.Fatalf("LoadIdentity returned nil error and identity %+v, want malformed file error", identity)
	}
}

func TestLoadIdentityRejectsIdentityWithoutUUID(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "identity.json"), []byte(`{"id":"not-a-uuid","createdAt":"2026-06-22T00:00:00Z","stateSchemaVersion":1}`), 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}

	identity, err := LoadIdentity(dir)
	if err == nil {
		t.Fatalf("LoadIdentity returned nil error and identity %+v, want invalid identity error", identity)
	}
}

func TestLoadIdentityCreatesStateDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing", "state")

	identity, err := LoadIdentity(dir)
	if err != nil {
		t.Fatalf("LoadIdentity returned error: %v", err)
	}
	if identity.ID == "" {
		t.Fatal("identity id is empty")
	}
	if _, err := os.Stat(filepath.Join(dir, "identity.json")); err != nil {
		t.Fatalf("stat identity file: %v", err)
	}
}

func TestLoadIdentityRejectsEmptyStateDirectory(t *testing.T) {
	identity, err := LoadIdentity("")
	if err == nil {
		t.Fatalf("LoadIdentity returned nil error and identity %+v, want state dir error", identity)
	}
}

func TestIdentityJSONUsesUTC(t *testing.T) {
	dir := t.TempDir()
	identity, err := LoadIdentity(dir)
	if err != nil {
		t.Fatalf("LoadIdentity returned error: %v", err)
	}

	if identity.CreatedAt.Location() != time.UTC {
		t.Fatalf("created at location = %s, want UTC", identity.CreatedAt.Location())
	}
}
