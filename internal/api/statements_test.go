package api

import (
	"strings"
	"testing"
)

// cacheLookup is one of authentik's most frequent queries, as Django hands
// it to psycopg, before sqlcommenter appends its comment.
const cacheLookup = `SELECT "django_postgres_cache_cacheentry"."cache_key", "django_postgres_cache_cacheentry"."value", "django_postgres_cache_cacheentry"."expires" FROM "django_postgres_cache_cacheentry" WHERE ("django_postgres_cache_cacheentry"."cache_key" = %s AND "django_postgres_cache_cacheentry"."expires" >= %s) ORDER BY "django_postgres_cache_cacheentry"."cache_key" ASC LIMIT 1`

func TestNormalizeStatementGroupsCallsOfOneStatement(t *testing.T) {
	cases := []struct {
		name string
		sqls []string
		want string
	}{
		{
			name: "sqlcommenter comments, whatever the route and traceparent",
			sqls: []string{
				cacheLookup + ` /*app_name='authentik_api',controller='authentik_api%%3Aflow-executor',db_driver='psqlextra.backend',framework='django%%3A5.2.17',route='api/v3/flows/executor/%%3Cslug%%3Aflow_slug%%3E/',traceparent='00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01'*/`,
				cacheLookup + ` /*db_driver='psqlextra.backend',framework='django%%3A5.2.17',traceparent='00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01'*/`,
			},
			want: strings.Replace(cacheLookup, "LIMIT 1", "LIMIT %s", 1),
		},
		{
			name: "IN lists of any length",
			sqls: []string{
				`SELECT "u"."id" FROM "u" WHERE "u"."id" IN (%s, %s)`,
				`SELECT "u"."id" FROM "u" WHERE "u"."id" IN (%s, %s, %s, %s)`,
			},
			want: `SELECT "u"."id" FROM "u" WHERE "u"."id" IN (%s, …)`,
		},
		{
			name: "bulk inserts of any size",
			sqls: []string{
				`INSERT INTO "e" ("a", "b") VALUES (%s, %s), (%s, %s) RETURNING "e"."id"`,
				`INSERT INTO "e" ("a", "b") VALUES (%s, %s), (%s, %s), (%s, %s) RETURNING "e"."id"`,
			},
			want: `INSERT INTO "e" ("a", "b") VALUES (%s, …), … RETURNING "e"."id"`,
		},
		{
			name: "savepoints of any thread",
			sqls: []string{`SAVEPOINT "s140234567_x1"`, `SAVEPOINT "s9_x12"`, `SAVEPOINT $1`},
			want: `SAVEPOINT %s`,
		},
		{
			name: "literals and Postgres placeholders",
			sqls: []string{
				`SELECT set_config('TimeZone', %s, false)`,
				"SELECT  set_config(\n'Etc/UTC', $2,\tfalse)",
			},
			want: `SELECT set_config(%s, …, false)`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, sql := range tc.sqls {
				if got := normalizeStatement(sql); got != tc.want {
					t.Errorf("normalizeStatement(%q)\n got %q\nwant %q", sql, got, tc.want)
				}
			}
		})
	}
}

