import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import {
  Plus,
  Trash2,
  RefreshCw,
  Copy,
  Check,
  Lock,
  AlertCircle,
  KeyRound,
  Bot,
  Server,
  Box,
} from 'lucide-react';

const cardCls = 'rounded-xl border border-[#2E2A27] bg-[#1C1A18]/40 p-5';
const inputCls =
  'w-full rounded-lg border border-[#2E2A27] bg-[#121110] px-3 py-2 text-sm text-stone-100 placeholder-stone-600 focus:border-orange-500 focus:outline-none';
const labelCls = 'block text-xs font-medium text-stone-400 mb-1';
const primaryBtn =
  'inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-orange-600 hover:bg-orange-500 text-sm font-medium disabled:opacity-50 transition-colors';
const dangerBtn =
  'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-red-500/10 text-red-400 hover:bg-red-500/20 text-xs font-medium transition-colors disabled:opacity-40 disabled:cursor-not-allowed';
const ghostBtn =
  'inline-flex items-center gap-2 px-3 py-1.5 rounded-md bg-[#2E2A27] hover:bg-[#3a3531] text-sm transition-colors';

function ErrorNote({ message }: { message: string }) {
  return (
    <div className="mt-4 flex items-start gap-2 rounded-lg border border-red-500/30 bg-red-950/30 p-3 text-sm text-red-300">
      <AlertCircle className="w-4 h-4 mt-0.5 shrink-0" />
      <pre className="whitespace-pre-wrap font-sans">{message}</pre>
    </div>
  );
}

// Copies text even in a non-secure context (plain http://), where
// navigator.clipboard is unavailable: falls back to a hidden textarea +
// document.execCommand('copy'), and reports failure instead of silently doing
// nothing.
function legacyCopy(value: string) {
  const ta = document.createElement('textarea');
  ta.value = value;
  ta.setAttribute('readonly', '');
  ta.style.position = 'fixed';
  ta.style.top = '-1000px';
  ta.style.left = '-1000px';
  ta.style.opacity = '0';
  document.body.appendChild(ta);
  ta.select();
  ta.setSelectionRange(0, value.length);
  const ok = document.execCommand('copy');
  document.body.removeChild(ta);
  if (!ok) throw new Error('execCommand copy failed');
}

function CopyButton({ text }: { text: string }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle');

  const copy = async () => {
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
      } else {
        legacyCopy(text);
      }
      setState('copied');
    } catch {
      try {
        legacyCopy(text);
        setState('copied');
      } catch {
        setState('failed');
      }
    }
    setTimeout(() => setState('idle'), 2500);
  };

  return (
    <div className="flex flex-col items-end gap-1">
      <button onClick={copy} className={ghostBtn} type="button">
        {state === 'copied' ? (
          <Check className="w-4 h-4 text-emerald-500" />
        ) : (
          <Copy className="w-4 h-4" />
        )}
        {state === 'copied' ? 'Copié' : 'Copier'}
      </button>
      {state === 'failed' && (
        <span className="max-w-40 text-right text-[10px] text-orange-400">
          Copie bloquée par le navigateur : sélectionnez le texte et faites Ctrl+C.
        </span>
      )}
    </div>
  );
}

