package toolcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const DefaultSchemaVersion = "v1"

var ErrInvalidKeyPart = errors.New("toolcache key part must be non-empty and must not contain ':'")

func BuildKey(tool string, args any, schemaVersion string) (string, error) {
	if schemaVersion == "" {
		schemaVersion = DefaultSchemaVersion
	}

	if !validKeyPart(tool) || !validKeyPart(schemaVersion) {
		return "", ErrInvalidKeyPart
	}

	payload, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("stable serialize args: %w", err)
	}

	sum := sha256.Sum256(payload)
	return tool + ":" + hex.EncodeToString(sum[:]) + ":" + schemaVersion, nil
}

func validKeyPart(value string) bool {
	return value != "" && !strings.Contains(value, ":")
}
