package api

import (
	"strings"
	"testing"
)

func TestParsePGStatStatementsStopsAtTheOutputLimit(t *testing.T) {
	out := &cappedBuffer{limit: 200}
	_, _ = out.Write([]byte("query,calls,total_exec_time,rows,shared_blks_hit,shared_blks_read,?column?,plans,total_plan_time,extract\n" +
		`"SELECT ""a"" FROM ""t"" WHERE ""b"" = $1",5,10,5,20,0,0,5,2,1792234567` + "\n" +
		`"SELECT ""a"" FROM ""t"" WHERE ""c"" = $1 AND ""d"" IN ($2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)",1,1,1,1,0,0,1,1,1792234567` + "\n"))

	stats, err := parsePGStatStatements(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.byKey) != 1 {
		t.Errorf("statements = %d, want the one before the cut", len(stats.byKey))
	}
	if total := stats.byKey[statementMatchKey(`SELECT "a" FROM "t" WHERE "b" = %s`)]; total == nil || total.calls != 5 {
		t.Errorf("totals = %+v, want the first statement's", total)
	}
	if stats.since.Unix() != 1792234567 {
		t.Errorf("since = %v", stats.since)
	}
}

func TestParsePGStatStatementsWithNothingRecorded(t *testing.T) {
	stats, err := parsePGStatStatements([]byte("query,calls,total_exec_time,rows,shared_blks_hit,shared_blks_read,?column?,plans,total_plan_time,extract\n,,,,,,,,,1792234567\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.byKey) != 0 || stats.since.Unix() != 1792234567 {
		t.Errorf("stats = %+v, want no statements but the reset time", stats)
	}
}

func TestPGStatStatementsError(t *testing.T) {
	cases := map[string]string{
		`psql:<stdin>:7: ERROR:  pg_stat_statements must be loaded via "shared_preload_libraries"`: "isn't loaded yet",
		`psql:<stdin>:7: ERROR:  relation "pg_stat_statements_info" does not exist`:                "isn't installed",
		"psql: error: connection to server failed\nmore detail":                                    "read pg_stat_statements: psql: error: connection to server failed",
		"": "psql failed",
	}
	for stderr, want := range cases {
		if got := pgStatStatementsError([]byte(stderr)).Error(); !strings.Contains(got, want) {
			t.Errorf("pgStatStatementsError(%q) = %q, want it to say %q", stderr, got, want)
		}
	}
}
