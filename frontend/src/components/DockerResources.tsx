import { useCallback, useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import {
  Network,
  HardDrive,
  Database,
  Trash2,
  RefreshCw,
  Plus,
  AlertCircle,
  CheckCircle2,
  Layers,
} from 'lucide-react';

const cardCls = 'rounded-xl border border-[#2E2A27] bg-[#1C1A18]/40 p-5';
const inputCls =
  'w-full rounded-lg border border-[#2E2A27] bg-[#121110] px-3 py-2 text-sm text-stone-100 placeholder-stone-600 focus:border-orange-500 focus:outline-none';
const labelCls = 'block text-xs font-medium text-stone-400 mb-1';
const primaryBtn =
  'inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-orange-600 hover:bg-orange-500 text-sm font-medium disabled:opacity-50 transition-colors';
const ghostBtn =
  'inline-flex items-center gap-2 px-3 py-1.5 rounded-md bg-[#2E2A27] hover:bg-[#3a3531] text-sm transition-colors';
const dangerBtn =
  'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-red-500/10 text-red-400 hover:bg-red-500/20 text-xs font-medium transition-colors disabled:opacity-40 disabled:cursor-not-allowed';
const tableHead = 'border-b border-[#2E2A27] text-stone-400';

export function formatBytes(n: number): string {
  if (!n || n < 0) return '0 o';
  const units = ['o', 'Ko', 'Mo', 'Go', 'To'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}

function ErrorNote({ message, onClose }: { message: string; onClose?: () => void }) {
  return (
    <div className="mt-4 flex items-start gap-2 rounded-lg border border-red-500/30 bg-red-950/30 p-3 text-sm text-red-300">
      <AlertCircle className="w-4 h-4 mt-0.5 shrink-0" />
      <pre className="whitespace-pre-wrap font-sans flex-1">{message}</pre>
      {onClose && (
        <button onClick={onClose} className="text-xs text-stone-400 hover:text-white">
          ✕
        </button>
      )}
    </div>
  );
}

function SuccessNote({ message }: { message: string }) {
  return (
    <div className="mt-4 flex items-start gap-2 rounded-lg border border-emerald-500/30 bg-emerald-950/20 p-3 text-sm text-emerald-300">
      <CheckCircle2 className="w-4 h-4 mt-0.5 shrink-0" />
      <span>{message}</span>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Disk usage summary                                                 */
/* ------------------------------------------------------------------ */

interface DiskUsage {
  images_size: number;
  images_reclaim: number;
  containers_size: number;
  containers_count: number;
  volumes_size: number;
  volumes_reclaim: number;
  build_cache_size: number;
  build_cache_count: number;
  total_reclaim: number;
}

export function DiskUsageBar({ refreshKey }: { refreshKey: number }) {
  const [usage, setUsage] = useState<DiskUsage | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await fetch('/api/docker/disk-usage');
      if (res.ok) setUsage(await res.json());
      else setError(await res.text());
    } catch (err) {
      setError(String(err));
    }
  }, []);

  useEffect(() => {
    load();
  }, [load, refreshKey]);

  const cleanAll = async () => {
    if (
      !confirm(
        'Récupérer l\'espace disque ?\n\n' +
          '• Images non utilisées supprimées\n' +
          '• Volumes non montés supprimés\n' +
          '• Cache de build supprimé (si le moteur le gère)\n\n' +
          'Les conteneurs en cours ne sont pas touchés.'
      )
    )
      return;
    setBusy(true);
    setError(null);
    setSuccess(null);
    let reclaimed = 0;
    try {
      const images = await fetch('/api/docker/images/prune', { method: 'POST' });
      if (images.ok) reclaimed += (await images.json()).space_reclaimed || 0;
      const volumes = await fetch('/api/docker/volumes/prune', { method: 'POST' });
      if (volumes.ok) reclaimed += (await volumes.json()).space_reclaimed || 0;
      const cache = await fetch('/api/docker/build-cache/prune', { method: 'POST' });
      if (cache.ok) {
        const data = await cache.json();
        if (data.supported === false) {
          setSuccess(
            `Nettoyage terminé — ${formatBytes(reclaimed)} récupérés. ${data.message || ''}`.trim()
          );
          await load();
          return;
        }
        reclaimed += data.space_reclaimed || 0;
      }
      setSuccess(`Nettoyage terminé — ${formatBytes(reclaimed)} récupérés.`);
      await load();
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy(false);
    }
  };

  const stats = [
    { icon: Layers, label: 'Images', value: usage?.images_size ?? 0, sub: `${formatBytes(usage?.images_reclaim ?? 0)} récupérables` },
    { icon: HardDrive, label: 'Conteneurs', value: usage?.containers_size ?? 0, sub: `${usage?.containers_count ?? 0} conteneur(s)` },
    { icon: Database, label: 'Volumes', value: usage?.volumes_size ?? 0, sub: `${formatBytes(usage?.volumes_reclaim ?? 0)} récupérables` },
    { icon: RefreshCw, label: 'Cache de build', value: usage?.build_cache_size ?? 0, sub: `${usage?.build_cache_count ?? 0} entrée(s)` },
  ];

  return (
    <div className={`${cardCls} mb-6`}>
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2">
          <HardDrive className="w-4 h-4 text-orange-500" />
          <h2 className="text-sm font-semibold">Espace disque</h2>
          {usage && usage.total_reclaim > 0 && (
            <span className="px-2 py-0.5 rounded-full text-[10px] bg-orange-500/15 text-orange-400">
              {formatBytes(usage.total_reclaim)} récupérables
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <button onClick={load} className={ghostBtn} type="button">
            <RefreshCw className="w-4 h-4" /> Actualiser
          </button>
          <button
            onClick={cleanAll}
            disabled={busy}
            className="inline-flex items-center gap-2 px-3 py-1.5 rounded-md bg-orange-600 hover:bg-orange-500 text-sm font-medium disabled:opacity-50 transition-colors"
          >
            <Trash2 className="w-4 h-4" /> {busy ? 'Nettoyage...' : "Récupérer l'espace"}
          </button>
        </div>
      </div>
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        {stats.map(({ icon: Icon, label, value, sub }) => (
          <div key={label} className="rounded-lg border border-[#2E2A27] bg-[#121110] p-3">
            <div className="flex items-center gap-2 text-xs text-stone-400 mb-1">
              <Icon className="w-3.5 h-3.5" /> {label}
            </div>
            <p className="text-lg font-semibold text-stone-100">{formatBytes(value)}</p>
            <p className="text-[11px] text-stone-500">{sub}</p>
          </div>
        ))}
      </div>
      {error && <ErrorNote message={error} onClose={() => setError(null)} />}
      {success && <SuccessNote message={success} />}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Networks                                                           */
/* ------------------------------------------------------------------ */

interface NetworkItem {
  id: string;
  name: string;
  driver: string;
  scope: string;
  internal: boolean;
  attachable: boolean;
  created: string;
  containers: number;
  in_use: boolean;
}

export function NetworksPanel({ onChanged }: { onChanged: () => void }) {
  const [networks, setNetworks] = useState<NetworkItem[]>([]);
  const [form, setForm] = useState({ name: '', driver: 'bridge', internal: false, subnet: '' });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const load = useCallback(async () => {
    const res = await fetch('/api/docker/networks');
    if (res.ok) setNetworks((await res.json()) ?? []);
    else setError(await res.text());
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError(null);
    setSuccess(null);
    try {
      const res = await fetch('/api/docker/networks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form),
      });
      if (!res.ok) {
        setError(await res.text());
        return;
      }
      setSuccess(`Réseau « ${form.name} » créé.`);
      setForm({ name: '', driver: 'bridge', internal: false, subnet: '' });
      await load();
      onChanged();
    } finally {
      setLoading(false);
    }
  };

  const remove = async (n: NetworkItem) => {
    if (!confirm(`Supprimer le réseau « ${n.name} » ?`)) return;
    setError(null);
    setSuccess(null);
    const res = await fetch(`/api/docker/networks/${n.id}`, { method: 'DELETE' });
    if (!res.ok) {
      setError(await res.text());
      return;
    }
    setSuccess(`Réseau « ${n.name} » supprimé.`);
    await load();
    onChanged();
  };

  const prune = async () => {
    if (!confirm('Supprimer tous les réseaux non utilisés ?')) return;
    setError(null);
    setSuccess(null);
    const res = await fetch('/api/docker/networks/prune', { method: 'POST' });
    if (!res.ok) {
      setError(await res.text());
      return;
    }
    const data = await res.json();
    setSuccess(
      data.deleted?.length
        ? `${data.deleted.length} réseau(x) supprimé(s) : ${data.deleted.join(', ')}`
        : 'Aucun réseau inutilisé à supprimer.'
    );
    await load();
    onChanged();
  };

  return (
    <div className="space-y-6">
      <div className={cardCls}>
        <div className="flex items-center gap-2 mb-4">
          <Network className="w-4 h-4 text-orange-500" />
          <h2 className="text-sm font-semibold">Nouveau réseau</h2>
        </div>
        <form onSubmit={create} className="grid grid-cols-1 md:grid-cols-4 gap-4">
          <div>
            <label className={labelCls}>Nom</label>
            <input
              className={inputCls}
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="ex: backend-net"
            />
          </div>
          <div>
            <label className={labelCls}>Driver</label>
            <select
              className={inputCls}
              value={form.driver}
              onChange={(e) => setForm({ ...form, driver: e.target.value })}
            >
              <option value="bridge">bridge</option>
              <option value="macvlan">macvlan</option>
              <option value="ipvlan">ipvlan</option>
              <option value="overlay">overlay</option>
            </select>
          </div>
          <div>
            <label className={labelCls}>Sous-réseau (optionnel)</label>
            <input
              className={inputCls}
              value={form.subnet}
              onChange={(e) => setForm({ ...form, subnet: e.target.value })}
              placeholder="172.30.0.0/24"
            />
          </div>
          <label className="flex items-end gap-2 pb-2 text-sm text-stone-300">
            <input
              type="checkbox"
              checked={form.internal}
              onChange={(e) => setForm({ ...form, internal: e.target.checked })}
              className="accent-orange-500"
            />
            Réseau interne
          </label>
          <div className="md:col-span-4">
            <button type="submit" disabled={loading || !form.name.trim()} className={primaryBtn}>
              <Plus className="w-4 h-4" /> {loading ? 'Création...' : 'Créer le réseau'}
            </button>
          </div>
        </form>
        {error && <ErrorNote message={error} onClose={() => setError(null)} />}
        {success && <SuccessNote message={success} />}
      </div>

      <div className={cardCls}>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-sm font-semibold">Réseaux ({networks.length})</h2>
          <div className="flex items-center gap-2">
            <button onClick={load} className={ghostBtn} type="button">
              <RefreshCw className="w-4 h-4" /> Actualiser
            </button>
            <button
              onClick={prune}
              className="inline-flex items-center gap-2 px-3 py-1.5 rounded-md bg-red-500/10 text-red-400 hover:bg-red-500/20 text-sm font-medium transition-colors"
            >
              <Trash2 className="w-4 h-4" /> Supprimer les inutilisés
            </button>
          </div>
        </div>
        <div className="overflow-x-auto rounded-lg border border-[#2E2A27]">
          <table className="w-full text-left text-sm">
            <thead className={tableHead}>
              <tr>
                <th className="p-3">Nom</th>
                <th className="p-3">Driver</th>
                <th className="p-3">Portée</th>
                <th className="p-3">Conteneurs</th>
                <th className="p-3">État</th>
                <th className="p-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[#2E2A27]/60">
              {networks.length === 0 && (
                <tr>
                  <td colSpan={6} className="p-4 text-center text-stone-500 text-xs">
                    Aucun réseau.
                  </td>
                </tr>
              )}
              {networks.map((n) => (
                <tr key={n.id} className="hover:bg-[#1C1A18]/70">
                  <td className="p-3 font-medium">
                    {n.name}
                    {n.internal && (
                      <span className="ml-2 px-1.5 py-0.5 rounded text-[10px] bg-[#2E2A27] text-stone-400">
                        interne
                      </span>
                    )}
                  </td>
                  <td className="p-3 font-mono text-xs text-stone-400">{n.driver}</td>
                  <td className="p-3 text-xs text-stone-400">{n.scope}</td>
                  <td className="p-3 text-xs text-stone-300">{n.containers}</td>
                  <td className="p-3">
                    <span
                      className={`px-2 py-0.5 rounded-full text-xs ${
                        n.in_use
                          ? 'bg-emerald-500/15 text-emerald-500'
                          : 'bg-[#2E2A27] text-stone-400'
                      }`}
                    >
                      {n.in_use ? 'utilisé' : 'inutilisé'}
                    </span>
                  </td>
                  <td className="p-3 text-right">
                    <button
                      onClick={() => remove(n)}
                      disabled={n.in_use}
                      title={n.in_use ? 'Réseau utilisé par un conteneur' : 'Supprimer'}
                      className={dangerBtn}
                    >
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
/* Images                                                             */
/* ------------------------------------------------------------------ */

interface ImageItem {
  id: string;
  short_id: string;
  tags: string[] | null;
  size: number;
  created: number;
  dangling: boolean;
  in_use: boolean;
  containers: number;
}

export function ImagesPanel({ onChanged }: { onChanged: () => void }) {
  const [images, setImages] = useState<ImageItem[]>([]);
  const [onlyUnused, setOnlyUnused] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    const res = await fetch('/api/docker/images');
    if (res.ok) setImages((await res.json()) ?? []);
    else setError(await res.text());
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const remove = async (img: ImageItem) => {
    const label = img.tags?.filter((t) => t !== '<none>:<none>').join(', ') || img.short_id;
    if (!confirm(`Supprimer l'image « ${label} » (${formatBytes(img.size)}) ?`)) return;
    setError(null);
    setSuccess(null);
    const res = await fetch(`/api/docker/images/${encodeURIComponent(img.id)}`, { method: 'DELETE' });
    if (!res.ok) {
      setError(await res.text());
      return;
    }
    setSuccess(`Image ${img.short_id} supprimée.`);
    await load();
    onChanged();
  };

  const prune = async () => {
    if (!confirm('Supprimer toutes les images non utilisées (y compris intermédiaires) ?')) return;
    setBusy(true);
    setError(null);
    setSuccess(null);
    const res = await fetch('/api/docker/images/prune', { method: 'POST' });
    if (!res.ok) {
      setError(await res.text());
      setBusy(false);
      return;
    }
    const data = await res.json();
    setSuccess(
      data.deleted?.length
        ? `${data.deleted.length} image(s) supprimée(s) — ${formatBytes(data.space_reclaimed || 0)} libérés.`
        : 'Aucune image inutilisée à supprimer.'
    );
    await load();
    onChanged();
    setBusy(false);
  };

  const unused = images.filter((i) => !i.in_use);
  const unusedSize = unused.reduce((sum, i) => sum + i.size, 0);
  const shown = onlyUnused ? unused : images;

  return (
    <div className={cardCls}>
      <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
        <div className="flex items-center gap-2">
          <Layers className="w-4 h-4 text-orange-500" />
          <h2 className="text-sm font-semibold">Images ({images.length})</h2>
          <span className="px-2 py-0.5 rounded-full text-[10px] bg-[#2E2A27] text-stone-300">
            {unused.length} inutilisée(s) · {formatBytes(unusedSize)}
          </span>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <label className="inline-flex items-center gap-2 text-xs text-stone-300 mr-1">
            <input
              type="checkbox"
              checked={onlyUnused}
              onChange={(e) => setOnlyUnused(e.target.checked)}
              className="accent-orange-500"
            />
            Inutilisées seulement
          </label>
          <button onClick={load} className={ghostBtn} type="button">
            <RefreshCw className="w-4 h-4" /> Actualiser
          </button>
          <button
            onClick={prune}
            disabled={busy || unused.length === 0}
            className="inline-flex items-center gap-2 px-3 py-1.5 rounded-md bg-red-500/10 text-red-400 hover:bg-red-500/20 text-sm font-medium disabled:opacity-40 transition-colors"
          >
            <Trash2 className="w-4 h-4" /> {busy ? 'Suppression...' : 'Supprimer les inutilisées'}
          </button>
        </div>
      </div>

      <div className="overflow-x-auto rounded-lg border border-[#2E2A27]">
        <table className="w-full text-left text-sm">
          <thead className={tableHead}>
            <tr>
              <th className="p-3">Image</th>
              <th className="p-3">ID</th>
              <th className="p-3">Taille</th>
              <th className="p-3">Créée</th>
              <th className="p-3">Utilisation</th>
              <th className="p-3 text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-[#2E2A27]/60">
            {shown.length === 0 && (
              <tr>
                <td colSpan={6} className="p-4 text-center text-stone-500 text-xs">
                  Aucune image.
                </td>
              </tr>
            )}
            {shown.map((img) => {
              const tags = (img.tags || []).filter((t) => t !== '<none>:<none>');
              return (
                <tr key={img.id} className="hover:bg-[#1C1A18]/70">
                  <td className="p-3 font-mono text-xs text-stone-200">
                    {tags.length ? tags.join(', ') : <span className="text-stone-500">&lt;none&gt;</span>}
                    {img.dangling && (
                      <span className="ml-2 px-1.5 py-0.5 rounded text-[10px] bg-orange-500/15 text-orange-400">
                        dangling
                      </span>
                    )}
                  </td>
                  <td className="p-3 font-mono text-xs text-stone-500">{img.short_id}</td>
                  <td className="p-3 text-xs text-stone-300">{formatBytes(img.size)}</td>
                  <td className="p-3 text-xs text-stone-400">
                    {img.created ? new Date(img.created * 1000).toLocaleDateString() : '—'}
                  </td>
                  <td className="p-3">
                    <span
                      className={`px-2 py-0.5 rounded-full text-xs ${
                        img.in_use
                          ? 'bg-emerald-500/15 text-emerald-500'
                          : 'bg-[#2E2A27] text-stone-400'
                      }`}
                    >
                      {img.in_use ? 'utilisée' : 'inutilisée'}
                    </span>
                  </td>
                  <td className="p-3 text-right">
                    <button
                      onClick={() => remove(img)}
                      disabled={img.in_use}
                      title={
                        img.in_use
                          ? 'Image utilisée par un conteneur — supprimez d’abord le conteneur'
                          : 'Supprimer'
                      }
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
      {error && <ErrorNote message={error} onClose={() => setError(null)} />}
      {success && <SuccessNote message={success} />}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Volumes (container scratch data)                                   */
/* ------------------------------------------------------------------ */

interface VolumeItem {
  name: string;
  driver: string;
  mountpoint: string;
  created_at: string;
  size: number;
  ref_count: number;
  in_use: boolean;
  scope: string;
}

export function VolumesPanel({ onChanged }: { onChanged: () => void }) {
  const [volumes, setVolumes] = useState<VolumeItem[]>([]);
  const [name, setName] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    const res = await fetch('/api/docker/volumes');
    if (res.ok) setVolumes((await res.json()) ?? []);
    else setError(await res.text());
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setSuccess(null);
    setBusy(true);
    try {
      const res = await fetch('/api/docker/volumes', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: name.trim() }),
      });
      if (!res.ok) {
        setError(await res.text());
        return;
      }
      setSuccess(`Volume « ${name} » créé.`);
      setName('');
      await load();
      onChanged();
    } finally {
      setBusy(false);
    }
  };

  const remove = async (v: VolumeItem) => {
    if (!confirm(`Supprimer le volume « ${v.name} » (${formatBytes(v.size)}) ?\nLes données seront perdues.`)) return;
    setError(null);
    setSuccess(null);
    const res = await fetch(`/api/docker/volumes/${encodeURIComponent(v.name)}?force=true`, {
      method: 'DELETE',
    });
    if (!res.ok) {
      setError(await res.text());
      return;
    }
    setSuccess(`Volume « ${v.name} » supprimé.`);
    await load();
    onChanged();
  };

  const prune = async () => {
    if (!confirm('Supprimer tous les volumes non utilisés ? Les données seront perdues.')) return;
    setBusy(true);
    setError(null);
    setSuccess(null);
    const res = await fetch('/api/docker/volumes/prune', { method: 'POST' });
    if (!res.ok) {
      setError(await res.text());
      setBusy(false);
      return;
    }
    const data = await res.json();
    setSuccess(
      data.deleted?.length
        ? `${data.deleted.length} volume(s) supprimé(s) — ${formatBytes(data.space_reclaimed || 0)} libérés.`
        : 'Aucun volume inutilisé à supprimer.'
    );
    await load();
    onChanged();
    setBusy(false);
  };

  const unused = volumes.filter((v) => !v.in_use);
  const unusedSize = unused.reduce((sum, v) => sum + v.size, 0);

  return (
    <div className="space-y-6">
      <div className={cardCls}>
        <div className="flex items-center gap-2 mb-4">
          <Database className="w-4 h-4 text-orange-500" />
          <h2 className="text-sm font-semibold">Nouveau volume</h2>
        </div>
        <form onSubmit={create} className="flex flex-wrap items-end gap-3">
          <div className="flex-1 min-w-[220px]">
            <label className={labelCls}>Nom</label>
            <input
              className={inputCls}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="ex: app-data"
            />
          </div>
          <button type="submit" disabled={busy || !name.trim()} className={primaryBtn}>
            <Plus className="w-4 h-4" /> Créer le volume
          </button>
        </form>
        {error && <ErrorNote message={error} onClose={() => setError(null)} />}
      </div>

      <div className={cardCls}>
        <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-semibold">Volumes ({volumes.length})</h2>
            <span className="px-2 py-0.5 rounded-full text-[10px] bg-[#2E2A27] text-stone-300">
              {unused.length} non utilisé(s) · {formatBytes(unusedSize)}
            </span>
          </div>
          <div className="flex items-center gap-2">
            <button onClick={load} className={ghostBtn} type="button">
              <RefreshCw className="w-4 h-4" /> Actualiser
            </button>
            <button
              onClick={prune}
              disabled={busy || unused.length === 0}
              className="inline-flex items-center gap-2 px-3 py-1.5 rounded-md bg-red-500/10 text-red-400 hover:bg-red-500/20 text-sm font-medium disabled:opacity-40 transition-colors"
            >
              <Trash2 className="w-4 h-4" /> Supprimer les non utilisés
            </button>
          </div>
        </div>

        <div className="overflow-x-auto rounded-lg border border-[#2E2A27]">
          <table className="w-full text-left text-sm">
            <thead className={tableHead}>
              <tr>
                <th className="p-3">Nom</th>
                <th className="p-3">Driver</th>
                <th className="p-3">Taille</th>
                <th className="p-3">Montages</th>
                <th className="p-3">État</th>
                <th className="p-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[#2E2A27]/60">
              {volumes.length === 0 && (
                <tr>
                  <td colSpan={6} className="p-4 text-center text-stone-500 text-xs">
                    Aucun volume.
                  </td>
                </tr>
              )}
              {volumes.map((v) => (
                <tr key={v.name} className="hover:bg-[#1C1A18]/70">
                  <td className="p-3 font-medium font-mono text-xs">{v.name}</td>
                  <td className="p-3 font-mono text-xs text-stone-400">{v.driver}</td>
                  <td className="p-3 text-xs text-stone-300">{formatBytes(v.size)}</td>
                  <td className="p-3 text-xs text-stone-300">{v.ref_count}</td>
                  <td className="p-3">
                    <span
                      className={`px-2 py-0.5 rounded-full text-xs ${
                        v.in_use
                          ? 'bg-emerald-500/15 text-emerald-500'
                          : 'bg-[#2E2A27] text-stone-400'
                      }`}
                    >
                      {v.in_use ? 'utilisé' : 'inutilisé'}
                    </span>
                  </td>
                  <td className="p-3 text-right">
                    <button
                      onClick={() => remove(v)}
                      disabled={v.in_use}
                      title={v.in_use ? 'Volume monté par un conteneur' : 'Supprimer'}
                      className={dangerBtn}
                    >
                      <Trash2 className="w-3.5 h-3.5" /> Supprimer
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {success && <SuccessNote message={success} />}
      </div>
    </div>
  );
}
