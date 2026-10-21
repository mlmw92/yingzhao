package configdoc

import "testing"

func TestFileStoreContract(t *testing.T) {
	runStoreContract(t, func(t *testing.T) Store {
		return NewFileStore(t.TempDir())
	})
}
