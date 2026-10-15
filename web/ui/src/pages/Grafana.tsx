import { api } from "../api";
import { Alert, Empty, Page } from "../components/ui";
import { useResource } from "../useResource";

// Embeds a workload cluster's own Grafana (internal/render/grafana.go) in an
// iframe, through cmd/apiserver's /grafana-proxy/<cluster>/ reverse proxy
// (internal/api/grafana_proxy.go). This app is served by cmd/apiserver
// itself, so the proxy is same-origin: no NodePort and no extra port-forward.
//
// The whole Grafana UI is embedded as-is - no dashboard picker or kiosk mode.
//
// The proxy only answers a browser holding its session cookie, minted here
// with this app's own API credential before the iframe loads and re-minted
// while the page stays open.
//
// The iframe's own location is mirrored into this page's URL
// (/grafana/<cluster>/<grafana path>?<grafana query>), so a deep link into
// Grafana can be shared and reopens where it was.
import { useEffect, useMemo, useRef } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router";

// Must match render.grafanaProxyPathPrefix, which Grafana's own
// GF_SERVER_ROOT_URL is set to.
function grafanaProxyURL(cluster: string): string {
    return `/grafana-proxy/${encodeURIComponent(cluster)}/`;
}

// Grafana navigates client-side (pushState) without telling the parent, so
// poll its location.
const SYNC_MS = 500;

// The cookie lasts an hour (grafanaSessionTTL); renew well inside that.
const SESSION_RENEW_MS = 20 * 60 * 1000;

export function GrafanaPage() {
    const { "cluster": clusterParam, "*": grafanaPath = "" } = useParams();
    const location = useLocation();
    const navigate = useNavigate();
    const frame = useRef<HTMLIFrameElement>(null);
    const clusters = useResource(() => api.workloadClusters(), [], 0);
    const session = useResource(() => api.grafanaSession(), [], SESSION_RENEW_MS);

    const withGrafana = (clusters.data ?? []).filter((c) => c.status?.grafanaURL);
    const cluster = clusterParam ?? withGrafana[0]?.metadata.name;
    const known = clusters.data?.find((c) => c.metadata.name === cluster);
    const ready = !!known?.status?.grafanaURL;

    // Only the initial deep link: afterwards the iframe navigates itself, and
    // this page's URL just follows it.
    const src = useMemo(
        () => cluster && grafanaProxyURL(cluster) + grafanaPath + location.search + location.hash,
        // oxlint-disable-next-line react-hooks/exhaustive-deps
        [cluster],
    );

    useEffect(() => {
        if (!cluster) return;
        const prefix = grafanaProxyURL(cluster);
        const id = setInterval(() => {
            const loc = frame.current?.contentWindow?.location;
            if (!loc?.pathname.startsWith(prefix)) return;
            const next = `/grafana/${encodeURIComponent(cluster)}/${loc.pathname.slice(prefix.length)}${loc.search}${loc.hash}`;
            const { pathname, search, hash } = window.location;
            if (next !== pathname + search + hash) navigate(next, { replace: true });
        }, SYNC_MS);
        return () => clearInterval(id);
    }, [cluster, navigate]);

    const selectCluster = (name: string) => navigate(`/grafana/${encodeURIComponent(name)}`);

    return (
        <Page
            title="Grafana"
            subtitle="A workload cluster's own Grafana, proxied through the API server."
            actions={
                clusters.data &&
                withGrafana.length > 0 && (
                    <select
                        value={cluster ?? ""}
                        onChange={(e) => selectCluster(e.target.value)}
                        aria-label="Cluster"
                    >
                        {clusterParam &&
                            !withGrafana.some((c) => c.metadata.name === clusterParam) && (
                                <option value={clusterParam}>{clusterParam}</option>
                            )}
                        {withGrafana.map((c) => (
                            <option key={c.metadata.name} value={c.metadata.name}>
                                {c.metadata.name}
                            </option>
                        ))}
                    </select>
                )
            }
        >
            <Alert>{clusters.error ?? session.error}</Alert>
            {clusters.data && withGrafana.length === 0 && (
                <Empty>
                    No workload cluster has Grafana yet - it's installed per workload cluster once
                    it's provisioned. <Link to="/clusters">Go to clusters</Link>
                </Empty>
            )}
            {clusters.data && cluster && !known && (
                <Alert>Workload cluster {cluster} not found.</Alert>
            )}
            {known && !ready && <Alert kind="info">Grafana on {cluster} isn't ready yet.</Alert>}
            {cluster && ready && session.data && (
                <iframe
                    key={cluster}
                    ref={frame}
                    className="grafana-frame"
                    title={`Grafana on ${cluster}`}
                    src={src}
                />
            )}
        </Page>
    );
}
