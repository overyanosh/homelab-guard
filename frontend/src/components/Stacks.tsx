import { useCallback, useEffect, useState } from 'react';
import {
  Layers,
  Plus,
  RefreshCw,
  Rocket,
  Trash2,
  FileCode,
  FileText,
  Loader2,
  PlayCircle,
  AlertCircle,
} from 'lucide-react';
import { CodeEditor } from './CodeEditor';
import {
  ComposeValidationPanel,
  EnvGapBadge,
  addEnvKeys,
  type ValidationResult,
} from './ComposeValidation';

const cardCls = 'rounded-xl border border-[#2E2A27] bg-[#1C1A18]/40 p-5';
const ghostBtn =
  'inline-flex items-center gap-2 px-3 py-1.5 rounded-md bg-[#2E2A27] hover:bg-[#3a3531] text-sm transition-colors';
const dangerBtn =
  'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-red-500/10 text-red-400 hover:bg-red-500/20 text-xs font-medium transition-colors disabled:opacity-40 disabled:cursor-not-allowed';

const STACK_TEMPLATE = `services:
  web:
    image: docker.io/library/nginx:alpine
    restart: unless-stopped
    ports:
      - "8080:80"
  cache:
    image: docker.io/library/redis:7-alpine
    restart: unless-stopped
`;

interface StackService {
  service: string;
  container_name: string;
  state: string;
}

interface StackItem {
  name: string;
  services: StackService[];
  total: number;
  running: number;
}

/* ------------------------------------------------------------------ */
/* Stack list                                                          */
/* ------------------------------------------------------------------ */

