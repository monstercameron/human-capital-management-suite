package chatload

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"testing/fstest"
)

// snapshotSchema pins migrations across sizes even while other lanes edit the
// checkout. The report retains a digest for the exact benchmark schema.
func snapshotSchema(root string) (fs.FS, string, error) {
	dir := filepath.Join(root, "internal/data/chatstore/migrations")
	entries, e := os.ReadDir(dir)
	if e != nil {
		return nil, "", e
	}
	snapshot := fstest.MapFS{}
	hash := sha256.New()
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		data, e := os.ReadFile(filepath.Join(dir, entry.Name()))
		if e != nil {
			return nil, "", e
		}
		snapshot[entry.Name()] = &fstest.MapFile{Data: data}
		_, _ = hash.Write([]byte(entry.Name() + "\x00"))
		_, _ = hash.Write(data)
	}
	return snapshot, hex.EncodeToString(hash.Sum(nil)), nil
}
