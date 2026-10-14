import { api } from "../api";
import { colorFor } from "../format";
import { useDarkTheme } from "../theme";
import { PodPlacement } from "../types";
import { useResource } from "../useResource";
import { Alert, Empty } from "./ui";

// Which node each deployment's pods landed on, one box per node grouped by
// cluster. A node holding more than one deployment is outlined - expected
// under Packed placement, a scheduling leak under Dedicated.
import { Link } from "react-router";

/**
 * With `deployment`, only nodes running that deployment's pods are shown,
 * listed by name; without it, every deployment's pods as colored squares.
 */
export function NodePlacement({ deployment }: { deployment?: string }) {
    const placement = useResource(() => api.placement(), [], 30000);
    const dark = useDarkTheme();

    const all = placement.data ?? [];
    const names = [...new Set(all.map((p) => p.deployment))].sort();
    const color = (name: string) => colorFor(names.indexOf(name), dark);

    // cluster -> node -> pods; the API returns them sorted by cluster/node/pod.
    const clusters = new Map<string, Map<string, PodPlacement[]>>();
    for (const p of all) {
        const nodes = clusters.get(p.cluster) ?? new Map<string, PodPlacement[]>();
        nodes.set(p.node, [...(nodes.get(p.node) ?? []), p]);
        clusters.set(p.cluster, nodes);
    }
    if (deployment) {
        for (const [cluster, nodes] of clusters) {
            for (const [node, pods] of nodes) {
                if (!pods.some((p) => p.deployment === deployment)) {
                    nodes.delete(node);
                }
            }
            if (nodes.size === 0) {
                clusters.delete(cluster);
            }
        }
    }

    return (
        <>
            <Alert>{placement.error}</Alert>
            {placement.data && clusters.size === 0 ? (
                <Empty>No running pods found in Prometheus yet.</Empty>
            ) : (
                [...clusters].map(([cluster, nodes]) => (
                    <div className="group" key={cluster}>
                        <div className="group-head">
                            <span>
                                <strong>{cluster}</strong>{" "}
                                <span className="muted">
                                    · {nodes.size} {nodes.size === 1 ? "node" : "nodes"}
                                </span>
                            </span>
                        </div>
                        <div className="placement">
                            {[...nodes].map(([node, pods]) => {
                                const here = [...new Set(pods.map((p) => p.deployment))];
                                const others = here.filter((d) => d !== deployment);
                                return (
                                    <div
                                        key={node}
                                        className={`placement-node${here.length > 1 ? " shared" : ""}`}
                                    >
                                        <div className="mono">{node}</div>
                                        {deployment ? (
                                            <>
                                                {pods
                                                    .filter((p) => p.deployment === deployment)
                                                    .map((p) => (
                                                        <div key={p.pod} className="mono muted">
                                                            {p.pod}
                                                        </div>
                                                    ))}
                                                {others.length > 0 && (
                                                    <div className="muted">
                                                        Shared with{" "}
                                                        {others.map((d, i) => (
                                                            <span key={d}>
                                                                {i > 0 && ", "}
                                                                <Link to={`/deployments/${d}`}>
                                                                    {d}
                                                                </Link>
                                                            </span>
                                                        ))}
                                                    </div>
                                                )}
                                            </>
                                        ) : (
                                            <div className="placement-pods">
                                                {pods.map((p) => (
                                                    <span
                                                        key={p.pod}
                                                        className="placement-pod"
                                                        style={{ background: color(p.deployment) }}
                                                        title={`${p.deployment} · ${p.pod}`}
                                                    />
                                                ))}
                                            </div>
                                        )}
                                    </div>
                                );
                            })}
                        </div>
                    </div>
                ))
            )}
            {!deployment && names.length > 0 && (
                <div className="row">
                    {names.map((name) => (
                        <Link key={name} className="chip" to={`/deployments/${name}`}>
                            <span className="placement-pod" style={{ background: color(name) }} />
                            {name}
                        </Link>
                    ))}
                </div>
            )}
        </>
    );
}
