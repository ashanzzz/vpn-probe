package monitor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"vpn-probe/internal/model"
)

var metadataMu sync.RWMutex
var metadataFile = filepath.Join("data", "node_metadata.json")

func SetNodeMetadataFileForTest(p string) func() {
	metadataMu.Lock()
	old := metadataFile
	metadataFile = p
	metadataMu.Unlock()
	return func() { metadataMu.Lock(); metadataFile = old; metadataMu.Unlock() }
}
func loadMetadataUnsafe() (map[string]model.NodeMetadata, error) {
	b, err := os.ReadFile(metadataFile)
	if os.IsNotExist(err) {
		return map[string]model.NodeMetadata{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]model.NodeMetadata{}
	if err = json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}
func LoadNodeMetadata() (map[string]model.NodeMetadata, error) {
	metadataMu.RLock()
	defer metadataMu.RUnlock()
	return loadMetadataUnsafe()
}
func SaveNodeMetadata(id string, meta model.NodeMetadata) error {
	if strings.TrimSpace(id) == "" || len(id) > 512 {
		return fmt.Errorf("invalid node ID")
	}
	switch meta.InfraType {
	case "", model.InfraNATResidential, model.InfraVPSResidential, model.InfraNATLine, model.InfraVPSLine, model.InfraCommercial:
	default:
		return fmt.Errorf("invalid infra_type")
	}
	metadataMu.Lock()
	defer metadataMu.Unlock()
	entries, err := loadMetadataUnsafe()
	if err != nil {
		return err
	}
	entries[id] = meta
	return writeAtomicJSON(metadataFile, entries)
}

func writeAtomicJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
