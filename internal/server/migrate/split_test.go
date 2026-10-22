package migrate

import "testing"

func TestSplitStatements(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "单条",
			in:   "CREATE TABLE t(id INT);",
			want: []string{"CREATE TABLE t(id INT)"},
		},
		{
			name: "多条",
			in:   "ALTER TABLE t COMMENT='a'; ALTER TABLE t MODIFY COLUMN id INT COMMENT 'b';",
			want: []string{"ALTER TABLE t COMMENT='a'", "ALTER TABLE t MODIFY COLUMN id INT COMMENT 'b'"},
		},
		{
			name: "字符串里的分号",
			in:   "INSERT INTO t(v) VALUES('a;b'); COMMENT ON TABLE t IS 'c;d';",
			want: []string{"INSERT INTO t(v) VALUES('a;b')", "COMMENT ON TABLE t IS 'c;d'"},
		},
		{
			name: "转义单引号",
			in:   "COMMENT ON TABLE t IS 'it''s; ok';",
			want: []string{"COMMENT ON TABLE t IS 'it''s; ok'"},
		},
		{
			name: "行注释",
			in:   "-- 说明; 不是语句\nCREATE TABLE t(id INT);",
			want: []string{"CREATE TABLE t(id INT)"},
		},
		{
			name: "空语句",
			in:   ";;\n",
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitStatements(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("splitStatements(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("第 %d 条 = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