func TestNormalizeStatementKeepsIdentifiers(t *testing.T) {
	sql := `SELECT U0."id", "oauth2"."x2", a$b, e FROM "t" U0 WHERE U0."n" = 42 AND U0."f" > 1.5e3 AND U0."s" = 'it''s' AND REPLACE(U0."d", E'\\.', E'\'') = X'1F'`
	want := `SELECT U0."id", "oauth2"."x2", a$b, e FROM "t" U0 WHERE U0."n" = %s AND U0."f" > %s AND U0."s" = %s AND REPLACE(U0."d", %s, …) = %s`
	if got := normalizeStatement(sql); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestStatementMatchKeyFindsPostgresText pairs what a span records with
// what pg_stat_statements shows for the same call: psycopg binds values
// client-side, so Postgres sees literals (some with a cast) that it
// replaces with $n, and the first caller's comment.
func TestStatementMatchKeyFindsPostgresText(t *testing.T) {
	pairs := []struct {
		name, traced, recorded string
	}{
		{
			name:     "cast values, inlined LIMIT, another caller's comment",
			traced:   cacheLookup + ` /*traceparent='00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01'*/`,
			recorded: strings.NewReplacer("= %s", "= $1", ">= %s", ">= $2::timestamptz", "LIMIT 1", "LIMIT $3").Replace(cacheLookup) + ` /*app_name='authentik_api',traceparent='00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01'*/`,
		},
		{
			name:     "an IN list Postgres 17 records by its length",
			traced:   `SELECT "u"."id" FROM "u" WHERE "u"."id" IN (%s, %s)`,
			recorded: `SELECT "u"."id" FROM "u" WHERE "u"."id" IN ($1::uuid, $2::uuid, $3::uuid)`,
		},
		{
			name:     "an IN list Postgres 18 squashes",
			traced:   `SELECT "u"."id" FROM "u" WHERE "u"."id" IN (%s, %s)`,
			recorded: `SELECT "u"."id" FROM "u" WHERE "u"."id" IN ($1 /*, ... */)`,
		},
		{
			name:     "a bool and a string Django writes out",
			traced:   `SELECT set_config('TimeZone', %s, false)`,
			recorded: `SELECT set_config($1, $2, $3)`,
		},
		{
			name:     "None, sent as NULL",
			traced:   `INSERT INTO "t" ("a", "b") VALUES (%s, %s)`,
			recorded: `INSERT INTO "t" ("a", "b") VALUES ($1, NULL)`,
		},
		{
			name:     "casts amid a list",
			traced:   `INSERT INTO "authentik_core_session" ("expires", "expiring", "session_key", "session_data", "last_ip") VALUES (%s, %s, %s, %s, %s)`,
			recorded: `INSERT INTO "authentik_core_session" ("expires", "expiring", "session_key", "session_data", "last_ip") VALUES ($1::timestamptz, $2, $3, $4::bytea, $5::inet)`,
		},
		{
			name:     "a savepoint",
			traced:   `SAVEPOINT "s140234567_x3" /*traceparent='00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01'*/`,
			recorded: `SAVEPOINT $1 /*traceparent='00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01'*/`,
		},
	}
	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			traced, recorded := statementMatchKey(p.traced), statementMatchKey(p.recorded)
			if traced != recorded {
				t.Errorf("keys differ\n traced   %q\n recorded %q", traced, recorded)
			}
		})
	}

	if statementMatchKey(`SELECT "a" FROM "t" WHERE "b" = %s`) == statementMatchKey(`SELECT "a" FROM "t" WHERE "b" > %s`) {
		t.Error("statements that differ in an operator share a key")
	}
}

func TestSummarizeStatement(t *testing.T) {
	columns := `"authentik_flows_stage"."stage_uuid", "authentik_flows_stage"."name", "authentik_stages_password_passwordstage"."stage_ptr_id"`
	join := func(table string) string {
		return `LEFT OUTER JOIN "` + table + `" ON ("authentik_flows_stage"."stage_uuid" = "` + table + `"."stage_ptr_id")`
	}
	cases := []struct {
		name, sql, want string
	}{
		{
			name: "a long column list",
			sql:  `SELECT ` + columns + ` FROM "authentik_flows_stage" WHERE "authentik_flows_stage"."stage_uuid" = %s`,
			want: `SELECT … FROM "authentik_flows_stage" WHERE "authentik_flows_stage"."stage_uuid" = %s`,
		},
		{
			name: "a short column list says what the query is for",
			sql:  `SELECT %s AS "a" FROM "authentik_core_user" WHERE "authentik_core_user"."id" = %s LIMIT %s`,
			want: `SELECT %s AS "a" FROM "authentik_core_user" WHERE "authentik_core_user"."id" = %s LIMIT %s`,
		},
		{
			name: "DISTINCT stays",
			sql:  `SELECT DISTINCT ` + columns + ` FROM "authentik_flows_stage"`,
			want: `SELECT DISTINCT … FROM "authentik_flows_stage"`,
		},
		{
			name: "a chain of joins",
			sql: `SELECT ` + columns + ` FROM "authentik_flows_stage" ` + join("a") + ` ` + join("b") + ` ` + join("c") +
				` WHERE "authentik_flows_stage"."stage_uuid" IN (SELECT ` + columns + ` FROM "x") ORDER BY "authentik_flows_stage"."name" ASC`,
			want: `SELECT … FROM "authentik_flows_stage" … 3 joins … WHERE "authentik_flows_stage"."stage_uuid" IN (SELECT ` + columns + ` FROM "x") ORDER BY "authentik_flows_stage"."name" ASC`,
		},
		{
			name: "two joins stay",
			sql:  `SELECT "s"."key" FROM "s" ` + join("a") + ` ` + join("b") + ` WHERE "s"."key" = %s`,
			want: `SELECT "s"."key" FROM "s" ` + join("a") + ` ` + join("b") + ` WHERE "s"."key" = %s`,
		},
		{
			name: "joins to the end",
			sql:  `SELECT "s"."key" FROM "s" ` + join("a") + ` ` + join("b") + ` ` + join("c"),
			want: `SELECT "s"."key" FROM "s" … 3 joins …`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := summarizeStatement(tc.sql); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestTruncateStatementKeepsCharacters(t *testing.T) {
	if got := truncateStatement("ab…cd", 3); got != "ab…" {
		t.Errorf("got %q, want the cut before the multi-byte character", got)
	}
	if got := truncateStatement("abc", 3); got != "abc" {
		t.Errorf("got %q, want it unchanged", got)
	}
}
