package injection

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type DocumentZones struct {
	Path  string
	Zones []Zone
}

func DefaultInstructionFiles(root string) []string {
	return []string{
		filepath.Join(root, "AGENTS.md"),
		filepath.Join(root, "CLAUDE.md"),
	}
}

func ExtractZonesFromFile(path string) ([]Zone, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read qiankun injection file %s: %w", path, err)
	}

	zones, err := ExtractZones(string(data))
	if err != nil {
		return nil, fmt.Errorf("extract qiankun injection zones from %s: %w", path, err)
	}
	return zones, nil
}

func ExtractZonesFromFiles(paths []string) ([]DocumentZones, error) {
	documents := make([]DocumentZones, 0, len(paths))
	for _, path := range paths {
		zones, err := ExtractZonesFromFile(path)
		if err != nil {
			return nil, err
		}
		if len(zones) == 0 {
			continue
		}
		documents = append(documents, DocumentZones{
			Path:  path,
			Zones: zones,
		})
	}
	return documents, nil
}
