/*
 * Copyright 2026
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// One xterm.js terminal bound to one backend shell (internal/api/shell.go).
// Connecting is two steps: mint a single-use ticket with an ordinary
// bearer-authenticated request, then open the WebSocket with it - the
// browser's WebSocket API can't send an Authorization header, and the
// long-lived token must never appear in a URL.
import '@xterm/xterm/css/xterm.css';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import { Terminal } from '@xterm/xterm';
import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '../api';
import { errorMessage } from '../components/ui';

export type SessionStatus = 'connecting' | 'connected' | 'closed';

const theme = {
  background: '#0f1115',
  foreground: '#e3e6ea',
  cursor: '#fd4b2d',
  selectionBackground: '#3a4350',
};

export function TerminalSession({
  context,
  prefill,
  active,
  onStatus,
}: {
  context: string;
  prefill?: string;
  active: boolean;
  onStatus: (status: SessionStatus) => void;
}) {
  const hostRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | undefined>(undefined);
  const fitRef = useRef<FitAddon | undefined>(undefined);
  const socketRef = useRef<WebSocket | undefined>(undefined);
  // Bumped by "Reconnect" to tear down and rebuild the connection.
  const [attempt, setAttempt] = useState(0);
  const [status, setStatus] = useState<SessionStatus>('connecting');
  const [error, setError] = useState<string>();

  // Held in a ref: the parent passes a fresh closure every render, and the
  // connection effect below must not reconnect because of that.
  const onStatusRef = useRef(onStatus);
  onStatusRef.current = onStatus;
  const updateStatus = useCallback((s: SessionStatus) => {
    setStatus(s);
    onStatusRef.current(s);
  }, []);

  // The terminal itself lives as long as the pane.
  useEffect(() => {
    const term = new Terminal({
      cursorBlink: true,
      fontFamily: "ui-monospace, 'SFMono-Regular', Menlo, Consolas, monospace",
      fontSize: 13,
      scrollback: 10000,
      theme,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon());
    term.open(hostRef.current!);
    termRef.current = term;
    fitRef.current = fit;

    const send = (msg: object) => {
      const ws = socketRef.current;
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify(msg));
      }
    };
    const dataSub = term.onData(data => send({ type: 'data', data }));
    const resizeSub = term.onResize(({ cols, rows }) => send({ type: 'resize', cols, rows }));

    const observer = new ResizeObserver(() => {
      // A hidden pane measures 0x0; fitting it would shrink the remote
      // shell to nothing.
      if (hostRef.current && hostRef.current.offsetParent !== null) {
        fit.fit();
      }
    });
    observer.observe(hostRef.current!);

    return () => {
      observer.disconnect();
      dataSub.dispose();
      resizeSub.dispose();
      term.dispose();
    };
  }, []);

  // One backend session per attempt.
  useEffect(() => {
    const term = termRef.current!;
    let cancelled = false;
    let ws: WebSocket | undefined;
    updateStatus('connecting');
    setError(undefined);

    (async () => {
      try {
        const { ticket } = await api.createShellSession(context);
        if (cancelled) {
          return;
        }
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        ws = new WebSocket(
          `${proto}//${window.location.host}/api/v1/shell/connect?ticket=${encodeURIComponent(ticket)}`
        );
        ws.binaryType = 'arraybuffer';
        socketRef.current = ws;
        ws.onopen = () => {
          updateStatus('connected');
          fitRef.current?.fit();
          ws!.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
          term.focus();
        };
        // The prefill is typed once the shell shows its prompt (output that
        // ends like one), so it isn't echoed into the middle of the startup
        // banner; a fallback covers prompts that don't look like one.
        let prefillPending = Boolean(prefill) && attempt === 0;
        const typePrefill = () => {
          if (prefillPending) {
            prefillPending = false;
            // Typed, not run - see links.ts's sanitizePrefill.
            ws?.send(JSON.stringify({ type: 'data', data: prefill }));
          }
        };
        const prefillFallback = prefillPending ? setTimeout(typePrefill, 3000) : undefined;
        const decoder = new TextDecoder();
        ws.onmessage = ev => {
          const chunk = typeof ev.data === 'string' ? ev.data : new Uint8Array(ev.data as ArrayBuffer);
          term.write(chunk);
          if (prefillPending) {
            const text = typeof chunk === 'string' ? chunk : decoder.decode(chunk, { stream: true });
            if (/[$#>%]\s*$/.test(text)) {
              clearTimeout(prefillFallback);
              typePrefill();
            }
          }
        };
        ws.onclose = ev => {
          if (cancelled) {
            return;
          }
          updateStatus('closed');
          term.write(`\r\n\x1b[2m[session ended${ev.reason ? `: ${ev.reason}` : ''}]\x1b[0m\r\n`);
        };
      } catch (err) {
        if (!cancelled) {
          setError(errorMessage(err));
          updateStatus('closed');
        }
      }
    })();

    return () => {
      cancelled = true;
      ws?.close();
      socketRef.current = undefined;
    };
    // prefill is only for the first attempt; it must not reconnect.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [context, attempt, updateStatus]);

  useEffect(() => {
    if (active) {
      // Became visible: size to the real container now.
      requestAnimationFrame(() => {
        fitRef.current?.fit();
        termRef.current?.focus();
      });
    }
  }, [active]);

  return (
    <>
      {(status === 'closed' || error) && (
        <div className="shell-bar">
          {error && <span className="danger">{error}</span>}
          <button
            className="small primary"
            onClick={() => {
              termRef.current?.reset();
              setAttempt(a => a + 1);
            }}
          >
            Reconnect
          </button>
        </div>
      )}
      <div className="terminal-host" ref={hostRef} />
    </>
  );
}
