package intelligence

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

var errInvalidSymbolRef = errors.New("invalid symbol reference")

type symbolIdentity struct {
	Version    int      `json:"v"`
	SnapshotID string   `json:"snapshot"`
	Path       string   `json:"path"`
	Position   Position `json:"position"`
	Kind       string   `json:"kind"`
	Package    string   `json:"package"`
	Qualified  string   `json:"qualified"`
	Digest     string   `json:"digest,omitempty"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

func encodeSymbolRef(identity symbolIdentity) (SymbolRef, error) {
	if identity.SnapshotID == "" || identity.Path == "" || identity.Kind == "" || identity.Qualified == "" || identity.Position.Line < 0 || identity.Position.Character < 0 {
		return "", errInvalidSymbolRef
	}
	clean, err := cleanSnapshotPath(identity.Path)
	if err != nil {
		return "", errInvalidSymbolRef
	}
	identity.Path = clean
	identity.Version = 1
	identity.Digest = ""
	payload, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	identity.Digest = hex.EncodeToString(digest[:])
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	return SymbolRef(base64.RawURLEncoding.EncodeToString(encoded)), nil
}

func decodeSymbolRef(ref SymbolRef) (symbolIdentity, error) {
	encoded, err := base64.RawURLEncoding.DecodeString(string(ref))
	if err != nil {
		return symbolIdentity{}, errInvalidSymbolRef
	}
	var identity symbolIdentity
	if err := json.Unmarshal(encoded, &identity); err != nil || identity.Version != 1 || identity.Digest == "" {
		return symbolIdentity{}, errInvalidSymbolRef
	}
	digest := identity.Digest
	identity.Digest = ""
	payload, err := json.Marshal(identity)
	if err != nil {
		return symbolIdentity{}, errInvalidSymbolRef
	}
	observed := sha256.Sum256(payload)
	if digest != hex.EncodeToString(observed[:]) {
		return symbolIdentity{}, errInvalidSymbolRef
	}
	clean, cleanErr := cleanSnapshotPath(identity.Path)
	if cleanErr != nil || clean != identity.Path || identity.SnapshotID == "" || identity.Kind == "" || identity.Qualified == "" || identity.Position.Line < 0 || identity.Position.Character < 0 {
		return symbolIdentity{}, errInvalidSymbolRef
	}
	identity.Digest = digest
	return identity, nil
}

func requireSymbolSnapshot(identity symbolIdentity, snapshot SnapshotRef) error {
	if identity.SnapshotID != snapshot.ID {
		return fmt.Errorf("%w: symbol belongs to %s", ErrSnapshotChanged, identity.SnapshotID)
	}
	return nil
}
