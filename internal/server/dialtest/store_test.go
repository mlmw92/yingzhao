package dialtest

import (
	"path/filepath"
	"testing"

	"github.com/mlmw92/yingzhao/internal/server/configdoc"
)

// 拨测任务经持久化接口读写：换成数据库后端后行为不变。
func TestStoreWithPersisterRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dialtest.yaml")
	s := NewStoreWithPersister(configdoc.NewFilePersister(path))
	task := s.Create(Task{Name: "probe", Target: "127.0.0.1:80", Type: TaskTypeTCP})

	reloaded := NewStoreWithPersister(configdoc.NewFilePersister(path))
	got, ok := reloaded.Get(task.ID)
	if !ok || got.Name != "probe" {
		t.Fatalf("重新加载 = %+v, %v", got, ok)
	}
}
