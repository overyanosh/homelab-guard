import { useEffect, useRef } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';

interface SshTerminalProps {
  hostId: number;
  hostLabel: string;
}

export function SshTerminal({ hostId, hostLabel }: SshTerminalProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!containerRef.current) return;

    const term = new Terminal({
      cursorBlink: true,
      fontFamily: 'JetBrains Mono, Fira Code, monospace',
      fontSize: 13,
      theme: {
        background: '#121110',
        foreground: '#f5f2ef',
        cursor: '#f97316',
        selectionBackground: '#f9731640',
      },
    });
    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(containerRef.current);
    fitAddon.fit();

    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${proto}//${window.location.host}/ws/ssh/${hostId}`);
    ws.binaryType = 'arraybuffer';

    ws.onopen = () => {
      term.writeln(`\x1b[32m[Homelab Guard]\x1b[0m Connected to ${hostLabel} via encrypted Ed25519 vault...`);
    };

    ws.onmessage = (event) => {
      if (event.data instanceof ArrayBuffer) {
        term.write(new Uint8Array(event.data));
      } else {
        term.write(event.data);
      }
    };

    ws.onclose = () => {
      term.writeln('\r\n\x1b[31m[Disconnected]\x1b[0m SSH session closed.');
    };

    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(new TextEncoder().encode(data));
      }
    });

    const handleResize = () => fitAddon.fit();
    window.addEventListener('resize', handleResize);

    return () => {
      window.removeEventListener('resize', handleResize);
      ws.close();
      term.dispose();
    };
  }, [hostId, hostLabel]);

  return (
    <div className="rounded-lg border border-slate-800 bg-[#090d16] p-2 shadow-inner">
      <div ref={containerRef} className="h-[460px] w-full" />
    </div>
  );
}
