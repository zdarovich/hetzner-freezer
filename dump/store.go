package dump

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/spf13/afero"
	"os"
)

func StoreServer(path string, s *ServerDump) error {
	p := afero.NewBasePathFs(afero.NewOsFs(), path)
	return storeServer(p, s)
}

func storeServer(p afero.Fs, s *ServerDump) error {
	var err error
	store := func(name string, obj interface{}) {
		bb, err := json.Marshal(obj)
		if err != nil {
			err = fmt.Errorf("failed to process '%s': %w", name, err)
			return
		}
		if storeErr := storePart(p, name, bb); storeErr != nil {
			err = fmt.Errorf("failed to write to disc: %w", storeErr)
		}
	}

	store("server", &s.Server)
	store("floatingIPs", &s.FloatingIPs)
	store("sshKeys", &s.SSHKeys)
	store("snapshot", &s.Snapshot)

	if err != nil {
		return err
	}

	return nil
}

func storePart(parent afero.Fs, name string, bb []byte) error {
	if len(bb) == 0 {
		return nil
	}

	output := bytes.Buffer{}
	if err := json.Indent(&output, bb, "", "    "); err != nil {
		return fmt.Errorf("failed to format '%s': %w", name, err)
	}
	f, err := parent.Create(fmt.Sprintf("%s.json", name))
	if err != nil {
		return fmt.Errorf("failed to create '%s': %w", fmt.Sprintf("%s.json", name), err)
	}
	_, err = f.Write(output.Bytes())
	if err != nil {
		return fmt.Errorf("failed to write json output: %w", err)
	}
	return nil
}

func EnsureHasDirectory(path string) (string, error) {
	_, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("could not stat dir %s: %w", path, err)
	}
	err = os.MkdirAll(path, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("failed to create %s: %w", path, err)
	}
	return path, nil
}
