package emojigen

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// File names under the workspace assets directory. They are served from
// /workspace/assets/ and named in internal/humanwork/workspace/assets.go.
const (
	OrderFile   = "emoji-order.json"
	NoticeFile  = "emoji-LICENSE.txt"
	langPrefix  = "emoji-"
	langSuffix  = ".json"
	maxInputKiB = 16 << 10
)

// LangFile is the file name of one language's names and keywords.
func LangFile(lang string) string { return langPrefix + lang + langSuffix }

// ReadDir reads the input folder: emoji-test.txt and the CLDR annotation files
// for each product language. It reads nothing else and opens no connection.
func ReadDir(dir string) (*Source, error) {
	src := &Source{Annotations: map[string][]byte{}, Derived: map[string][]byte{}}
	read := func(rel string) ([]byte, error) {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("%s: %w (place Unicode's files under %s)", rel, err, dir)
		}
		if len(data) > maxInputKiB<<10 {
			return nil, fmt.Errorf("%s is larger than %d KiB", rel, maxInputKiB)
		}
		return data, nil
	}
	var err error
	if src.EmojiTest, err = read("emoji-test.txt"); err != nil {
		return nil, err
	}
	for _, lang := range Languages {
		if src.Annotations[lang], err = read("annotations/" + lang + ".xml"); err != nil {
			return nil, err
		}
		if src.Derived[lang], err = read("annotationsDerived/" + lang + ".xml"); err != nil {
			return nil, err
		}
	}
	return src, nil
}

// gzipDeterministic compresses with every header field that could carry the
// build time or host pinned, so identical data produces identical bytes.
func gzipDeterministic(data []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	writer.Header.ModTime = time.Unix(0, 0)
	writer.Header.Name = ""
	writer.Header.Comment = ""
	writer.Header.OS = 255
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// Files lists what Write puts in the directory, name to bytes. The JSON files
// come with their gzip form because the server answers with the compressed
// bytes when the browser accepts them.
func (o *Output) Files() (map[string][]byte, error) {
	files := map[string][]byte{NoticeFile: o.Notice}
	add := func(name string, data []byte) error {
		files[name] = data
		packed, err := gzipDeterministic(data)
		if err != nil {
			return err
		}
		files[name+".gz"] = packed
		return nil
	}
	if err := add(OrderFile, o.Order); err != nil {
		return nil, err
	}
	for _, lang := range Languages {
		if err := add(LangFile(lang), o.Langs[lang]); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// Write puts the generated files in dir and, when dir holds the workspace
// asset manifest, brings the manifest up to date with them.
func Write(dir string, out *Output) ([]string, error) {
	files, err := out.Files()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var written []string
	for name, data := range files {
		if err := publish(filepath.Join(dir, name), data); err != nil {
			return nil, err
		}
		written = append(written, name)
	}
	if _, err := os.Stat(filepath.Join(dir, workspace.AssetIntegrityManifestName)); err == nil {
		if err := RefreshManifest(dir); err != nil {
			return nil, err
		}
		written = append(written, workspace.AssetIntegrityManifestName)
	}
	return written, nil
}

func publish(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".emoji-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return err
		}
		return os.Rename(name, path)
	}
	return nil
}

// RefreshManifest rewrites the workspace asset manifest from the bytes in dir,
// exactly as the wasm bundle build does, so adding data files does not leave the
// server refusing to start on a stale manifest.
func RefreshManifest(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	identities := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() && !strings.HasSuffix(entry.Name(), ".gz") {
			identities[entry.Name()] = true
		}
	}
	var sources []workspace.AssetIntegritySource
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == ".keep" || name == workspace.AssetIntegrityManifestName {
			continue
		}
		if strings.HasSuffix(name, ".gz") {
			if !identities[strings.TrimSuffix(name, ".gz")] {
				return fmt.Errorf("orphaned compressed asset %q has no identity file", name)
			}
			continue
		}
		contentType, routable := workspace.FrontendAssetContentType(name)
		if !routable {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		source := workspace.AssetIntegritySource{Name: name, Body: body, ContentType: contentType}
		if packed, readErr := os.ReadFile(filepath.Join(dir, name+".gz")); readErr == nil {
			source.GzipBody = packed
		} else if !os.IsNotExist(readErr) {
			return readErr
		}
		sources = append(sources, source)
	}
	manifest, err := workspace.GenerateAssetIntegrityManifest(sources)
	if err != nil {
		return fmt.Errorf("generate asset integrity manifest: %w", err)
	}
	body, err := manifest.CanonicalJSON()
	if err != nil {
		return err
	}
	return publish(filepath.Join(dir, workspace.AssetIntegrityManifestName), body)
}
