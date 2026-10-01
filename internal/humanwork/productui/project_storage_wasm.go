//go:build js && wasm

package productui

// projectViewerMemory keeps small per-viewer presentation choices (a collapsed
// section, a page size) for the life of the page. WEB-031 confines browser
// storage to the history adapter, so these choices survive soft navigation and
// fall back to the default on reload.
var projectViewerMemory = map[string]string{}

func projectStorageRead(key string) string { return projectViewerMemory[key] }

func projectStorageWrite(key, value string) { projectViewerMemory[key] = value }
