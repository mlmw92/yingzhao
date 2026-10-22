package migrate

import "testing"

func TestParseFilename(t *testing.T) {
	for _, tc := range []struct {
		in      string
		version int
		name    string
		ok      bool
	}{
		{"0001_config_documents.sql", 1, "config_documents", true},
		{"0012_audit_chain.sql", 12, "audit_chain", true},
		{"bad.sql", 0, "", false},
		{"0001_.sql", 0, "", false},
	} {
		v, n, ok := parseFilename(tc.in)
		if ok != tc.ok || v != tc.version || n != tc.name {
			t.Fatalf("parseFilename(%q) = (%d,%q,%v)", tc.in, v, n, ok)
		}
	}
}

func TestLoadReturnsOrderedMigrations(t *testing.T) {
	ms, err := Load("mysql")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(ms) == 0 {
		t.Fatal("至少应有一条迁移")
	}
	for i := 1; i < len(ms); i++ {
		if ms[i-1].Version >= ms[i].Version {
			t.Fatalf("迁移未按版本升序: %d >= %d", ms[i-1].Version, ms[i].Version)
		}
	}
}

func TestLoadDMMigrations(t *testing.T) {
	ms, err := Load("dm")
	if err != nil {
		t.Fatalf("Load(dm): %v", err)
	}
	if len(ms) != 5 {
		t.Fatalf("达梦迁移 %d 条，want 5", len(ms))
	}
	for i := 1; i < len(ms); i++ {
		if ms[i-1].Version >= ms[i].Version {
			t.Fatalf("迁移未按版本升序: %d >= %d", ms[i-1].Version, ms[i].Version)
		}
	}
}