function SectionTitle({ icon: Icon, title, subtitle }: { icon: typeof Box; title: string; subtitle?: string }) {
  return (
    <div className="flex items-start gap-3 mb-4">
      <div className="p-2 rounded-lg bg-orange-500/10 text-orange-500">
        <Icon className="w-4 h-4" />
      </div>
      <div>
        <h2 className="font-semibold text-stone-100">{title}</h2>
        {subtitle && <p className="text-xs text-stone-500 mt-0.5">{subtitle}</p>}
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Docker & Podman agents                                              */
/* ------------------------------------------------------------------ */

interface AgentNode {
  id: number;
  name: string;
  created_at?: string;
  online: boolean;
  enroll_token?: string;
}

interface CreatedAgent {
  name: string;
  token: string;
  runtime: 'docker' | 'podman';
  serverUrl: string;
}

const isLocal = (name: string) => name.trim() === 'Local Host';

export function DockerSettings() {
  const [nodes, setNodes] = useState<AgentNode[]>([]);
  const [name, setName] = useState('');
  const [runtime, setRuntime] = useState<'docker' | 'podman'>('podman');
  const [serverUrl, setServerUrl] = useState(window.location.origin);
  const [created, setCreated] = useState<CreatedAgent | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = async () => {
    const res = await fetch('/api/nodes');
    if (res.ok) setNodes((await res.json()) ?? []);
  };

  useEffect(() => {
    load();
  }, []);

  const create = async (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/nodes', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: name.trim() }),
      });
      if (!res.ok) {
        setError(await res.text());
        return;
      }
      const node = await res.json();
      setCreated({ name: node.name, token: node.enroll_token, runtime, serverUrl });
      setName('');
      await load();
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
    }
  };

  const remove = async (id: number) => {
    if (!confirm('Supprimer cet agent ? Cette action est irréversible.')) return;
    setError(null);
    const res = await fetch(`/api/nodes/${id}`, { method: 'DELETE' });
    if (!res.ok) {
      setError(await res.text());
      return;
    }
    load();
  };

  const deployCmd = created
    ? [
        `${created.runtime} run -d --name homelab-guard-agent \\`,
        `  --restart unless-stopped \\`,
        `  -e GUARD_SERVER_URL=${created.serverUrl} \\`,
        `  -e GUARD_ENROLL_TOKEN=${created.token} \\`,
        `  ghcr.io/overyanosh/homelab-guard-agent:latest`,
      ].join('\n')
    : '';

  return (
    <div className="space-y-6 max-w-4xl">
      <div className={cardCls}>
        <SectionTitle
          icon={Box}
          title="Déployer un nouvel agent Docker / Podman"
          subtitle="Enregistre un nœud distant, puis déploie l'agent avec le jeton d'enrôlement généré."
        />
        <form onSubmit={create} className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div>
            <label className={labelCls}>Nom du nœud</label>
            <input
              className={inputCls}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="ex: nas-01"
            />
          </div>
          <div>
            <label className={labelCls}>Runtime</label>
            <select
              className={inputCls}
              value={runtime}
              onChange={(e) => setRuntime(e.target.value as 'docker' | 'podman')}
            >
              <option value="podman">Podman</option>
              <option value="docker">Docker</option>
            </select>
          </div>
          <div>
            <label className={labelCls}>URL du serveur Guard</label>
            <input
              className={inputCls}
              value={serverUrl}
              onChange={(e) => setServerUrl(e.target.value)}
              placeholder="http://192.168.1.10:9015"
            />
          </div>
          <div className="md:col-span-3">
            <button type="submit" disabled={loading || !name.trim()} className={primaryBtn}>
              <Plus className="w-4 h-4" />
              {loading ? 'Enregistrement...' : "Enregistrer l'agent"}
            </button>
          </div>
        </form>

        {created && (
          <div className="mt-5 rounded-lg border border-emerald-500/30 bg-emerald-950/20 p-4">
            <p className="text-sm text-emerald-300 mb-2">
              Agent <span className="font-semibold">{created.name}</span> enregistré. Jeton d'enrôlement
              (affiché une seule fois) :
            </p>
            <div className="flex items-start gap-3">
              <pre className="flex-1 font-mono text-xs text-stone-200 bg-[#121110] p-3 rounded-lg overflow-x-auto whitespace-pre">
                {deployCmd}
              </pre>
              <CopyButton text={deployCmd} />
            </div>
            <p className="text-xs text-stone-500 mt-2">
              Collez cette commande sur la machine cible. L'agent se connectera automatiquement au hub.
            </p>
          </div>
        )}

        {error && <ErrorNote message={error} />}
      </div>

      <div className={cardCls}>
        <div className="flex items-center justify-between mb-4">
          <SectionTitle icon={Server} title="Agents enregistrés" />
          <button onClick={load} className={ghostBtn} type="button">
            <RefreshCw className="w-4 h-4" /> Actualiser
          </button>
        </div>
        <div className="overflow-x-auto rounded-lg border border-[#2E2A27]">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-[#2E2A27] text-stone-400">
              <tr>
                <th className="p-3">Nom</th>
                <th className="p-3">État</th>
                <th className="p-3">Enregistré le</th>
                <th className="p-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[#2E2A27]/60">
              {nodes.length === 0 && (
                <tr>
                  <td colSpan={4} className="p-4 text-center text-stone-500 text-xs">
                    Aucun agent enregistré.
                  </td>
                </tr>
              )}
              {nodes.map((n) => {
                const local = isLocal(n.name);
                return (
                  <tr key={n.id} className="hover:bg-[#1C1A18]/70">
                    <td className="p-3 font-medium flex items-center gap-2">
                      {n.name}
                      {local && (
                        <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] bg-[#2E2A27] text-stone-400">
                          <Lock className="w-3 h-3" /> local
                        </span>
                      )}
                    </td>
                    <td className="p-3">
                      <span
                        className={`px-2 py-0.5 rounded-full text-xs ${
                          n.online
                            ? 'bg-emerald-500/15 text-emerald-500'
                            : 'bg-[#2E2A27] text-stone-400'
                        }`}
                      >
                        {n.online ? 'en ligne' : 'hors ligne'}
                      </span>
                    </td>
                    <td className="p-3 text-xs text-stone-500">{n.created_at || '—'}</td>
                    <td className="p-3 text-right">
                      <button
                        onClick={() => remove(n.id)}
                        disabled={local}
                        title={local ? "L'agent local ne peut pas être supprimé" : 'Supprimer'}
                        className={dangerBtn}
                      >
                        <Trash2 className="w-3.5 h-3.5" /> Supprimer
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Proxmox VE connections                                             */
/* ------------------------------------------------------------------ */

interface PveCluster {
  id: number;
  name: string;
  endpoint: string;
  token_id: string;
  insecure_tls: boolean;
}

export function ProxmoxSettings() {
  const [clusters, setClusters] = useState<PveCluster[]>([]);
  const [form, setForm] = useState({
    name: '',
    endpoint: '',
    token_id: '',
    token_secret: '',
    insecure_tls: true,
  });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = async () => {
    const res = await fetch('/api/proxmox/clusters');
    if (res.ok) setClusters((await res.json()) ?? []);
  };

  useEffect(() => {
    load();
  }, []);

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/proxmox/clusters', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form),
      });
      if (!res.ok) {
        setError(await res.text());
        return;
      }
      setForm({ name: '', endpoint: '', token_id: '', token_secret: '', insecure_tls: true });
      await load();
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
    }
  };

  const remove = async (id: number) => {
    if (!confirm('Supprimer cette connexion Proxmox VE ?')) return;
    setError(null);
    const res = await fetch(`/api/proxmox/clusters/${id}`, { method: 'DELETE' });
    if (!res.ok) {
      setError(await res.text());
      return;
    }
    load();
  };

  return (
    <div className="space-y-6 max-w-4xl">
      <div className={cardCls}>
        <SectionTitle
          icon={Server}
          title="Nouvelle connexion Proxmox VE"
          subtitle="La connexion est testée avant d'être enregistrée."
        />
        <form onSubmit={create} className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <label className={labelCls}>Nom</label>
            <input
              className={inputCls}
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="ex: pve-prod"
            />
          </div>
          <div>
            <label className={labelCls}>Endpoint</label>
            <input
              className={inputCls}
              value={form.endpoint}
              onChange={(e) => setForm({ ...form, endpoint: e.target.value })}
              placeholder="https://pve.local:8006"
            />
          </div>
          <div>
            <label className={labelCls}>Token ID</label>
            <input
              className={inputCls}
              value={form.token_id}
              onChange={(e) => setForm({ ...form, token_id: e.target.value })}
              placeholder="user@realm!tokenid"
            />
          </div>
          <div>
            <label className={labelCls}>Token Secret</label>
            <input
              type="password"
              className={inputCls}
              value={form.token_secret}
              onChange={(e) => setForm({ ...form, token_secret: e.target.value })}
              placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
            />
          </div>
          <label className="md:col-span-2 flex items-center gap-2 text-sm text-stone-300">
            <input
              type="checkbox"
              checked={form.insecure_tls}
              onChange={(e) => setForm({ ...form, insecure_tls: e.target.checked })}
              className="accent-orange-500"
            />
            Ignorer la vérification TLS (certificat auto-signé)
          </label>
          <div className="md:col-span-2">
            <button
              type="submit"
              disabled={loading || !form.name.trim() || !form.endpoint.trim()}
              className={primaryBtn}
            >
              <Plus className="w-4 h-4" />
              {loading ? 'Test & enregistrement...' : 'Tester & enregistrer'}
            </button>
          </div>
        </form>
        {error && <ErrorNote message={error} />}
      </div>

      <div className={cardCls}>
        <div className="flex items-center justify-between mb-4">
          <SectionTitle icon={Server} title="Connexions enregistrées" />
          <button onClick={load} className={ghostBtn} type="button">
            <RefreshCw className="w-4 h-4" /> Actualiser
          </button>
        </div>
        <div className="overflow-x-auto rounded-lg border border-[#2E2A27]">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-[#2E2A27] text-stone-400">
              <tr>
                <th className="p-3">Nom</th>
                <th className="p-3">Endpoint</th>
                <th className="p-3">Token</th>
                <th className="p-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[#2E2A27]/60">
              {clusters.length === 0 && (
                <tr>
                  <td colSpan={4} className="p-4 text-center text-stone-500 text-xs">
                    Aucune connexion Proxmox VE.
                  </td>
                </tr>
              )}
              {clusters.map((c) => (
                <tr key={c.id} className="hover:bg-[#1C1A18]/70">
                  <td className="p-3 font-medium">{c.name}</td>
                  <td className="p-3 font-mono text-xs text-stone-400">{c.endpoint}</td>
                  <td className="p-3 font-mono text-xs text-stone-400">{c.token_id}</td>
                  <td className="p-3 text-right">
                    <button onClick={() => remove(c.id)} className={dangerBtn}>
                      <Trash2 className="w-3.5 h-3.5" /> Supprimer
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* SSH bastion connections                                            */
/* ------------------------------------------------------------------ */

interface SshHost {
  id: number;
  name: string;
  host: string;
  port: number;
  username: string;
  public_key?: string;
}

export function SshSettings() {
  const [hosts, setHosts] = useState<SshHost[]>([]);
  const [form, setForm] = useState({ name: '', host: '', port: 22, username: '' });
  const [createdKey, setCreatedKey] = useState<{ name: string; publicKey: string } | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = async () => {
    const res = await fetch('/api/ssh/hosts');
    if (res.ok) setHosts((await res.json()) ?? []);
  };

  useEffect(() => {
    load();
  }, []);

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/ssh/hosts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...form, port: Number(form.port) || 22 }),
      });
      if (!res.ok) {
        setError(await res.text());
        return;
      }
      const host = await res.json();
      setCreatedKey({ name: host.name, publicKey: host.public_key || '' });
      setForm({ name: '', host: '', port: 22, username: '' });
      await load();
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
    }
  };

  const remove = async (id: number) => {
    if (!confirm('Supprimer cet hôte SSH ?')) return;
    setError(null);
    const res = await fetch(`/api/ssh/hosts/${id}`, { method: 'DELETE' });
    if (!res.ok) {
      setError(await res.text());
      return;
    }
    load();
  };

  return (
    <div className="space-y-6 max-w-4xl">
      <div className={cardCls}>
        <SectionTitle
          icon={KeyRound}
          title="Nouvelle connexion SSH"
          subtitle="Une paire de clés Ed25519 est générée automatiquement pour l'hôte."
        />
        <form onSubmit={create} className="grid grid-cols-1 md:grid-cols-4 gap-4">
          <div>
            <label className={labelCls}>Nom</label>
            <input
              className={inputCls}
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="ex: web-01"
            />
          </div>
          <div>
            <label className={labelCls}>Hôte</label>
            <input
              className={inputCls}
              value={form.host}
              onChange={(e) => setForm({ ...form, host: e.target.value })}
              placeholder="192.168.1.20"
            />
          </div>
          <div>
            <label className={labelCls}>Port</label>
            <input
              type="number"
              className={inputCls}
              value={form.port}
              onChange={(e) => setForm({ ...form, port: Number(e.target.value) })}
            />
          </div>
          <div>
            <label className={labelCls}>Utilisateur</label>
            <input
              className={inputCls}
              value={form.username}
              onChange={(e) => setForm({ ...form, username: e.target.value })}
              placeholder="root"
            />
          </div>
          <div className="md:col-span-4">
            <button
              type="submit"
              disabled={loading || !form.name.trim() || !form.host.trim() || !form.username.trim()}
              className={primaryBtn}
            >
              <Plus className="w-4 h-4" />
              {loading ? 'Enregistrement...' : "Enregistrer l'hôte"}
            </button>
          </div>
        </form>

        {createdKey && (
          <div className="mt-5 rounded-lg border border-emerald-500/30 bg-emerald-950/20 p-4">
            <p className="text-sm text-emerald-300 mb-2">
              Hôte <span className="font-semibold">{createdKey.name}</span> créé. Ajoutez cette clé
              publique dans <code className="font-mono">~/.ssh/authorized_keys</code> :
            </p>
            <div className="flex items-start gap-3">
              <pre className="flex-1 font-mono text-xs text-stone-200 bg-[#121110] p-3 rounded-lg overflow-x-auto whitespace-pre-wrap break-all">
                {createdKey.publicKey}
              </pre>
              <CopyButton text={createdKey.publicKey} />
            </div>
          </div>
        )}

        {error && <ErrorNote message={error} />}
      </div>

      <div className={cardCls}>
        <div className="flex items-center justify-between mb-4">
          <SectionTitle icon={KeyRound} title="Hôtes enregistrés" />
          <button onClick={load} className={ghostBtn} type="button">
            <RefreshCw className="w-4 h-4" /> Actualiser
          </button>
        </div>
        <div className="overflow-x-auto rounded-lg border border-[#2E2A27]">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-[#2E2A27] text-stone-400">
              <tr>
                <th className="p-3">Nom</th>
                <th className="p-3">Adresse</th>
                <th className="p-3">Utilisateur</th>
                <th className="p-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[#2E2A27]/60">
              {hosts.length === 0 && (
                <tr>
                  <td colSpan={4} className="p-4 text-center text-stone-500 text-xs">
                    Aucun hôte SSH enregistré.
                  </td>
                </tr>
              )}
              {hosts.map((h) => (
                <tr key={h.id} className="hover:bg-[#1C1A18]/70">
                  <td className="p-3 font-medium">{h.name}</td>
                  <td className="p-3 font-mono text-xs text-stone-400">
                    {h.host}:{h.port}
                  </td>
                  <td className="p-3 text-xs text-stone-300">{h.username}</td>
                  <td className="p-3 text-right">
                    <button onClick={() => remove(h.id)} className={dangerBtn}>
                      <Trash2 className="w-3.5 h-3.5" /> Supprimer
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* AI Ops worker configuration                                        */
/* ------------------------------------------------------------------ */

interface AiConfig {
  worker_online: boolean;
  model: string;
  base_url: string;
}

export function AiSettings() {
  const [config, setConfig] = useState<AiConfig | null>(null);
  const [loading, setLoading] = useState(false);

  const load = async () => {
    setLoading(true);
    try {
      const res = await fetch('/api/ai/config');
      if (res.ok) setConfig(await res.json());
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  return (
    <div className="space-y-6 max-w-3xl">
      <div className={cardCls}>
        <div className="flex items-center justify-between mb-4">
          <SectionTitle
            icon={Bot}
            title="Worker IA & Ops"
            subtitle="État du worker local qui génère les stacks Compose et diagnostique les régressions."
          />
          <button onClick={load} className={ghostBtn} type="button" disabled={loading}>
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} /> Tester
          </button>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div className="rounded-lg border border-[#2E2A27] bg-[#121110] p-4">
            <p className="text-xs text-stone-500 mb-1">État du worker</p>
            {config ? (
              <span
                className={`inline-flex items-center gap-2 px-2 py-0.5 rounded-full text-xs ${
                  config.worker_online
                    ? 'bg-emerald-500/15 text-emerald-500'
                    : 'bg-red-500/15 text-red-400'
                }`}
              >
                <span
                  className={`w-1.5 h-1.5 rounded-full ${
                    config.worker_online ? 'bg-emerald-500' : 'bg-red-500'
                  }`}
                />
                {config.worker_online ? 'en ligne' : 'hors ligne'}
              </span>
            ) : (
              <span className="text-xs text-stone-500">—</span>
            )}
          </div>
          <div className="rounded-lg border border-[#2E2A27] bg-[#121110] p-4">
            <p className="text-xs text-stone-500 mb-1">Modèle</p>
            <p className="font-mono text-sm text-stone-200">{config?.model || '—'}</p>
          </div>
          <div className="rounded-lg border border-[#2E2A27] bg-[#121110] p-4 sm:col-span-2">
            <p className="text-xs text-stone-500 mb-1">Endpoint du modèle (base URL)</p>
            <p className="font-mono text-xs text-stone-300 break-all">{config?.base_url || '—'}</p>
          </div>
        </div>

        <div className="mt-4 flex items-start gap-2 rounded-lg border border-[#2E2A27] bg-[#1C1A18]/60 p-3 text-xs text-stone-400">
          <AlertCircle className="w-4 h-4 mt-0.5 shrink-0 text-orange-500" />
          <p>
            La configuration du worker IA (modèle, endpoint, clé API) se définit via les variables
            d'environnement du conteneur : <code className="font-mono">AI_BASE_URL</code>,{' '}
            <code className="font-mono">AI_MODEL</code>, <code className="font-mono">AI_API_KEY</code>.
            Modifiez ces variables puis redémarrez le conteneur pour les appliquer.
          </p>
        </div>
      </div>
    </div>
  );
}
