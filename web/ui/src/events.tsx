import { api } from "./api";
import { useAuth } from "./auth";

// Live updates: one shared WebSocket to internal/api/events.go's feed for
// the whole app, fanning {kind, name, type} events out to whichever pages
// (via useResource's watchKind) care about that kind right now. Connecting
// is the same two-step ticket dance the kubectl shell uses (TerminalSession.tsx) -
// a browser WebSocket can't carry an Authorization header, so a short-lived
// ticket from an ordinary bearer-authenticated request stands in for it.
//
// Reconnects on its own with capped exponential backoff: unlike the shell
// (a session the user is actively typing into, where "Reconnect" is a
// deliberate user action), this is a best-effort background feed - a page
// that misses an update still has its own poll interval as a fallback
// (see useResource.ts), so silently retrying is the right default.
import { createContext, ReactNode, useContext, useEffect, useLayoutEffect, useRef } from "react";

export interface ResourceEvent {
    kind: string;
    name: string;
    type: string;
}

type Listener = (ev: ResourceEvent) => void;

const RECONNECT_MIN_MS = 1000;
const RECONNECT_MAX_MS = 30000;

interface EventsContextValue {
    subscribe: (kind: string, listener: Listener) => () => void;
}

const EventsContext = createContext<EventsContextValue | undefined>(undefined);

export function EventsProvider({ children }: { children: ReactNode }) {
    const listenersRef = useRef(new Map<string, Set<Listener>>());
    const { authenticated } = useAuth();

    useEffect(() => {
        // Minting a ticket hits a bearer-authed route - connecting before login
        // would just 401 in a loop until the user signs in.
        if (!authenticated) {
            return;
        }
        let cancelled = false;
        let ws: WebSocket | undefined;
        let retryDelay = RECONNECT_MIN_MS;
        let retryTimer: ReturnType<typeof setTimeout> | undefined;

        function dispatch(ev: ResourceEvent) {
            for (const listener of listenersRef.current.get(ev.kind) ?? []) {
                listener(ev);
            }
        }

        function scheduleReconnect() {
            if (cancelled) {
                return;
            }
            retryTimer = setTimeout(connect, retryDelay);
            retryDelay = Math.min(retryDelay * 2, RECONNECT_MAX_MS);
        }

        async function connect() {
            if (cancelled) {
                return;
            }
            try {
                const { ticket } = await api.createEventsSession();
                if (cancelled) {
                    return;
                }
                const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
                ws = new WebSocket(
                    `${proto}//${window.location.host}/api/v1/events/connect?ticket=${encodeURIComponent(ticket)}`,
                );
                ws.onopen = () => {
                    retryDelay = RECONNECT_MIN_MS;
                };
                ws.onmessage = (event) => {
                    try {
                        dispatch(JSON.parse(event.data as string) as ResourceEvent);
                    } catch {
                        // Malformed frame - ignore rather than tear down the whole feed.
                    }
                };
                ws.onclose = () => {
                    if (!cancelled) {
                        scheduleReconnect();
                    }
                };
            } catch {
                // Minting a ticket failed (backend briefly unreachable, a dropped
                // connection right as the token expired) - the same backoff applies.
                scheduleReconnect();
            }
        }

        void connect();

        return () => {
            cancelled = true;
            clearTimeout(retryTimer);
            ws?.close();
        };
    }, [authenticated]);

    const subscribe = (kind: string, listener: Listener) => {
        const listeners = listenersRef.current;
        if (!listeners.has(kind)) {
            listeners.set(kind, new Set());
        }
        listeners.get(kind)!.add(listener);
        return () => {
            listeners.get(kind)?.delete(listener);
        };
    };

    return <EventsContext.Provider value={{ subscribe }}>{children}</EventsContext.Provider>;
}

/**
 * useResourceEvents calls onEvent whenever a `kind` object changes - pass ''
 * to disable (so a caller can make this conditional without violating the
 * rules of hooks). onEvent doesn't need to be stable across renders; the
 * latest one is always used, and only `kind` changing resubscribes.
 */
export function useResourceEvents(kind: string, onEvent: (ev: ResourceEvent) => void) {
    const ctx = useContext(EventsContext);
    const onEventRef = useRef(onEvent);
    useLayoutEffect(() => {
        onEventRef.current = onEvent;
    });

    useEffect(() => {
        if (!ctx || !kind) {
            return;
        }
        return ctx.subscribe(kind, (ev) => onEventRef.current(ev));
    }, [ctx, kind]);
}
