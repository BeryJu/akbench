package api

// What each of the trace breakdown's costliest queries costs inside
// Postgres, from the deployment's pg_stat_statements, next to what it costs
// authentik end to end (traces.go). The difference is time spent outside
// Postgres (the network, the driver, waiting on Python) or committing.
//
// It's read through the same psql exec as the SQL endpoint (sql.go), so it
// needs the shell kubeconfig as well. pg_stat_statements' counters run from
// its last reset rather than over the breakdown's window, so the figures
// are per call and come with that reset's time.

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
)

const (
	// pgStatStatementsQuery reads every top-level statement recorded for
	// the current database, the most expensive first. The LEFT JOIN keeps
	// one row, for the reset time, when none are recorded.
	pgStatStatementsQuery = `SELECT s.query, s.calls, s.total_exec_time, s.rows,
       s.shared_blks_hit, s.shared_blks_read, s.temp_blks_read + s.temp_blks_written,
       s.plans, s.total_plan_time, extract(epoch FROM i.stats_reset)
FROM pg_stat_statements_info i
LEFT JOIN pg_stat_statements s
       ON s.dbid = (SELECT oid FROM pg_database WHERE datname = current_database()) AND s.toplevel
ORDER BY s.total_exec_time + s.total_plan_time DESC NULLS LAST
LIMIT 10000
`
	pgStatStatementsColumns = 10
	// pgStatStatementsOutputLimit bounds the texts read: thousands of
	// statements, the longest of authentik's ~16KB. Past it, the cheapest
	// statements go unmatched.
	pgStatStatementsOutputLimit = 32 << 20
)

// PostgresStats says where a target's per-statement Postgres figures come
// from, or why there are none.
type PostgresStats struct {
	// Since is pg_stat_statements' last reset. The figures cover every call
	// from then on, not just the breakdown's window.
	Since *time.Time `json:"since,omitempty"`
	Error string     `json:"error,omitempty"`
}

// PostgresStatement is what pg_stat_statements recorded for one traced
// statement, summed over its entries of the same shape. Times are seconds.
type PostgresStatement struct {
	Calls int64 `json:"calls"`
	// MeanExec is the time Postgres spent executing it, per call, and
	// MeanPlan the time it spent planning it. Plans is how often it did:
	// zero when pg_stat_statements.track_planning is off, and MeanPlan
	// unknown. Neither counts the commit, whose WAL flush a write outside a
	// transaction waits for too.
	MeanExec    float64 `json:"meanExec"`
	MeanPlan    float64 `json:"meanPlan"`
	Plans       int64   `json:"plans"`
	RowsPerCall float64 `json:"rowsPerCall"`
	// BlocksPerCall is the shared buffer blocks it touched per call, and
	// ReadShare the part of them Postgres had to read in rather than find
	// in its buffer cache.
	BlocksPerCall float64 `json:"blocksPerCall"`
	ReadShare     float64 `json:"readShare"`
	// TempBlocksPerCall is the temporary file blocks it read and wrote per
	// call: sorts and hashes that didn't fit in work_mem.
	TempBlocksPerCall float64 `json:"tempBlocksPerCall"`
}

// pgStatementTotals sums pg_stat_statements counters.
type pgStatementTotals struct {
	calls, execMillis, rows, hit, read, temp, plans, planMillis float64
}

type postgresStatements struct {
	since time.Time
	// byKey are the totals by statementMatchKey.
	byKey map[string]*pgStatementTotals
}

// postgresStatements reads topo's pg_stat_statements.
func (s *Server) postgresStatements(ctx context.Context, topo *benchv1alpha1.AuthentikDeployment) (*postgresStatements, error) {
	if s.cfg.Shell == nil {
		return nil, errors.New("reading pg_stat_statements needs the shell kubeconfig, which is not configured on this backend")
	}
	req, _, err := s.postgresExecRequest(ctx, topo)
	if err != nil {
		return nil, err
	}
	req.Command = append([]string{"sh", "-c", sqlPsqlScript, "psql"}, sqlFormatArgs[sqlFormatCSV]...)
	req.Stdin = strings.NewReader(pgStatStatementsQuery)
	req.StdoutLimit = pgStatStatementsOutputLimit
	res, err := s.podExec(ctx, req)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, pgStatStatementsError(res.Stderr)
	}
	return parsePGStatStatements(res.Stdout)
}

