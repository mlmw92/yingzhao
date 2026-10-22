package sqlx

import (
	"strings"
	"testing"
)

func TestDMUpsertGeneratesMerge(t *testing.T) {
	insert := "INSERT INTO ops_tasks(id, batch_id) VALUES(?,?)" +
		DM().Upsert("id", "batch_id=excluded.batch_id")
	got := DM().Rewrite(insert)
	want := "MERGE INTO ops_tasks t USING (SELECT ? AS id, ? AS batch_id FROM DUAL) s " +
		"ON (t.id = s.id) " +
		"WHEN MATCHED THEN UPDATE SET t.batch_id=s.batch_id " +
		"WHEN NOT MATCHED THEN INSERT (id, batch_id) VALUES (s.id, s.batch_id)"
	if got != want {
		t.Fatalf("MERGE 生成不对:\n got: %s\nwant: %s", got, want)
	}
}

func TestDMUpsertNothingGeneratesMergeWithoutUpdate(t *testing.T) {
	insert := "INSERT INTO audit_chain(id, seq, hash) VALUES(1, 0, '')" +
		DM().UpsertNothing("id")
	got := DM().Rewrite(insert)
	if strings.Contains(got, "WHEN MATCHED") {
		t.Fatalf("DO NOTHING 不应生成 WHEN MATCHED: %s", got)
	}
	if !strings.Contains(got, "WHEN NOT MATCHED THEN INSERT (id, seq, hash) VALUES (s.id, s.seq, s.hash)") {
		t.Fatalf("缺少 INSERT 分支: %s", got)
	}
}

func TestDMUpsertQualifiesTargetColumns(t *testing.T) {
	insert := "INSERT INTO nodes(hostname, last_seen) VALUES(?,?)" +
		DM().Upsert("hostname", "last_seen=MAX(last_seen, excluded.last_seen)")
	got := DM().Rewrite(insert)
	if !strings.Contains(got, "last_seen=GREATEST(t.last_seen, s.last_seen)") {
		t.Fatalf("目标列未限定或 MAX 未转换: %s", got)
	}
}

func TestDMRewriteBackticksAndRows(t *testing.T) {
	got := DM().Rewrite("SELECT `key`, rows FROM audit_pruned")
	if !strings.Contains(got, "key") || strings.Contains(got, "`") {
		t.Fatalf("反引号应改成裸标识符: %s", got)
	}
	if !strings.Contains(got, `"rows"`) {
		t.Fatalf("rows 是保留字，应加双引号: %s", got)
	}
}

func TestDMParse(t *testing.T) {
	for _, name := range []string{"dm", "dameng"} {
		d, err := Parse(name)
		if err != nil || d.Name() != "dm" {
			t.Fatalf("Parse(%q) = %v, %v", name, d, err)
		}
	}
}

func TestDMBindKeepsQuestionMarks(t *testing.T) {
	if got := DM().Bind("SELECT * FROM t WHERE a=? AND b=?"); got != "SELECT * FROM t WHERE a=? AND b=?" {
		t.Fatalf("达梦占位符不应重绑定: %q", got)
	}
}