export function StacksPanel({
  refreshKey,
  onEdit,
}: {
  refreshKey: number;
  onEdit: (name: string) => void;
}) {
  const [stacks, setStacks] = useState<StackItem[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const res = await fetch('/api/docker/stacks', { cache: 'no-store' });
      if (res.ok) setStacks((await res.json()) ?? []);
      else setError(await res.text());
    } catch (err) {
      setError(String(err));
    }
  }, []);

  useEffect(() => {
    load();
  }, [load, refreshKey]);

  const down = async (name: string) => {
    if (!confirm(`Arrêter et supprimer tous les conteneurs de la stack « ${name} » ?`)) return;
    setBusy(name);
    setError(null);
    setNotice(null);
    try {
      const res = await fetch(`/api/docker/stacks/${encodeURIComponent(name)}`, {
        method: 'DELETE',
      });
      const text = await res.text();
      if (!res.ok) {
        setError(text);
        return;
      }
      let removed: string[] = [];
      let failed: string[] = [];
      try {
        const data = JSON.parse(text);
        removed = data.removed ?? [];
        failed = data.failed ?? [];
      } catch {
        /* empty body */
      }
      if (failed.length > 0) {
        setError(
          `${removed.length} conteneur(s) supprimé(s), ${failed.length} échec(s) : ${failed.join(' ; ')}`
        );
      } else {
        setNotice(
          removed.length > 0
            ? `${removed.length} conteneur(s) supprimé(s) : ${removed.join(', ')}`
            : 'Aucun conteneur associé à supprimer.'
        );
      }
      await load();
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className={cardCls}>
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2">
          <Layers className="w-4 h-4 text-orange-500" />
          <h2 className="text-sm font-semibold">Stacks déployées ({stacks.length})</h2>
        </div>
        <button onClick={load} className={ghostBtn} type="button">
          <RefreshCw className="w-4 h-4" /> Actualiser
        </button>
      </div>

      {stacks.length === 0 ? (
        <div className="rounded-lg border border-dashed border-[#2E2A27] p-8 text-center text-xs text-stone-500">
          Aucune stack déployée. Utilisez le bouton « Stacks » pour déployer un compose
          multi-services.
        </div>
      ) : (
        <div className="space-y-3">
          {stacks.map((s) => (
            <div key={s.name} className="rounded-lg border border-[#2E2A27] bg-[#121110] p-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex items-center gap-2">
                  <span className="font-semibold text-sm">{s.name}</span>
                  <span
                    className={`px-2 py-0.5 rounded-full text-[10px] ${
                      s.running === s.total
                        ? 'bg-emerald-500/15 text-emerald-500'
                        : 'bg-orange-500/15 text-orange-400'
                    }`}
                  >
                    {s.running}/{s.total} actif(s)
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <button onClick={() => onEdit(s.name)} className={ghostBtn} type="button">
                    <FileCode className="w-4 h-4" /> Modifier / Redéployer
                  </button>
                  <button
                    onClick={() => down(s.name)}
                    disabled={busy === s.name}
                    className={dangerBtn}
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                    {busy === s.name ? 'Suppression…' : 'Arrêter et supprimer'}
                  </button>
                </div>
              </div>
              <div className="mt-3 flex flex-wrap gap-2">
                {s.services.map((svc) => (
                  <span
                    key={svc.container_name}
                    className="inline-flex items-center gap-1.5 px-2 py-1 rounded-md bg-[#1C1A18] border border-[#2E2A27] text-[11px] text-stone-300"
                    title={svc.container_name}
                  >
                    <span
                      className={`w-1.5 h-1.5 rounded-full ${
                        svc.state === 'running' ? 'bg-emerald-500' : 'bg-red-500'
                      }`}
                    />
                    {svc.service}
                    <span className="text-stone-500 font-mono">{svc.container_name}</span>
                  </span>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
      {notice && (
        <div className="mt-4 flex items-start gap-2 rounded-lg border border-emerald-500/30 bg-emerald-950/20 p-3 text-xs text-emerald-300">
          <AlertCircle className="w-4 h-4 mt-0.5 shrink-0" />
          <span>{notice}</span>
        </div>
      )}
      {error && (
        <div className="mt-4 flex items-start gap-2 rounded-lg border border-red-500/30 bg-red-950/30 p-3 text-xs text-red-300">
          <AlertCircle className="w-4 h-4 mt-0.5 shrink-0" />
          <pre className="whitespace-pre-wrap font-sans flex-1">{error}</pre>
        </div>
      )}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Stack editor modal                                                  */
/* ------------------------------------------------------------------ */

export function StackEditor({
  open,
  initialName,
  onClose,
  onDeployed,
}: {
  open: boolean;
  initialName: string | null;
  onClose: () => void;
  onDeployed: () => void;
}) {
  const [name, setName] = useState('');
  const [yamlDoc, setYamlDoc] = useState('');
  const [env, setEnv] = useState('');
  const [envExists, setEnvExists] = useState(false);
  const [tab, setTab] = useState<'compose' | 'env'>('compose');
  const [validation, setValidation] = useState<ValidationResult | null>(null);
  const [validating, setValidating] = useState(false);
  const [deploying, setDeploying] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [results, setResults] = useState<
    { service: string; container_name: string; status: string; message?: string; logs?: string }[] | null
  >(null);

  // Load an existing stack for edit, or start from the template.
  useEffect(() => {
    if (!open) return;
    setMsg(null);
    setResults(null);
    setValidation(null);
    setTab('compose');
    if (!initialName) {
      setName('');
      setYamlDoc(STACK_TEMPLATE);
      setEnv('');
      setEnvExists(false);
      return;
    }
    setName(initialName);
    (async () => {
      const res = await fetch(`/api/docker/stacks/${encodeURIComponent(initialName)}`, {
        cache: 'no-store',
      });
      if (res.ok) {
        const data = await res.json();
        setYamlDoc(data.yaml || STACK_TEMPLATE);
        setEnv(data.env || '');
        setEnvExists(Boolean((data.env || '').trim()));
      } else {
        setYamlDoc(STACK_TEMPLATE);
      }
    })();
  }, [open, initialName]);

  // Debounced validation.
  useEffect(() => {
    if (!open) return;
    const timer = setTimeout(async () => {
      setValidating(true);
      try {
        const res = await fetch('/api/docker/compose/validate', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ yaml: yamlDoc, env: envExists ? env : '' }),
        });
        if (res.ok) setValidation(await res.json());
      } catch {
        /* ignore */
      } finally {
        setValidating(false);
      }
    }, 350);
    return () => clearTimeout(timer);
  }, [open, yamlDoc, env, envExists]);

  // Auto-add missing .env keys.
  useEffect(() => {
    if (!open) return;
    const missing = validation?.missing_env ?? [];
    if (missing.length === 0) return;
    setEnvExists(true);
    setEnv((prev) => addEnvKeys(prev, missing));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, validation]);

  const deploy = async () => {
    if (!name.trim()) return;
    setDeploying(true);
    setMsg(null);
    setResults(null);
    try {
      const res = await fetch('/api/docker/stacks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: name.trim(), yaml: yamlDoc, env: envExists ? env : '' }),
      });
      if (res.ok) {
        const data = await res.json();
        setResults(data.services || []);
        onDeployed();
      } else {
        const text = await res.text();
        try {
          const parsed = JSON.parse(text);
          if (parsed && Array.isArray(parsed.errors)) {
            setValidation(parsed);
            setMsg('Compose invalide — corrigez les erreurs signalées.');
          } else {
            setMsg(parsed.message || text);
          }
        } catch {
          setMsg(text);
        }
      }
    } catch (err) {
      setMsg(String(err));
    }
    setDeploying(false);
  };

  if (!open) return null;

  const invalid = validation !== null && !validation.valid;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm p-4">
      <div className="flex max-h-[90vh] w-full max-w-3xl flex-col bg-[#1C1A18] rounded-xl border border-[#2E2A27] shadow-2xl">
        <div className="flex shrink-0 flex-wrap items-center gap-3 p-4 border-b border-[#2E2A27]">
          <div className="flex items-center gap-2">
            <Layers className="w-4 h-4 text-orange-500" />
            <h3 className="text-lg font-semibold text-stone-100">Stack</h3>
          </div>
          <input
            autoFocus
            autoComplete="off"
            spellCheck={false}
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="nom-de-la-stack"
            className="w-56 rounded-lg border border-[#2E2A27] bg-[#121110] px-3 py-1.5 font-mono text-sm text-orange-400 placeholder-stone-600 focus:border-orange-500 focus:outline-none"
          />
          <span className="px-2 py-0.5 rounded-full text-[10px] bg-orange-500/15 text-orange-400">
            multi-services
          </span>
          <button onClick={onClose} className="ml-auto text-stone-400 hover:text-white transition-colors">
            ✕
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto p-4">
          <div className="flex items-center gap-1 mb-3 border-b border-[#2E2A27]">
            <button
              onClick={() => setTab('compose')}
              className={`inline-flex items-center gap-2 px-3 py-2 text-xs font-medium border-b-2 -mb-px transition-colors ${
                tab === 'compose'
                  ? 'border-orange-500 text-orange-500'
                  : 'border-transparent text-stone-400 hover:text-stone-200'
              }`}
            >
              <FileCode className="w-3.5 h-3.5" /> docker-compose.yml
            </button>
            {envExists ? (
              <button
                onClick={() => setTab('env')}
                className={`inline-flex items-center gap-2 px-3 py-2 text-xs font-medium border-b-2 -mb-px transition-colors ${
                  tab === 'env'
                    ? 'border-orange-500 text-orange-500'
                    : 'border-transparent text-stone-400 hover:text-stone-200'
                }`}
              >
                <FileText className="w-3.5 h-3.5" /> .env
              </button>
            ) : (
              <button
                onClick={() => {
                  setEnvExists(true);
                  setTab('env');
                }}
                className="inline-flex items-center gap-2 px-3 py-2 text-xs font-medium border-b-2 border-transparent -mb-px text-stone-500 hover:text-orange-500 transition-colors"
              >
                <Plus className="w-3.5 h-3.5" /> .env
              </button>
            )}
            <EnvGapBadge validation={validation} />
            <span className="ml-auto text-[11px] text-stone-500">
              {validation?.services.length ?? 0} service(s)
            </span>
          </div>

          {tab === 'compose' ? (
            <CodeEditor
              value={yamlDoc}
              onChange={setYamlDoc}
              height="min(38vh, 420px)"
              errorLines={(validation?.errors ?? []).map((e) => e.line ?? 0).filter((n) => n > 0)}
            />
          ) : (
            <CodeEditor
              value={env}
              onChange={setEnv}
              placeholder="CLE=VALEUR"
              tone="env"
              height="min(38vh, 420px)"
            />
          )}

          <ComposeValidationPanel
            validation={validation}
            validating={validating}
            envText={env}
            onEnvChange={setEnv}
            onEnvUsed={() => setEnvExists(true)}
          />


          {results && (
            <div className="mt-3 rounded-lg border border-[#2E2A27] bg-[#121110] p-3 text-xs">
              <p className="font-medium text-stone-200 mb-2 flex items-center gap-2">
                <PlayCircle className="w-4 h-4 text-orange-500" /> Résultat du déploiement
              </p>
              <ul className="space-y-1">
                {results.map((r) => (
                  <li key={r.container_name} className="flex flex-wrap items-center gap-2">
                    <span
                      className={`w-1.5 h-1.5 rounded-full ${
                        r.status === 'failed' ? 'bg-red-500' : 'bg-emerald-500'
                      }`}
                    />
                    <code className="font-mono text-[11px] text-stone-200">{r.service}</code>
                    <span className="text-stone-500 font-mono text-[11px]">{r.container_name}</span>
                    <span
                      className={
                        r.status === 'failed' ? 'text-red-300 text-[11px]' : 'text-emerald-300 text-[11px]'
                      }
                    >
                      {r.status}
                    </span>
                    {r.message && <span className="text-stone-500 text-[11px]">— {r.message}</span>}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>

        <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-t border-[#2E2A27] p-4">
          <p className="text-xs text-stone-500">
            Chaque service devient un conteneur, étiqueté <code className="font-mono">guard.stack</code>.
          </p>
          <div className="flex items-center gap-3">
            {msg && <span className="text-xs text-red-300">{msg}</span>}
            <button
              onClick={deploy}
              disabled={deploying || validating || invalid || !name.trim()}
              title={invalid ? 'Corrigez les erreurs de validation avant de déployer' : 'Déployer la stack'}
              className="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-orange-600 hover:bg-orange-500 text-sm font-medium disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {deploying ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <Rocket className="w-4 h-4" />
              )}
              {deploying ? 'Déploiement...' : 'Déployer la stack'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