// pgStatStatementsError explains psql failing to read pg_stat_statements.
func pgStatStatementsError(stderr []byte) error {
	msg := strings.TrimSpace(string(stderr))
	switch {
	case strings.Contains(msg, `"shared_preload_libraries"`):
		return errors.New("pg_stat_statements isn't loaded yet: Postgres needs a restart with it in shared_preload_libraries")
	case strings.Contains(msg, `relation "pg_stat_statements`):
		return errors.New("the pg_stat_statements extension isn't installed in authentik's database yet")
	}
	if line, _, _ := strings.Cut(msg, "\n"); line != "" {
		return fmt.Errorf("read pg_stat_statements: %s", line)
	}
	return errors.New("read pg_stat_statements: psql failed")
}

// parsePGStatStatements sums psql's CSV of pgStatStatementsQuery by
// statement shape. A record cut short by the output limit ends it.
func parsePGStatStatements(out []byte) (*postgresStatements, error) {
	r := csv.NewReader(bytes.NewReader(out))
	r.FieldsPerRecord = pgStatStatementsColumns
	r.ReuseRecord = true
	if _, err := r.Read(); err != nil {
		return nil, fmt.Errorf("read pg_stat_statements: %w", err)
	}
	stats := &postgresStatements{byKey: map[string]*pgStatementTotals{}}
	number := func(s string) float64 {
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	for {
		rec, err := r.Read()
		if err != nil {
			// io.EOF, or the record the output limit cut.
			break
		}
		if stats.since.IsZero() && rec[9] != "" {
			seconds, fraction := math.Modf(number(rec[9]))
			stats.since = time.Unix(int64(seconds), int64(fraction*1e9)).UTC()
		}
		if rec[0] == "" {
			continue
		}
		key := statementMatchKey(rec[0])
		totals := stats.byKey[key]
		if totals == nil {
			totals = &pgStatementTotals{}
			stats.byKey[key] = totals
		}
		totals.calls += number(rec[1])
		totals.execMillis += number(rec[2])
		totals.rows += number(rec[3])
		totals.hit += number(rec[4])
		totals.read += number(rec[5])
		totals.temp += number(rec[6])
		totals.plans += number(rec[7])
		totals.planMillis += number(rec[8])
	}
	return stats, nil
}

// applyPostgresStatements matches t's top queries to their totals in stats,
// or records why there are none.
func applyPostgresStatements(t *TraceTarget, stats *postgresStatements, err error) {
	if len(t.TopQueries) == 0 {
		return
	}
	if err != nil {
		t.Postgres = &PostgresStats{Error: err.Error()}
		return
	}
	t.Postgres = &PostgresStats{}
	if !stats.since.IsZero() {
		since := stats.since
		t.Postgres.Since = &since
	}
	for i := range t.TopQueries {
		q := &t.TopQueries[i]
		totals := stats.byKey[q.matchKey]
		if totals == nil || totals.calls == 0 {
			continue
		}
		q.Postgres = &PostgresStatement{
			Calls:             int64(totals.calls),
			MeanExec:          totals.execMillis / 1000 / totals.calls,
			MeanPlan:          totals.planMillis / 1000 / totals.calls,
			Plans:             int64(totals.plans),
			RowsPerCall:       totals.rows / totals.calls,
			BlocksPerCall:     (totals.hit + totals.read) / totals.calls,
			TempBlocksPerCall: totals.temp / totals.calls,
		}
		if blocks := totals.hit + totals.read; blocks > 0 {
			q.Postgres.ReadShare = totals.read / blocks
		}
	}
}
