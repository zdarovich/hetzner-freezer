package dump

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/afero"
)

func LoadServer(path string) (*ServerDump, error) {
	p := afero.NewBasePathFs(afero.NewOsFs(), path)
	return loadServer(p)
}

func loadServer(p afero.Fs) (*ServerDump, error) {
	s := ServerDump{}
	var err error
	load := func(name string, target interface{}) {
		if err != nil {
			return
		}
		if loadErr := loadPart(p, name, target); loadErr != nil {
			err = fmt.Errorf("failed to load '%s': %w", name, loadErr)
		}
	}

	load("server", &s.Server)
	load("floatingIPs", &s.FloatingIPs)
	load("sshKeys", &s.SSHKeys)
	load("snapshot", &s.Snapshot)

	return &s, nil
}

func loadPart(path afero.Fs, name string, target interface{}) error {
	f, err := path.Open(fmt.Sprintf("%s.json", name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to check if '%s' exists: %w", name, err)
	}
	defer f.Close()
	bb, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("failed read %s: %w", name, err)
	}

	if err := json.Unmarshal(bb, target); err != nil {
		return fmt.Errorf("failed to read from '%s: %w'", name, err)
	}
	return nil
}
