//go:build !(js && wasm)

package productui

func docsSuggestMount(*docsEditorController, *docsSuggestController) func() {
	return func() {}
}
