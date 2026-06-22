package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const identityFileName = "identity.json"
const CurrentStateSchemaVersion = 1

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type AgentIdentity struct {
	ID                 string    `json:"id"`
	CreatedAt          time.Time `json:"createdAt"`
	StateSchemaVersion int       `json:"stateSchemaVersion"`
}

func LoadIdentity(stateDir string) (AgentIdentity, error) {
	if stateDir == "" {
		return AgentIdentity{}, fmt.Errorf("state directory is required")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return AgentIdentity{}, fmt.Errorf("create state directory: %w", err)
	}

	path := filepath.Join(stateDir, identityFileName)
	data, err := os.ReadFile(path)
	if err == nil {
		return parseIdentity(data)
	}
	if !os.IsNotExist(err) {
		return AgentIdentity{}, fmt.Errorf("read identity file: %w", err)
	}

	identity := AgentIdentity{
		ID:                 newUUID(),
		CreatedAt:          time.Now().UTC(),
		StateSchemaVersion: CurrentStateSchemaVersion,
	}
	data, err = json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return AgentIdentity{}, fmt.Errorf("marshal identity: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return AgentIdentity{}, fmt.Errorf("write identity file: %w", err)
	}
	return identity, nil
}

func parseIdentity(data []byte) (AgentIdentity, error) {
	var identity AgentIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return AgentIdentity{}, fmt.Errorf("parse identity file: %w", err)
	}
	if !isUUID(identity.ID) {
		return AgentIdentity{}, fmt.Errorf("identity file has invalid agent id")
	}
	if identity.CreatedAt.IsZero() {
		return AgentIdentity{}, fmt.Errorf("identity file has invalid createdAt")
	}
	if identity.StateSchemaVersion != CurrentStateSchemaVersion {
		return AgentIdentity{}, fmt.Errorf("identity file has unsupported state schema version %d", identity.StateSchemaVersion)
	}
	identity.CreatedAt = identity.CreatedAt.UTC()
	return identity, nil
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("generate uuid: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	var dst [36]byte
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst[:])
}

func isUUID(value string) bool {
	return uuidPattern.MatchString(value)
}
