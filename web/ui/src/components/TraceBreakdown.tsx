import { api } from "../api";
import { formatMetricValue, formatQueryTime, relativeDelta } from "../format";
import { PostgresStatement, TraceTarget } from "../types";
import { useResource } from "../useResource";
import { Delta, TargetKey, TargetRow } from "./Targets";
import { Alert, Empty, formatTimestamp, Section, Segmented } from "./ui";

// "Where does a login's time go" per target, from the traces authentik
// exports to its workload cluster's Tempo (internal/api/traces.go). k6
// metrics say how slow a target is; this says why - database vs app time,
// how many queries a request makes, which flow stage and which query.
import { useState } from "react";

export const BREAKDOWN_WINDOWS = [
    { value: "15m", label: "15 m" },
    { value: "1h", label: "1 h" },
    { value: "6h", label: "6 h" },
];

const seconds = (v: number | undefined) => formatMetricValue(v, "s");

export function TraceBreakdownSection({
    testid,
    rows,
    baseline,
}: {
    testid: string;
    /** The test case's target rows, for consistent letters and colors. */
    rows: TargetRow[];
    baseline?: string;
}) {
    const [traceWindow, setTraceWindow] = useState("1h");
    // The backend caches for two minutes; polling faster would only re-read it.
    const breakdown = useResource(
        () => api.traceBreakdown(testid, traceWindow),
        [testid, traceWindow],
        120000,
    );
    const targets = breakdown.data?.targets ?? [];
    const sampled = targets.filter((t) => t.requests > 0);
    const indexOf = (target: string) => rows.find((r) => r.target === target)?.index ?? 0;
    const base = sampled.find((t) => t.target === baseline) ?? sampled[0];
    const stageNames = [...new Set(sampled.flatMap((t) => t.stages.map((s) => s.name)))].sort();

    return (
        <Section
            id="tracing"
            title="Tracing"
            description={
                <>
                    Where login time goes, from each target's most recent traces in Tempo. Times are
                    per request to authentik; a login is several requests.
                </>
            }
            actions={
                <Segmented
                    label="Trace window"
                    options={BREAKDOWN_WINDOWS}
                    value={traceWindow}
                    onChange={setTraceWindow}
                />
            }
        >
            <Alert>{breakdown.error}</Alert>
            {targets
                .filter((t) => t.error)
                .map((t) => (
                    <Alert kind="info" key={t.target}>
                        No traces for {t.target}: {t.error}
                    </Alert>
                ))}
            {breakdown.data && sampled.length === 0 ? (
                <Empty>
                    No traces for this test in the last {traceWindow}. They need authentik's tracing
                    enabled on the deployment and a trace sample rate above zero on the test case.
                </Empty>
            ) : (
                sampled.length > 0 && (
                    <>
                        <div className="table-scroll">
                            <table className="table">
                                <thead>
                                    <tr>
                                        <th>Target</th>
                                        <th className="num">Requests sampled</th>
                                        <th className="num">Request p50</th>
                                        <th className="num">Request p95</th>
                                        {sampled.length > 1 && <th className="num">Δ p50</th>}
                                        <th className="num">Queries / request</th>
                                        <th className="num">DB time / request</th>
                                        <th>Share of time in the database</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {sampled.map((t) => (
                                        <tr key={t.target}>
                                            <td>
                                                <span
                                                    className="row"
                                                    style={{ flexWrap: "nowrap" }}
                                                >
                                                    <TargetKey index={indexOf(t.target)} />
                                                    {t.target}
                                                </span>
                                            </td>
                                            <td className="mono num">
                                                {t.requests}{" "}
                                                <span className="muted">/ {t.traces} traces</span>
                                            </td>
                                            <td className="mono num">{seconds(t.requestP50)}</td>
                                            <td className="mono num">{seconds(t.requestP95)}</td>
                                            {sampled.length > 1 && (
                                                <td className="mono num">
                                                    {t === base ? (
                                                        <span className="muted">baseline</span>
                                                    ) : (
                                                        <Delta
                                                            value={relativeDelta(
                                                                t.requestP50,
                                                                base.requestP50,
                                                            )}
                                                            better="lower"
                                                        />
                                                    )}
                                                </td>
                                            )}
                                            <td className="mono num">
                                                {t.queriesPerRequest.toFixed(1)}
                                            </td>
                                            <td className="mono num">
                                                {seconds(t.dbTimePerRequest)}
                                            </td>
                                            <td>
                                                <ShareBar share={t.dbShare} />
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>

                        {stageNames.length > 0 && (
                            <>
                                <h3>Flow stages · p50 (p95)</h3>
                                <div className="table-scroll">
                                    <table className="table">
                                        <thead>
                                            <tr>
                                                <th>Stage</th>
                                                {sampled.map((t) => (
                                                    <th key={t.target} className="num">
                                                        <span
                                                            className="row"
                                                            style={{
                                                                justifyContent: "flex-end",
                                                                flexWrap: "nowrap",
                                                            }}
                                                        >
                                                            <TargetKey index={indexOf(t.target)} />
                                                            {t.target}
                                                        </span>
                                                    </th>
                                                ))}
                                            </tr>
                                        </thead>
                                        <tbody>
                                            {stageNames.map((name) => (
                                                <tr key={name}>
                                                    <td className="mono">{name}</td>
                                                    {sampled.map((t) => {
                                                        const stage = t.stages.find(
                                                            (s) => s.name === name,
                                                        );
                                                        return (
                                                            <td key={t.target} className="mono num">
                                                                {stage ? (
                                                                    <>
                                                                        {seconds(stage.p50)}{" "}
                                                                        <span className="muted">
                                                                            ({seconds(stage.p95)})
                                                                        </span>
                                                                    </>
                                                                ) : (
                                                                    <span className="muted">-</span>
                                                                )}
                                                            </td>
                                                        );
                                                    })}
                                                </tr>
                                            ))}
                                        </tbody>
                                    </table>
                                </div>
                            </>
                        )}

                        {sampled.map((t) => (
                            <TopQueries key={t.target} target={t} index={indexOf(t.target)} />
                        ))}
                    </>
                )
            )}
        </Section>
    );
}

function ShareBar({ share }: { share: number }) {
    return (
        <span className="row" style={{ flexWrap: "nowrap" }}>
            <span className="share-bar" aria-hidden="true">
                <span style={{ width: `${Math.min(100, share * 100)}%` }} />
            </span>
            <span className="mono">{formatMetricValue(share, "ratio")}</span>
        </span>
    );
}

/** perCall renders a per-call count: "1.0", "12.5", "4096". */
const perCall = (v: number) => (v >= 100 ? v.toFixed(0) : v.toFixed(1));

function TopQueries({ target, index }: { target: TraceTarget; index: number }) {
    if (target.topQueries.length === 0) {
        return null;
    }
    const postgres = target.postgres;
    const withPostgres = postgres !== undefined && !postgres.error;
    return (
        <details>
            <summary>
                <span className="row" style={{ display: "inline-flex", flexWrap: "nowrap" }}>
                    Costliest queries on <TargetKey index={index} /> {target.target}
                </span>
            </summary>
            <p className="muted">
                The round-trip floor is {formatQueryTime(target.queryFloor)}: the 10th percentile of
                every query in the sample, about what any query costs outside Postgres. A query
                close to it costs its round trips, so make fewer calls rather than tune it.
                {withPostgres && (
                    <>
                        {" "}
                        Postgres columns are from pg_stat_statements over every call
                        {postgres.since ? ` since ${formatTimestamp(postgres.since)}` : ""}, not
                        just this window. They count planning and executing but not committing,
                        which a write outside a transaction also waits for.
                    </>
                )}
            </p>
            {postgres?.error && (
                <Alert kind="info">
                    No Postgres figures for {target.target}: {postgres.error}
                </Alert>
            )}
            <div className="table-scroll">
                <table className="table">
                    <thead>
                        <tr>
                            <th>Statement</th>
                            <th className="num" title="Calls in the sample">
                                Calls
                            </th>
                            <th
                                className="num"
                                title="Calls per request on average, and the most in one request"
                            >
                                Per request
                            </th>
                            <th
                                className="num"
                                title="Time each request spends in it on average, and its share of the sampled requests' time: the most removing it could save"
                            >
                                Time / request
                            </th>
                            <th className="num">p50 (p95)</th>
                            <th className="num">Max</th>
                            <th
                                className="num"
                                title="How far its p50 is above the round-trip floor"
                            >
                                Over floor
                            </th>
                            {withPostgres && (
                                <>
                                    <th
                                        className="num"
                                        title="Mean time Postgres spent planning and executing it, per call"
                                    >
                                        In Postgres
                                    </th>
                                    <th className="num" title="Rows returned or changed per call">
                                        Rows / call
                                    </th>
                                    <th
                                        className="num"
                                        title="Shared buffer blocks touched per call, and the share read in from outside Postgres' buffer cache"
                                    >
                                        Blocks / call
                                    </th>
                                </>
                            )}
                        </tr>
                    </thead>
                    <tbody>
                        {target.topQueries.map((q) => (
                            <tr key={q.id}>
                                <td className="mono statement" title={q.statement}>
                                    <div>{q.statement}</div>
                                </td>
                                <td className="mono num">{q.count}</td>
                                <td className="mono num">
                                    {q.perRequest.toFixed(1)}{" "}
                                    <span className="muted">max {q.maxPerRequest}</span>
                                </td>
                                <td className="mono num">
                                    {formatQueryTime(q.timePerRequest)}{" "}
                                    <span className="muted">
                                        {formatMetricValue(q.share, "ratio")}
                                    </span>
                                </td>
                                <td className="mono num">
                                    {formatQueryTime(q.p50)}{" "}
                                    <span className="muted">({formatQueryTime(q.p95)})</span>
                                </td>
                                <td className="mono num">{formatQueryTime(q.max)}</td>
                                <td className="mono num">+{formatQueryTime(q.overFloor)}</td>
                                {withPostgres && <PostgresCells stats={q.postgres} />}
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </details>
    );
}

function PostgresCells({ stats }: { stats?: PostgresStatement }) {
    if (!stats) {
        return (
            <td
                className="muted"
                colSpan={3}
                title="pg_stat_statements has no statement of this shape"
            >
                no match
            </td>
        );
    }
    return (
        <>
            <td
                className="mono num"
                title={
                    stats.plans > 0
                        ? `${formatQueryTime(stats.meanPlan)} planning, ${formatQueryTime(stats.meanExec)} executing`
                        : "Executing only: Postgres doesn't track planning time here"
                }
            >
                {formatQueryTime(stats.meanPlan + stats.meanExec)}
                {stats.plans > 0 && (
                    <>
                        {" "}
                        <span className="muted">plan {formatQueryTime(stats.meanPlan)}</span>
                    </>
                )}
            </td>
            <td className="mono num">{perCall(stats.rowsPerCall)}</td>
            <td className="mono num">
                {perCall(stats.blocksPerCall)}{" "}
                <span className="muted">
                    {formatMetricValue(stats.readShare, "ratio")} read
                    {stats.tempBlocksPerCall > 0 && `, ${perCall(stats.tempBlocksPerCall)} temp`}
                </span>
            </td>
        </>
    );
}
