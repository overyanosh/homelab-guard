import { useEffect, useMemo, useState } from 'react';
import {
  Shield,
  Box,
  Server,
  Terminal as TermIcon,
  Bot,
  RefreshCw,
  Play,
  Square,
  Camera,
  KeyRound,
  Eye,
  PackageCheck,
  Settings,
  ArrowLeft,
  FileCode,
  FileText,
  Plus,
  Rocket,
  Save,
  Network,
  Layers,
  Database,
  Loader2,
  Plug,
  AlertCircle,
  Trash2,
  Pause,
  ArrowUp,
  ArrowDown,
  ArrowUpDown,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { SshTerminal } from './components/SshTerminal';
import { DockerSettings, ProxmoxSettings, SshSettings, AiSettings } from './components/SettingsPanels';
import {
  DiskUsageBar,
  NetworksPanel,
  ImagesPanel,
  VolumesPanel,
} from './components/DockerResources';
import {
  ComposeValidationPanel,
  EnvGapBadge,
  addEnvKeys,
  type ValidationResult,
} from './components/ComposeValidation';
import { StacksPanel, StackEditor } from './components/Stacks';
import { CodeEditor } from './components/CodeEditor';
import { LoginPage } from './components/LoginPage';

type Tab = 'docker' | 'proxmox' | 'ssh' | 'ai';
type DockerTab = 'containers' | 'stacks' | 'networks' | 'images' | 'volumes';
type ContainerSortKey =
  | 'name'
  | 'image'
  | 'state'
  | 'runtime'
  | 'networks'
  | 'ports'
  | 'guard'
  | 'update';

interface ContainerItem {
  id: string;
  names: string[];
  image: string;
  state: string;
  status: string;
  guard: {
    enabled: boolean;
    group: string;
    update_mode: string;
    test_strategy: string;
  };
  runtime: string;
  networks?: string[];
  ports?: string[];
  newer_version_available?: boolean;
}

// CompactCell shows a single value directly, or an icon with a hover tooltip
// listing every value when there are several.
function CompactCell({
  items,
  icon: Icon,
  mono = true,
}: {
  items?: string[];
  icon: LucideIcon;
  mono?: boolean;
}) {
  const list = items ?? [];
  if (list.length === 0) return <span className="text-xs text-stone-600">—</span>;
  if (list.length === 1) {
    return (
      <span className={`text-xs text-stone-300 ${mono ? 'font-mono' : ''}`} title={list[0]}>
        {list[0]}
      </span>
    );
  }
  return (
    <span
      title={list.join('\n')}
      className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-[#2E2A27] text-stone-300 hover:bg-[#3a3531] cursor-help transition-colors"
    >
      <Icon className="w-3 h-3 text-orange-400" />
      <span className="text-[11px] leading-none">…</span>
    </span>
  );
}

interface PveResource {
  id: string;
  type: string;
  node: string;
  vmid?: number;
  name?: string;
  status?: string;
}

interface SshHost {
  id: number;
  name: string;
  host: string;
  port: number;
  username: string;
}

const NAV: { id: Tab; label: string; icon: LucideIcon }[] = [
  { id: 'docker', label: 'Podman, Docker & Swarm', icon: Box },
  { id: 'proxmox', label: 'Proxmox VE', icon: Server },
  { id: 'ssh', label: 'Bastion SSH', icon: TermIcon },
  { id: 'ai', label: 'Agent IA Ops', icon: Bot },
];

const SETTINGS_LABEL: Record<Tab, string> = {
  docker: 'Podman, Docker & Swarm',
  proxmox: 'Proxmox VE',
  ssh: 'Bastion SSH',
  ai: 'Agent IA & Ops',
};

export default function App() {
  const [tab, setTab] = useState<Tab>('docker');
  const [settingsTab, setSettingsTab] = useState<Tab | null>(null);
  const [dockerTab, setDockerTab] = useState<DockerTab>('containers');
  const [sort, setSort] = useState<{ key: ContainerSortKey; dir: 'asc' | 'desc' }>(() => {
    try {
      const raw = localStorage.getItem('hg.containers.sort');
      if (raw) {
        const parsed = JSON.parse(raw);
        if (parsed?.key && (parsed.dir === 'asc' || parsed.dir === 'desc')) return parsed;
      }
    } catch {
      /* ignore */
    }
    return { key: 'name', dir: 'asc' };
  });
  const [resourceRefresh, setResourceRefresh] = useState(0);
  const [stackEditorOpen, setStackEditorOpen] = useState(false);
  const [nodes, setNodes] = useState<{ id: number; name: string; online: boolean }[]>([]);
  const [selectedNode, setSelectedNode] = useState<string>('local');
  const [remoteContainers, setRemoteContainers] = useState<ContainerItem[]>([]);
  const [remoteError, setRemoteError] = useState<string | null>(null);
  const [serverVersion, setServerVersion] = useState<{ version: string; asset: string } | null>(null);
  const [staleBundle, setStaleBundle] = useState(false);
  const [auth, setAuth] = useState<{
    enabled: boolean;
    authenticated: boolean;
    email?: string;
    name?: string;
  } | null>(null);
  const [stackEditing, setStackEditing] = useState<string | null>(null);
  const [containers, setContainers] = useState<ContainerItem[]>([]);
  const [pveResources, setPveResources] = useState<PveResource[]>([]);
  const [sshHosts, setSshHosts] = useState<SshHost[]>([]);
  const [activeSsh, setActiveSsh] = useState<SshHost | null>(null);
  const [aiPrompt, setAiPrompt] = useState('');
  const [aiYaml, setAiYaml] = useState('');
  const [loading, setLoading] = useState(false);
  const [statusBanner, setStatusBanner] = useState<string | null>(null);

  // New state for logs and updates
  const [logsModalOpen, setLogsModalOpen] = useState(false);
  const [containerLogs, setContainerLogs] = useState('');
  const [checkingUpdates, setCheckingUpdates] = useState<Record<string, boolean>>({});
  const [newerVersions, setNewerVersions] = useState<Set<string>>(new Set());

  // Compose viewer/editor state
  const [composeOpen, setComposeOpen] = useState(false);
  const [composeLoading, setComposeLoading] = useState(false);
  const [composeSaving, setComposeSaving] = useState(false);
  const [composeContainerId, setComposeContainerId] = useState('');
  const [composeService, setComposeService] = useState('');
  const [composeYaml, setComposeYaml] = useState('');
  const [composeEnv, setComposeEnv] = useState('');
  const [composeEnvExists, setComposeEnvExists] = useState(false);
  const [composeTab, setComposeTab] = useState<'compose' | 'env'>('compose');
  const [composeSource, setComposeSource] = useState<'stored' | 'generated'>('generated');
  const [composeMsg, setComposeMsg] = useState<string | null>(null);
  const [applyService, setApplyService] = useState('');
  const [composeValidation, setComposeValidation] = useState<ValidationResult | null>(null);
  const [composeValidating, setComposeValidating] = useState(false);
  const [composeApplying, setComposeApplying] = useState(false);
  const [composeApplyResult, setComposeApplyResult] = useState<{
    status: string;
    message?: string;
    logs?: string;
    ai_diagnosis?: string;
  } | null>(null);

  // New container (compose without AI) state
  const [createOpen, setCreateOpen] = useState(false);
  const [createName, setCreateName] = useState('');
  const [createYaml, setCreateYaml] = useState('');
  const [createEnv, setCreateEnv] = useState('');
  const [createEnvExists, setCreateEnvExists] = useState(false);
  const [createTab, setCreateTab] = useState<'compose' | 'env'>('compose');
  const [createTouched, setCreateTouched] = useState(false);
  const [creating, setCreating] = useState(false);
  const [createMsg, setCreateMsg] = useState<string | null>(null);
  const [createResult, setCreateResult] = useState<{
    status: string;
    container_name?: string;
    message?: string;
    logs?: string;
    ai_diagnosis?: string;
  } | null>(null);

  // Live compose validation
  const [validation, setValidation] = useState<ValidationResult | null>(null);
  const [validating, setValidating] = useState(false);

  useEffect(() => {
    try {
      localStorage.setItem('hg.containers.sort', JSON.stringify(sort));
    } catch {
      /* ignore */
    }
  }, [sort]);

  const isRemote = selectedNode !== 'local';
  const nodeBase = isRemote ? `/api/nodes/${selectedNode}` : '/api/docker';

  const toggleSort = (key: ContainerSortKey) =>
    setSort((prev) => (prev.key === key ? { key, dir: prev.dir === 'asc' ? 'desc' : 'asc' } : { key, dir: 'asc' }));

  const sortedContainers = useMemo(() => {
    const factor = sort.dir === 'asc' ? 1 : -1;
    const valueOf = (c: ContainerItem): string | number => {
      switch (sort.key) {
        case 'name':
          return (c.names[0] || '').replace(/^\//, '').toLowerCase();
        case 'image':
          return c.image.toLowerCase();
        case 'state':
          return c.state.toLowerCase();
        case 'runtime':
          return (c.runtime || '').toLowerCase();
        case 'networks':
          return (c.networks ?? []).join(',').toLowerCase();
        case 'ports':
          return (c.ports ?? []).join(',').toLowerCase();
        case 'guard':
          return c.guard.enabled ? `${c.guard.update_mode}/${c.guard.test_strategy}` : '';
        case 'update':
          return c.newer_version_available || newerVersions.has(c.id) ? 1 : 0;
        default:
          return '';
      }
    };
    const source = selectedNode === 'local' ? containers : remoteContainers;
    return [...source].sort((a, b) => {
      const va = valueOf(a);
      const vb = valueOf(b);
      if (va < vb) return -1 * factor;
      if (va > vb) return factor;
      return 0;
    });
  }, [containers, remoteContainers, selectedNode, sort, newerVersions]);

  const SortHeader = ({
    label,
    sortKey,
    className = '',
  }: {
    label: string;
    sortKey: ContainerSortKey;
    className?: string;
  }) => {
    const active = sort.key === sortKey;
    const Icon = !active ? ArrowUpDown : sort.dir === 'asc' ? ArrowUp : ArrowDown;
    return (
      <th className={`p-3 ${className}`}>
        <button
          type="button"
          onClick={() => toggleSort(sortKey)}
          title={`Trier par ${label}`}
          className={`inline-flex items-center gap-1 transition-colors ${
            active ? 'text-orange-400' : 'hover:text-stone-200'
          }`}
        >
          {label}
          <Icon className={`w-3 h-3 ${active ? '' : 'opacity-40'}`} />
        </button>
      </th>
    );
  };

  const loadNodes = async () => {
    try {
      const res = await fetch('/api/nodes', { cache: 'no-store' });
      if (res.ok) setNodes((await res.json()) ?? []);
    } catch {
      /* ignore */
    }
  };

  const loadRemoteContainers = async (nodeID: string) => {
    setRemoteError(null);
    setLoading(true);
    try {
      const res = await fetch(`/api/nodes/${nodeID}/containers`, { cache: 'no-store' });
      if (res.ok) {
        const data = await res.json();
        if (Array.isArray(data)) {
          setRemoteContainers(data);
        } else {
          setRemoteContainers([]);
          setRemoteError(data?.error || 'réponse inattendue de l\'agent');
        }
      } else {
        setRemoteContainers([]);
        setRemoteError(await res.text());
      }
    } catch (err) {
      setRemoteError(String(err));
    } finally {
      setLoading(false);
    }
  };

  const loadDocker = async () => {
    const res = await fetch('/api/docker/containers', { cache: 'no-store' });
    if (res.ok) {
      const data = await res.json();
      setContainers(data);

      // Check for newer versions after containers are loaded
      await checkForNewerVersions(data);
    }
  };

  const checkForNewerVersions = async (containersToCheck: ContainerItem[] = containers) => {
    const checks: Promise<{ id: string; hasUpdate: boolean }>[] = [];

    containersToCheck.forEach((c) => {
      if (c.newer_version_available === undefined && !checkingUpdates[c.id]) {
        setCheckingUpdates((prev) => ({ ...prev, [c.id]: true }));
        checks.push(
          fetch('/api/docker/images/check-update', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ image_name: c.image }),
          })
            .then((res) => res.json())
            .then((data: { has_update: boolean }) => ({ id: c.id, hasUpdate: data.has_update }))
            .catch(() => ({ id: c.id, hasUpdate: false }))
            .finally(() => setCheckingUpdates((prev) => ({ ...prev, [c.id]: false })))
        );
      }
    });

    if (checks.length > 0) {
      const results = await Promise.all(checks);
      const updatedSet = new Set(newerVersions);
      results.forEach((r) => {
        if (r.hasUpdate) {
          updatedSet.add(r.id);
        } else {
          updatedSet.delete(r.id);
        }
      });
      setNewerVersions(updatedSet);
    }
  };

  const openLogs = async (id: string) => {
    setLogsModalOpen(true);
    setLoading(true);
    try {
      const res = isRemote
        ? await fetch(`/api/nodes/${selectedNode}/containers/${id}/logs?tail=200`, { cache: 'no-store' })
        : await fetch(`/api/docker/containers/${id}/logs`);
      if (res.ok) {
        const text = await res.text();
        setContainerLogs(text);
      } else {
        setContainerLogs(await res.text());
      }
    } catch (err) {
      console.error('Failed to load logs:', err);
    }
    setLoading(false);
  };

  const errorLinesOf = (v: ValidationResult | null): number[] =>
    (v?.errors ?? []).map((e) => e.line ?? 0).filter((n) => n > 0);

  const composeTemplate = (name: string) => `services:
  ${name || 'mon-service'}:
    image: docker.io/library/nginx:alpine
    restart: unless-stopped
    ports:
      - "8080:80"
`;

  const openCreate = () => {
    setCreateOpen(true);
    setCreateName('');
    setCreateYaml(composeTemplate(''));
    setCreateEnv('');
    setCreateEnvExists(false);
    setCreateTab('compose');
    setCreateTouched(false);
    setCreateMsg(null);
    setCreateResult(null);
    setValidation(null);
  };

  const existingNames = new Set(
    containers.map((c) => (c.names[0] || '').replace(/^\//, '')).filter(Boolean)
  );
  const createNameTaken = createName.trim() !== '' && existingNames.has(createName.trim());
  const suggestName = (base: string) => {
    const clean = base.trim() || 'service';
    if (!existingNames.has(clean)) return clean;
    for (let i = 2; i < 100; i++) {
      const candidate = `${clean}-${i}`;
      if (!existingNames.has(candidate)) return candidate;
    }
    return `${clean}-new`;
  };

  const changeCreateName = (value: string) => {
    setCreateName(value);
    if (!createTouched) {
      setCreateYaml(composeTemplate(value.trim()));
    }
  };

  const createContainer = async () => {
    if (!createName.trim()) return;
    setCreating(true);
    setCreateMsg(null);
    setCreateResult(null);
    try {
      const res = await fetch(`${nodeBase}/containers/compose`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: createName.trim(),
          yaml: createYaml,
          env: createEnvExists ? createEnv : '',
        }),
      });
      if (res.ok) {
        const data = await res.json();
        setCreateResult(data);
        loadDocker();
      } else {
        const text = await res.text();
        try {
          const parsed = JSON.parse(text);
          if (parsed && Array.isArray(parsed.errors)) {
            setValidation(parsed);
            setCreateMsg('Compose invalide — corrigez les erreurs signalées.');
          } else {
            setCreateMsg(parsed.message || text);
          }
        } catch {
          setCreateMsg(text);
        }
      }
    } catch (err) {
      setCreateMsg(String(err));
    }
    setCreating(false);
  };

  // Debounced server-side validation of the compose being edited.
  useEffect(() => {
    if (!createOpen) return;
    const timer = setTimeout(async () => {
      setValidating(true);
      try {
        const res = await fetch('/api/docker/compose/validate', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ yaml: createYaml, env: createEnvExists ? createEnv : '' }),
        });
        if (res.ok) setValidation(await res.json());
      } catch {
        /* ignore transient errors while typing */
      } finally {
        setValidating(false);
      }
    }, 350);
    return () => clearTimeout(timer);
  }, [createOpen, createYaml, createEnv, createEnvExists]);

  useEffect(() => {
    const services = composeValidation?.services ?? [];
    if (services.length === 0) return;
    setApplyService((prev) => {
      if (prev && services.includes(prev)) return prev;
      const byName = services.find((s) => s === composeService);
      if (byName) return byName;
      return services.length === 1 ? services[0] : '';
    });
  }, [composeValidation, composeService]);

  // Debounced validation for the existing container's compose editor.
  useEffect(() => {
    if (!composeOpen || composeLoading) return;
    const timer = setTimeout(async () => {
      setComposeValidating(true);
      try {
        const res = await fetch('/api/docker/compose/validate', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            yaml: composeYaml,
            env: composeEnvExists ? composeEnv : '',
          }),
        });
        if (res.ok) setComposeValidation(await res.json());
      } catch {
        /* ignore transient errors while typing */
      } finally {
        setComposeValidating(false);
      }
    }, 350);
    return () => clearTimeout(timer);
  }, [composeOpen, composeLoading, composeYaml, composeEnv, composeEnvExists]);

  // Auto-add keys referenced by the compose but absent from .env.
  useEffect(() => {
    if (!composeOpen) return;
    const missing = composeValidation?.missing_env ?? [];
    if (missing.length === 0) return;
    setComposeEnvExists(true);
    setComposeEnv((prev) => addEnvKeys(prev, missing));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [composeOpen, composeValidation]);

  // Automatically add referenced-but-missing keys to the .env with an empty
  // value, so the user only has to fill in the value.
  useEffect(() => {
    if (!createOpen) return;
    const missing = validation?.missing_env ?? [];
    if (missing.length === 0) return;
    setCreateEnvExists(true);
    setCreateEnv((prev) => addEnvKeys(prev, missing));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [createOpen, validation]);

  const openCompose = async (id: string) => {
    setComposeOpen(true);
    setComposeLoading(true);
    setComposeMsg(null);
    setComposeValidation(null);
    setComposeContainerId(id);
    setComposeYaml('');
    setComposeEnv('');
    setComposeEnvExists(false);
    setComposeTab('compose');
    setComposeApplyResult(null);
    try {
      const res = await fetch(`${nodeBase}/containers/${id}/compose`, { cache: 'no-store' });
      if (res.ok) {
        const data = await res.json();
        setComposeService(data.service);
        setComposeYaml(data.yaml);
        setComposeEnv(data.env || '');
        setComposeEnvExists(Boolean(data.env_exists));
        setComposeSource(data.source === 'stored' ? 'stored' : 'generated');
      } else {
        setComposeMsg(await res.text());
      }
    } catch (err) {
      setComposeMsg(String(err));
    }
    setComposeLoading(false);
  };

  const saveCompose = async () => {
    if (!composeContainerId) return;
    setComposeSaving(true);
    setComposeMsg(null);
    try {
      const res = await fetch(`${nodeBase}/containers/${composeContainerId}/compose`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          service: applyService,
          yaml: composeYaml,
          env: composeEnvExists ? composeEnv : '',
        }),
      });
      if (res.ok) {
        const data = await res.json();
        setComposeSource('stored');
        setComposeEnvExists(Boolean(data.env_exists));
        if (!data.env_exists) setComposeTab('compose');
        setComposeMsg('Enregistré.');
      } else {
        setComposeMsg(await res.text());
      }
    } catch (err) {
      setComposeMsg(String(err));
    }
    setComposeSaving(false);
  };

  const applyCompose = async () => {
    if (!composeContainerId) return;
    if (
      !confirm(
        `Recréer le conteneur « ${composeService} » à partir de ce compose ?\n\n` +
          `Le conteneur actuel sera conservé en sauvegarde et restauré automatiquement ` +
          `si le nouveau ne démarre pas ou devient unhealthy.`
      )
    ) {
      return;
    }
    setComposeApplying(true);
    setComposeMsg(null);
    setComposeApplyResult(null);
    try {
      const res = await fetch(`${nodeBase}/containers/${composeContainerId}/compose/apply`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          service: applyService,
          yaml: composeYaml,
          env: composeEnvExists ? composeEnv : '',
        }),
      });
      if (res.ok) {
        const data = await res.json();
        setComposeApplyResult(data);
        setComposeSource('stored');
        if (data.status === 'applied') setComposeMsg('Appliqué avec succès.');
        loadDocker();
      } else {
        const text = await res.text();
        try {
          const parsed = JSON.parse(text);
          if (parsed && Array.isArray(parsed.errors)) {
            setComposeValidation(parsed);
            setComposeMsg('Compose invalide — corrigez les erreurs signalées.');
          } else {
            setComposeMsg(parsed.message || text);
          }
        } catch {
          setComposeMsg(text);
        }
      }
    } catch (err) {
      setComposeMsg(String(err));
    }
    setComposeApplying(false);
  };

  const loadProxmox = async () => {
    const clustersRes = await fetch('/api/proxmox/clusters');
    if (!clustersRes.ok) return;
    const clusters = await clustersRes.json();
    if (Array.isArray(clusters) && clusters.length > 0) {
      const res = await fetch(`/api/proxmox/clusters/${clusters[0].id}/resources`);
      if (res.ok) setPveResources((await res.json()) ?? []);
    }
  };

  const loadSshHosts = async () => {
    const res = await fetch('/api/ssh/hosts');
    if (res.ok) setSshHosts((await res.json()) ?? []);
  };

  useEffect(() => {
    loadDocker();
    loadProxmox();
    loadSshHosts();
    loadNodes();
    const timer = setInterval(loadNodes, 60000);
    return () => clearInterval(timer);
  }, []);

  useEffect(() => {
    if (selectedNode === 'local') {
      loadDocker();
    } else {
      // Remote agents only expose containers; the other tabs are local-only.
      setDockerTab('containers');
      loadRemoteContainers(selectedNode);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedNode]);

  // Optional OIDC gate: /auth/me tells whether a login page is required.
  useEffect(() => {
    let cancelled = false;
    const check = async () => {
      try {
        const res = await fetch('/auth/me', { cache: 'no-store' });
        if (!res.ok) return;
        const data = await res.json();
        if (!cancelled) setAuth(data);
      } catch {
        /* keep previous state */
      }
    };
    check();
    const timer = setInterval(check, 60000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, []);

  const logout = async () => {
    await fetch('/auth/logout', { method: 'POST' });
    window.location.replace('/');
  };

  // Detect an outdated frontend bundle (e.g. after a redeploy): the JS file name
  // referenced by the server's index.html differs from the one we are running.
  useEffect(() => {
    const currentScript = Array.from(document.querySelectorAll('script[src]'))
      .map((el) => (el as HTMLScriptElement).src)
      .map((src) => src.split('/').pop() || '')
      .find((name) => name.startsWith('index-')) || '';
    let cancelled = false;
    const check = async () => {
      try {
        const res = await fetch('/api/version', { cache: 'no-store' });
        if (!res.ok) return;
        const data = await res.json();
        if (cancelled) return;
        setServerVersion({ version: data.version, asset: data.asset });
        setStaleBundle(Boolean(currentScript && data.asset && data.asset !== currentScript));
      } catch {
        /* ignore */
      }
    };
    check();
    const timer = setInterval(check, 60000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, []);

  const containerAction = async (
    id: string,
    action: 'start' | 'stop' | 'pause' | 'unpause'
  ) => {
    const res = isRemote
      ? await fetch(`/api/nodes/${selectedNode}/containers/${id}/action`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ action }),
        })
      : await fetch(`/api/docker/containers/${id}/${action}`, { method: 'POST' });
    if (!res.ok) {
      alert(await res.text());
      return;
    }
    loadDocker();
  };

  const deleteContainer = async (id: string, name: string) => {
    if (!confirm(`Supprimer le conteneur « ${name} » ? Cette action est irréversible.`)) return;
    const res = isRemote
      ? await fetch(`/api/nodes/${selectedNode}/containers/${id}?force=true`, { method: 'DELETE' })
      : await fetch(`/api/docker/containers/${id}?force=true`, { method: 'DELETE' });
    if (!res.ok) {
      alert(await res.text());
      return;
    }
    loadDocker();
  };

  const triggerUpgrade = async (id: string) => {
    setLoading(true);
    setStatusBanner('Exécution du pipeline Sandbox & Non-Régression...');
    const url =
      selectedNode === 'local'
        ? `/api/docker/containers/${id}/upgrade?force=true`
        : `/api/nodes/${selectedNode}/containers/${id}/upgrade?force=true`;
    const res = await fetch(url, { method: 'POST' });
    const data = await res.json();
    setStatusBanner(
      data.status === 'rolled-back'
        ? `Rollback effectué sur ${data.container_name} — Diagnostic IA : ${data.ai_diagnosis}`
        : `Résultat (${data.container_name}) : ${data.status}`
    );
    setLoading(false);
    if (selectedNode === 'local') loadDocker();
    else loadRemoteContainers(selectedNode);
  };

  const generateCompose = async () => {
    setLoading(true);
    const res = await fetch('/api/ai/generate-compose', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ prompt: aiPrompt, group: 'homelab', auto_update: true }),
    });
    if (res.ok) {
      const data = await res.json();
      setAiYaml(data.compose_yaml);
    }
    setLoading(false);
  };

  const openSettings = (id: Tab) => setSettingsTab((prev) => (prev === id ? null : id));

  if (auth === null) {
    return (
      <div className="min-h-screen bg-[#121110] flex items-center justify-center text-sm text-stone-500">
        Chargement…
      </div>
    );
  }

  if (auth.enabled && !auth.authenticated) {
    return <LoginPage />;
  }

  return (
    <div className="min-h-screen bg-[#121110] text-stone-100 flex">
      <aside className="w-64 border-r border-[#2E2A27] bg-[#1C1A18]/70 p-4 flex flex-col gap-1.5">
        <div className="flex items-center gap-2.5 px-2 py-3 mb-4 border-b border-[#2E2A27]">
          <Shield className="w-6 h-6 text-orange-500" />
          <span className="font-bold tracking-tight text-lg">
            HOMELAB<span className="text-orange-500"> GUARD</span>
          </span>
        </div>

        {NAV.map(({ id, label, icon: Icon }) => {
          const activeMain = tab === id && settingsTab === null;
          const activeSettings = settingsTab === id;
          return (
            <div
              key={id}
              className={`flex items-center rounded-lg transition-colors ${
                activeMain ? 'bg-orange-500/15' : ''
              } ${activeSettings ? 'bg-[#2E2A27]/70' : ''}`}
            >
              <button
                onClick={() => {
                  setTab(id);
                  setSettingsTab(null);
                }}
                className={`flex-1 flex items-center gap-3 px-3 py-2.5 rounded-l-lg text-sm font-medium ${
                  activeMain ? 'text-orange-500' : 'text-stone-400 hover:bg-[#2E2A27]'
                }`}
              >
                <Icon className="w-4 h-4" /> {label}
              </button>
              <button
                onClick={() => openSettings(id)}
                title={`Réglages — ${label}`}
                aria-label={`Réglages ${label}`}
                className={`mr-1 p-2 rounded-md transition-colors ${
                  activeSettings
                    ? 'text-orange-500 bg-orange-500/15'
                    : 'text-stone-500 hover:text-orange-500 hover:bg-[#2E2A27]'
                }`}
              >
                <Settings className="w-4 h-4" />
              </button>
            </div>
          );
        })}

        <div className="mt-auto pt-3 border-t border-[#2E2A27] text-[10px] text-stone-500 leading-relaxed">
          {auth.enabled && auth.authenticated && (
            <div className="mb-2">
              <div className="truncate text-stone-400" title={auth.email}>
                {auth.email || auth.name || 'connecté'}
              </div>
              <button
                onClick={logout}
                className="mt-1 text-[10px] text-orange-400 hover:text-orange-300 transition-colors"
              >
                Se déconnecter
              </button>
            </div>
          )}
          <div>Homelab Guard {serverVersion?.version ? `v${serverVersion.version}` : ''}</div>
          <div className="truncate font-mono text-stone-600" title={serverVersion?.asset}>
            {serverVersion?.asset || 'bundle inconnu'}
          </div>
        </div>
      </aside>

      <main className="flex-1 p-6 overflow-y-auto">
        {staleBundle && (
          <div className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-orange-500/40 bg-orange-950/30 p-3 text-sm text-orange-200">
            <span>
              Une nouvelle version de l'interface est disponible (serveur
              {serverVersion?.version ? ` v${serverVersion.version}` : ''}). Votre navigateur affiche
              encore l'ancien bundle.
            </span>
            <button
              onClick={() => {
                const url = new URL(window.location.href);
                url.searchParams.set('_v', Date.now().toString());
                window.location.replace(url.toString());
              }}
              className="px-3 py-1.5 rounded-md bg-orange-600 hover:bg-orange-500 text-white text-xs font-medium transition-colors"
            >
              Recharger maintenant
            </button>
          </div>
        )}
        {statusBanner && (
          <div className="mb-6 rounded-lg border border-orange-500/30 bg-orange-950/40 p-4 text-sm text-orange-200 flex justify-between items-start">
            <pre className="whitespace-pre-wrap font-sans">{statusBanner}</pre>
            <button
              onClick={() => setStatusBanner(null)}
              className="text-xs text-stone-400 hover:text-white ml-4"
            >
              Fermer
            </button>
          </div>
        )}

        {settingsTab !== null ? (
          <div>
            <div className="flex items-center gap-3 mb-6">
              <button
                onClick={() => setSettingsTab(null)}
                className="inline-flex items-center gap-2 px-3 py-1.5 rounded-md bg-[#2E2A27] hover:bg-[#3a3531] text-sm transition-colors"
              >
                <ArrowLeft className="w-4 h-4" /> Retour
              </button>
              <h1 className="text-xl font-semibold">
                Réglages — <span className="text-orange-500">{SETTINGS_LABEL[settingsTab]}</span>
              </h1>
            </div>
            {settingsTab === 'docker' && <DockerSettings />}
            {settingsTab === 'proxmox' && <ProxmoxSettings />}
            {settingsTab === 'ssh' && <SshSettings />}
            {settingsTab === 'ai' && <AiSettings />}
          </div>
        ) : (
          <>
            {tab === 'docker' && (
              <section>
                <div className="flex justify-between items-center mb-4">
                  <h1 className="text-xl font-semibold">Podman, Docker & Swarm</h1>
                  <div className="flex items-center gap-2">
                    <select
                      value={selectedNode}
                      onChange={(e) => setSelectedNode(e.target.value)}
                      title="Nœud dont on affiche les conteneurs"
                      className="rounded-md border border-[#2E2A27] bg-[#121110] px-2 py-1.5 text-sm text-stone-200 focus:border-orange-500 focus:outline-none"
                    >
                      <option value="local">Local Host</option>
                      {nodes
                        .filter((n) => !n.name.toLowerCase().includes('local'))
                        .map((n) => (
                          <option key={n.id} value={String(n.id)}>
                            {n.name} {n.online ? '●' : '(hors ligne)'}
                          </option>
                        ))}
                    </select>
                    <button
                      onClick={() => {
                        setStackEditing(null);
                        setStackEditorOpen(true);
                      }}
                      title="Déployer un compose multi-services (stack)"
                      className="flex items-center gap-2 px-3 py-1.5 rounded-md border border-orange-500/60 text-orange-400 hover:bg-orange-500/15 text-sm font-medium transition-colors"
                    >
                      <Layers className="w-4 h-4" /> Stacks
                    </button>
                    <button
                      onClick={openCreate}
                      title="Créer un conteneur simple à partir d'un compose (sans IA)"
                      className="flex items-center gap-2 px-3 py-1.5 rounded-md bg-orange-600 hover:bg-orange-500 text-white text-sm font-medium transition-colors"
                    >
                      <Plus className="w-4 h-4" /> Nouveau
                    </button>
                    <button
                      onClick={() => {
                        loadDocker();
                        setResourceRefresh((v) => v + 1);
                      }}
                      className="flex items-center gap-2 px-3 py-1.5 rounded-md bg-[#2E2A27] hover:bg-orange-600 text-sm transition-colors"
                    >
                      <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} /> Actualiser
                    </button>
                    <button
                      onClick={() => openSettings('docker')}
                      title="Réglages Podman, Docker & Swarm"
                      className="p-2 rounded-md bg-[#2E2A27] hover:bg-orange-600 text-stone-300 hover:text-white transition-colors"
                    >
                      <Settings className="w-4 h-4" />
                    </button>
                  </div>
                </div>

                <nav className="flex flex-wrap items-center gap-1 mb-6 border-b border-[#2E2A27]">
                  {(
                    [
                      { id: 'containers', label: 'Conteneurs', icon: Box },
                      { id: 'stacks', label: 'Stacks', icon: Layers },
                      { id: 'networks', label: 'Réseaux', icon: Network },
                      { id: 'images', label: 'Images', icon: Layers },
                      { id: 'volumes', label: 'Volumes', icon: Database },
                    ] as { id: DockerTab; label: string; icon: LucideIcon }[]
                  ).map(({ id, label, icon: Icon }) => (
                    <button
                      key={id}
                      onClick={() => setDockerTab(id)}
                      className={`inline-flex items-center gap-2 px-3.5 py-2.5 text-sm font-medium border-b-2 -mb-px transition-colors ${
                        dockerTab === id
                          ? 'border-orange-500 text-orange-500'
                          : 'border-transparent text-stone-400 hover:text-stone-200'
                      }`}
                    >
                      <Icon className="w-4 h-4" /> {label}
                    </button>
                  ))}
                </nav>

                {(dockerTab === 'images' || dockerTab === 'volumes') && (
                  <DiskUsageBar refreshKey={resourceRefresh} base={nodeBase} />
                )}

                {dockerTab === 'stacks' && (
                  <StacksPanel
                    base={nodeBase}
                    refreshKey={resourceRefresh}
                    onEdit={(stackName) => {
                      setStackEditing(stackName);
                      setStackEditorOpen(true);
                    }}
                  />
                )}
                {dockerTab === 'networks' && (
                  <NetworksPanel base={nodeBase} onChanged={() => setResourceRefresh((v) => v + 1)} />
                )}
                {dockerTab === 'images' && (
                  <ImagesPanel base={nodeBase} onChanged={() => setResourceRefresh((v) => v + 1)} />
                )}
                {dockerTab === 'volumes' && (
                  <VolumesPanel base={nodeBase} onChanged={() => setResourceRefresh((v) => v + 1)} />
                )}

                {dockerTab === 'containers' && selectedNode !== 'local' && (
                  <div className="mb-4 flex flex-wrap items-center gap-2 rounded-lg border border-[#2E2A27] bg-[#1C1A18]/60 p-3 text-xs text-stone-400">
                    <span className="font-medium text-stone-200">
                      Agent distant : {nodes.find((n) => String(n.id) === selectedNode)?.name || selectedNode}
                    </span>
                    <span
                      className={`px-2 py-0.5 rounded-full ${
                        nodes.find((n) => String(n.id) === selectedNode)?.online
                          ? 'bg-emerald-500/15 text-emerald-500'
                          : 'bg-red-500/15 text-red-400'
                      }`}
                    >
                      {nodes.find((n) => String(n.id) === selectedNode)?.online ? 'en ligne' : 'hors ligne'}
                    </span>
                    <span className="text-stone-500">
                      Toutes les actions sont exécutées par l'agent : cycle de vie, logs, compose, Test &amp; Update, suppression.
                    </span>
                  </div>
                )}

                {dockerTab === 'containers' && remoteError && (
                  <div className="mb-4 flex items-start gap-2 rounded-lg border border-red-500/30 bg-red-950/25 p-3 text-xs text-red-300">
                    <AlertCircle className="w-4 h-4 mt-0.5 shrink-0" />
                    <pre className="whitespace-pre-wrap font-sans">{remoteError}</pre>
                  </div>
                )}

                {dockerTab === 'containers' && (
                <div className="overflow-x-auto rounded-xl border border-[#2E2A27] bg-[#1C1A18]/40">
                  <table className="w-full text-left text-sm">
                    <thead className="border-b border-[#2E2A27] text-stone-400">
                      <tr>
                        <SortHeader label="Service" sortKey="name" />
                        <SortHeader label="Image" sortKey="image" />
                        <SortHeader label="État" sortKey="state" />
                        <SortHeader label="Moteur" sortKey="runtime" />
                        <SortHeader label="Réseaux" sortKey="networks" />
                        <SortHeader label="Ports" sortKey="ports" />
                        <SortHeader label="Mode / Test" sortKey="guard" />
                        <th className="p-3">Compose</th>
                        <SortHeader label="Mise à jour" sortKey="update" />
                        <th className="p-3">Logs</th>
                        <th className="p-3 text-right">Actions</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-[#2E2A27]/60">
                      {sortedContainers.map((c) => (
                        <tr key={c.id} className="hover:bg-[#1C1A18]/70">
                          <td className="p-3 font-medium">{c.names[0]?.replace('/', '')}</td>
                          <td className="p-3 font-mono text-xs text-stone-400">{c.image}</td>
                          <td className="p-3">
                            <span
                              className={`px-2 py-0.5 rounded-full text-xs ${
                                c.state === 'running'
                                  ? 'bg-emerald-500/15 text-emerald-500'
                                  : 'bg-[#2E2A27] text-stone-300'
                              }`}
                            >
                              {c.state}
                            </span>
                          </td>
                          <td className="p-3 text-xs text-stone-300 capitalize">
                            {c.runtime || 'inconnu'}
                          </td>
                          <td className="p-3">
                            <CompactCell items={c.networks} icon={Network} mono={false} />
                          </td>
                          <td className="p-3">
                            <CompactCell items={c.ports} icon={Plug} />
                          </td>
                          <td className="p-3 text-xs text-stone-300">
                            {c.guard.enabled
                              ? `${c.guard.update_mode} (${c.guard.test_strategy})`
                              : 'Non géré'}
                          </td>
                          <td className="p-3">
                            <button
                              onClick={() => openCompose(c.id)}
                              className="p-1.5 rounded bg-[#2E2A27] hover:bg-orange-500/30 text-stone-400 hover:text-orange-500 transition-colors"
                              title="Afficher / modifier le compose"
                            >
                              <FileCode className="w-3.5 h-3.5" />
                            </button>
                          </td>
                          <td className="p-3">
                            {c.newer_version_available || newerVersions.has(c.id) ? (
                              <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs bg-orange-500/15 text-orange-500">
                                <PackageCheck className="w-3 h-3" /> Nouvelle version
                              </span>
                            ) : (
                              <span className="text-xs text-stone-500">—</span>
                            )}
                          </td>
                          <td className="p-3">
                            <button
                              onClick={() => openLogs(c.id)}
                              className="p-1.5 rounded bg-[#2E2A27] hover:bg-orange-500/30 text-stone-400 hover:text-orange-500 transition-colors"
                              title="Voir les logs"
                            >
                              <Eye className="w-3.5 h-3.5" />
                            </button>
                          </td>
                          <td className="p-3 text-right">
                            <div className="inline-flex items-center gap-1.5">
                              <button
                                onClick={() =>
                                  containerAction(c.id, c.state === 'paused' ? 'unpause' : 'start')
                                }
                                disabled={c.state === 'running' || c.state === 'restarting'}
                                title={c.state === 'paused' ? 'Reprendre' : 'Démarrer'}
                                className="p-1.5 rounded bg-[#2E2A27] text-emerald-400 hover:bg-emerald-500/20 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                              >
                                <Play className="w-3.5 h-3.5" />
                              </button>
                              <button
                                onClick={() =>
                                  containerAction(c.id, c.state === 'paused' ? 'unpause' : 'pause')
                                }
                                disabled={c.state !== 'running' && c.state !== 'paused'}
                                title={c.state === 'paused' ? 'Reprendre' : 'Suspendre'}
                                className="p-1.5 rounded bg-[#2E2A27] text-orange-400 hover:bg-orange-500/20 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                              >
                                {c.state === 'paused' ? (
                                  <Play className="w-3.5 h-3.5" />
                                ) : (
                                  <Pause className="w-3.5 h-3.5" />
                                )}
                              </button>
                              <button
                                onClick={() =>
                                  containerAction(c.id, 'stop')
                                }
                                disabled={c.state !== 'running' && c.state !== 'paused'}
                                title="Arrêter"
                                className="p-1.5 rounded bg-[#2E2A27] text-red-400 hover:bg-red-500/20 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                              >
                                <Square className="w-3.5 h-3.5" />
                              </button>
                              <button
                                disabled={loading}
                                onClick={() => triggerUpgrade(c.id)}
                                className="px-2.5 py-1 rounded bg-orange-600 hover:bg-orange-500 text-xs font-medium disabled:opacity-50 transition-colors"
                              >
                                Test & Update
                              </button>
                              <button
                                onClick={() =>
                                  deleteContainer(c.id, (c.names[0] || '').replace(/^\//, ''))
                                }
                                title="Supprimer le conteneur"
                                className="p-1.5 rounded bg-red-500/10 text-red-400 hover:bg-red-500/20 transition-colors"
                              >
                                <Trash2 className="w-3.5 h-3.5" />
                              </button>
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                )}
              </section>
            )}

            {tab === 'proxmox' && (
              <section>
                <div className="flex justify-between items-center mb-6">
                  <h1 className="text-xl font-semibold">
                    Cluster Proxmox VE — Nœuds, VM & LXC
                  </h1>
                  <button
                    onClick={() => openSettings('proxmox')}
                    title="Réglages Proxmox VE"
                    className="p-2 rounded-md bg-[#2E2A27] hover:bg-orange-600 text-stone-300 hover:text-white transition-colors"
                  >
                    <Settings className="w-4 h-4" />
                  </button>
                </div>
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                  {pveResources
                    .filter((r) => r.type === 'qemu' || r.type === 'lxc')
                    .map((vm) => (
                      <div
                        key={vm.id}
                        className="rounded-xl border border-[#2E2A27] bg-[#1C1A18]/50 p-4 flex flex-col justify-between gap-3"
                      >
                        <div>
                          <div className="flex justify-between items-center">
                            <span className="font-semibold">
                              {vm.name} <span className="text-xs text-stone-500">#{vm.vmid}</span>
                            </span>
                            <span
                              className={`text-xs px-2 py-0.5 rounded-full ${
                                vm.status === 'running'
                                  ? 'bg-emerald-500/15 text-emerald-500'
                                  : 'bg-orange-500/15 text-orange-500'
                              }`}
                            >
                              {vm.status}
                            </span>
                          </div>
                          <p className="text-xs text-stone-400 mt-1">
                            Nœud : {vm.node} • Type : {vm.type.toUpperCase()}
                          </p>
                        </div>
                        <div className="flex gap-2 pt-2 border-t border-[#2E2A27]/80">
                          <button
                            className="p-1.5 rounded bg-[#2E2A27] hover:bg-emerald-500/30 text-emerald-500 transition-colors"
                            title="Démarrer"
                          >
                            <Play className="w-3.5 h-3.5" />
                          </button>
                          <button
                            className="p-1.5 rounded bg-[#2E2A27] hover:bg-red-500/30 text-red-500 transition-colors"
                            title="Arrêter"
                          >
                            <Square className="w-3.5 h-3.5" />
                          </button>
                          <button
                            className="p-1.5 rounded bg-[#2E2A27] hover:bg-orange-500/30 text-orange-500 transition-colors"
                            title="Snapshot"
                          >
                            <Camera className="w-3.5 h-3.5" />
                          </button>
                        </div>
                      </div>
                    ))}
                </div>
              </section>
            )}

            {tab === 'ssh' && (
              <section>
                <div className="flex justify-between items-center mb-4">
                  <h1 className="text-xl font-semibold">Bastion SSH Zero-Trust (Clés Ed25519)</h1>
                  <div className="flex items-center gap-2">
                    {sshHosts.map((h) => (
                      <button
                        key={h.id}
                        onClick={() => setActiveSsh(h)}
                        className={`flex items-center gap-2 px-3 py-1.5 rounded-md text-xs font-medium ${
                          activeSsh?.id === h.id
                            ? 'bg-orange-600 text-white'
                            : 'bg-[#2E2A27] text-stone-300'
                        }`}
                      >
                        <KeyRound className="w-3.5 h-3.5" /> {h.username}@{h.name}
                      </button>
                    ))}
                    <button
                      onClick={() => openSettings('ssh')}
                      title="Réglages Bastion SSH"
                      className="p-2 rounded-md bg-[#2E2A27] hover:bg-orange-600 text-stone-300 hover:text-white transition-colors"
                    >
                      <Settings className="w-4 h-4" />
                    </button>
                  </div>
                </div>
                {activeSsh ? (
                  <SshTerminal
                    hostId={activeSsh.id}
                    hostLabel={`${activeSsh.username}@${activeSsh.host}`}
                  />
                ) : (
                  <div className="rounded-xl border border-dashed border-[#2E2A27] p-12 text-center text-stone-500 text-sm">
                    Sélectionnez un hôte Linux enregistré pour ouvrir une session terminal chiffrée
                    sans mot de passe.
                  </div>
                )}
              </section>
            )}

            {tab === 'ai' && (
              <section className="max-w-3xl">
                <div className="flex justify-between items-center mb-4">
                  <h1 className="text-xl font-semibold">
                    Générateur de Stacks Compose & Labels guard.*
                  </h1>
                  <button
                    onClick={() => openSettings('ai')}
                    title="Réglages Agent IA & Ops"
                    className="p-2 rounded-md bg-[#2E2A27] hover:bg-orange-600 text-stone-300 hover:text-white transition-colors"
                  >
                    <Settings className="w-4 h-4" />
                  </button>
                </div>
                <textarea
                  rows={3}
                  value={aiPrompt}
                  onChange={(e) => setAiPrompt(e.target.value)}
                  placeholder="Ex: Déploie une instance Redis avec persistance AOF et healthcheck TCP..."
                  className="w-full rounded-lg border border-[#2E2A27] bg-[#1C1A18] p-3 text-sm focus:border-orange-500 focus:outline-none"
                />
                <button
                  disabled={loading || !aiPrompt.trim()}
                  onClick={generateCompose}
                  className="mt-3 px-4 py-2 rounded-lg bg-orange-600 hover:bg-orange-500 text-sm font-medium disabled:opacity-50 transition-colors"
                >
                  {loading ? 'Génération en cours...' : 'Générer le docker-compose.yml'}
                </button>
                {aiYaml && (
                  <pre className="mt-6 rounded-xl border border-[#2E2A27] bg-[#1C1A18] p-4 text-xs font-mono text-emerald-300 overflow-x-auto">
                    {aiYaml}
                  </pre>
                )}
              </section>
            )}
          </>
        )}

        <StackEditor
          base={nodeBase}
          open={stackEditorOpen}
          initialName={stackEditing}
          onClose={() => setStackEditorOpen(false)}
          onDeployed={() => {
            loadDocker();
            setResourceRefresh((v) => v + 1);
          }}
        />

        {/* New Container Modal (compose without AI) */}
        {createOpen && (
          <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm p-4">
            <div className="flex max-h-[90vh] w-full max-w-3xl flex-col bg-[#1C1A18] rounded-xl border border-[#2E2A27] shadow-2xl">
              <div className="flex shrink-0 flex-wrap items-center gap-3 p-4 border-b border-[#2E2A27]">
                <div className="flex items-center gap-2">
                  <FileCode className="w-4 h-4 text-orange-500" />
                  <h3 className="text-lg font-semibold text-stone-100">Nouveau conteneur</h3>
                </div>
                <input
                  autoFocus
                  autoComplete="off"
                  spellCheck={false}
                  value={createName}
                  onChange={(e) => changeCreateName(e.target.value)}
                  placeholder="nom-du-conteneur"
                  className="w-56 rounded-lg border border-[#2E2A27] bg-[#121110] px-3 py-1.5 font-mono text-sm text-orange-400 placeholder-stone-600 focus:border-orange-500 focus:outline-none"
                />
                <span className="px-2 py-0.5 rounded-full text-[10px] bg-orange-500/15 text-orange-400">
                  compose
                </span>
                <button
                  onClick={() => setCreateOpen(false)}
                  className="ml-auto text-stone-400 hover:text-white transition-colors"
                >
                  ✕
                </button>
              </div>

              <div className="min-h-0 flex-1 overflow-y-auto p-4">
                {createNameTaken && (
                  <div className="mb-3 flex flex-wrap items-center gap-3 rounded-lg border border-red-500/30 bg-red-950/25 p-3 text-xs text-red-300">
                    <AlertCircle className="w-4 h-4 shrink-0" />
                    <span>
                      Un conteneur nommé <code className="font-mono">{createName.trim()}</code>{' '}
                      existe déjà.
                    </span>
                    <button
                      onClick={() => changeCreateName(suggestName(createName.trim()))}
                      className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-orange-600 hover:bg-orange-500 text-white text-xs font-medium transition-colors"
                    >
                      Utiliser « {suggestName(createName.trim())} »
                    </button>
                  </div>
                )}
                <div className="flex items-center gap-1 mb-3 border-b border-[#2E2A27]">
                  <button
                    onClick={() => setCreateTab('compose')}
                    className={`inline-flex items-center gap-2 px-3 py-2 text-xs font-medium border-b-2 -mb-px transition-colors ${
                      createTab === 'compose'
                        ? 'border-orange-500 text-orange-500'
                        : 'border-transparent text-stone-400 hover:text-stone-200'
                    }`}
                  >
                    <FileCode className="w-3.5 h-3.5" /> docker-compose.yml
                  </button>
                  {createEnvExists ? (
                    <button
                      onClick={() => setCreateTab('env')}
                      className={`inline-flex items-center gap-2 px-3 py-2 text-xs font-medium border-b-2 -mb-px transition-colors ${
                        createTab === 'env'
                          ? 'border-orange-500 text-orange-500'
                          : 'border-transparent text-stone-400 hover:text-stone-200'
                      }`}
                    >
                      <FileText className="w-3.5 h-3.5" /> .env
                    </button>
                  ) : (
                    <button
                      onClick={() => {
                        setCreateEnvExists(true);
                        setCreateTab('env');
                      }}
                      title="Créer un fichier .env associé"
                      className="inline-flex items-center gap-2 px-3 py-2 text-xs font-medium border-b-2 border-transparent -mb-px text-stone-500 hover:text-orange-500 transition-colors"
                    >
                      <Plus className="w-3.5 h-3.5" /> .env
                    </button>
                  )}
                  <EnvGapBadge validation={validation} />
                </div>

                {createTab === 'compose' ? (
                  <CodeEditor
                    value={createYaml}
                    onChange={(v) => {
                      setCreateYaml(v);
                      setCreateTouched(true);
                    }}
                    height="min(38vh, 420px)"
                    errorLines={errorLinesOf(validation)}
                  />
                ) : (
                  <CodeEditor
                    value={createEnv}
                    onChange={setCreateEnv}
                    placeholder="CLE=VALEUR"
                    tone="env"
                    height="min(38vh, 420px)"
                  />
                )}


                {(validation?.services.length ?? 0) > 1 && (
                  <div className="mt-3 flex items-start gap-2 rounded-lg border border-orange-500/30 bg-orange-950/20 p-3 text-xs text-orange-200">
                    <AlertCircle className="w-4 h-4 mt-0.5 shrink-0" />
                    <span>
                      Ce compose déclare {validation?.services.length} services. La création simple
                      n'accepte qu'un seul conteneur : utilisez le bouton{' '}
                      <button
                        onClick={() => {
                          setCreateOpen(false);
                          setStackEditing(null);
                          setStackEditorOpen(true);
                        }}
                        className="font-medium underline hover:text-orange-100"
                      >
                        Stacks
                      </button>{' '}
                      pour les déployer ensemble.
                    </span>
                  </div>
                )}

                {/* Compose validation feedback */}
                <ComposeValidationPanel
                  validation={validation}
                  validating={validating}
                  envText={createEnv}
                  onEnvChange={setCreateEnv}
                  onEnvUsed={() => setCreateEnvExists(true)}
                />

                {createResult && (
                  <div
                    className={`mt-3 rounded-lg border p-3 text-xs ${
                      createResult.status === 'created'
                        ? 'border-emerald-500/30 bg-emerald-950/20 text-emerald-300'
                        : 'border-red-500/30 bg-red-950/25 text-red-300'
                    }`}
                  >
                    <p className="font-medium mb-1">
                      {createResult.status === 'created'
                        ? `Conteneur « ${createResult.container_name} » créé et démarré.`
                        : 'Création échouée — conteneur supprimé.'}
                    </p>
                    {createResult.message && (
                      <p className="opacity-80">{createResult.message}</p>
                    )}
                    {createResult.ai_diagnosis && (
                      <p className="mt-2 opacity-90">
                        <span className="font-medium">Diagnostic IA : </span>
                        {createResult.ai_diagnosis}
                      </p>
                    )}
                    {createResult.logs && (
                      <pre className="mt-2 max-h-32 overflow-auto rounded bg-[#121110] p-2 font-mono text-[10px] text-stone-300 whitespace-pre-wrap">
                        {createResult.logs}
                      </pre>
                    )}
                  </div>
                )}
              </div>

              <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-t border-[#2E2A27] p-4">
                <p className="text-xs text-stone-500">
                  {createTab === 'env'
                    ? 'Fichier .env associé (substitution ${VAR} dans le compose).'
                    : 'Le nom saisi ci-dessus devient le nom du conteneur.'}
                </p>
                <div className="flex items-center gap-3">
                  {createMsg && <span className="text-xs text-red-300">{createMsg}</span>}
                  <button
                    onClick={createContainer}
                    disabled={
                      creating ||
                      !createName.trim() ||
                      validating ||
                      (validation !== null && !validation.valid) ||
                      (validation?.services.length ?? 0) > 1 ||
                      createNameTaken
                    }
                    title={
                      validation && !validation.valid
                        ? 'Corrigez les erreurs de validation avant de créer le conteneur'
                        : 'Créer et démarrer le conteneur'
                    }
                    className="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-orange-600 hover:bg-orange-500 text-sm font-medium disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                  >
                    {creating || validating ? (
                      <Loader2 className="w-4 h-4 animate-spin" />
                    ) : (
                      <Rocket className="w-4 h-4" />
                    )}
                    {creating ? 'Création...' : 'Créer le conteneur'}
                  </button>
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Logs Modal */}
        {logsModalOpen && (
          <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm">
            <div className="w-full max-w-4xl bg-[#1C1A18] rounded-xl border border-[#2E2A27] shadow-2xl">
              <div className="flex items-center justify-between p-4 border-b border-[#2E2A27]">
                <h3 className="text-lg font-semibold text-stone-100">Logs du conteneur</h3>
                <button
                  onClick={() => setLogsModalOpen(false)}
                  className="text-stone-400 hover:text-white transition-colors"
                >
                  ✕
                </button>
              </div>
              <div className="p-4 max-h-[60vh] overflow-y-auto">
                {loading ? (
                  <div className="flex items-center justify-center py-8">
                    <RefreshCw className="w-6 h-6 animate-spin text-orange-500" />
                    <span className="ml-2 text-stone-400">Chargement des logs...</span>
                  </div>
                ) : (
                  <pre className="font-mono text-xs text-emerald-300 whitespace-pre-wrap bg-[#121110] p-4 rounded-lg overflow-x-auto max-h-[400px]">
                    {containerLogs || 'Aucun log disponible'}
                  </pre>
                )}
              </div>
            </div>
          </div>
        )}

        {/* Compose Viewer / Editor Modal */}
        {composeOpen && (
          <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm p-4">
            <div className="flex max-h-[90vh] w-full max-w-3xl flex-col bg-[#1C1A18] rounded-xl border border-[#2E2A27] shadow-2xl">
              <div className="flex shrink-0 items-center justify-between p-4 border-b border-[#2E2A27]">
                <div className="flex items-center gap-2">
                  <FileCode className="w-4 h-4 text-orange-500" />
                  <h3 className="text-lg font-semibold text-stone-100">
                    Compose — <span className="font-mono text-orange-500">{composeService}</span>
                  </h3>
                  <span
                    className={`px-2 py-0.5 rounded-full text-[10px] ${
                      composeSource === 'stored'
                        ? 'bg-emerald-500/15 text-emerald-500'
                        : 'bg-[#2E2A27] text-stone-400'
                    }`}
                  >
                    {composeSource === 'stored' ? 'enregistré' : 'généré'}
                  </span>
                </div>
                <button
                  onClick={() => setComposeOpen(false)}
                  className="text-stone-400 hover:text-white transition-colors"
                >
                  ✕
                </button>
              </div>
              <div className="min-h-0 flex-1 overflow-y-auto p-4">
                {composeLoading ? (
                  <div className="flex items-center justify-center py-8">
                    <RefreshCw className="w-6 h-6 animate-spin text-orange-500" />
                    <span className="ml-2 text-stone-400">Chargement du compose...</span>
                  </div>
                ) : (
                  <>
                    <div className="flex items-center gap-1 mb-3 border-b border-[#2E2A27]">
                      <button
                        onClick={() => setComposeTab('compose')}
                        className={`inline-flex items-center gap-2 px-3 py-2 text-xs font-medium border-b-2 -mb-px transition-colors ${
                          composeTab === 'compose'
                            ? 'border-orange-500 text-orange-500'
                            : 'border-transparent text-stone-400 hover:text-stone-200'
                        }`}
                      >
                        <FileCode className="w-3.5 h-3.5" /> docker-compose.yml
                      </button>
                      {composeEnvExists ? (
                        <button
                          onClick={() => setComposeTab('env')}
                          className={`inline-flex items-center gap-2 px-3 py-2 text-xs font-medium border-b-2 -mb-px transition-colors ${
                            composeTab === 'env'
                              ? 'border-orange-500 text-orange-500'
                              : 'border-transparent text-stone-400 hover:text-stone-200'
                          }`}
                        >
                          <FileText className="w-3.5 h-3.5" /> .env
                        </button>
                      ) : (
                        <button
                          onClick={() => {
                            setComposeEnvExists(true);
                            setComposeTab('env');
                          }}
                          title="Créer un fichier .env associé"
                          className="inline-flex items-center gap-2 px-3 py-2 text-xs font-medium border-b-2 border-transparent -mb-px text-stone-500 hover:text-orange-500 transition-colors"
                        >
                          <Plus className="w-3.5 h-3.5" /> .env
                        </button>
                      )}
                      <EnvGapBadge validation={composeValidation} />
                      {(composeValidation?.services.length ?? 0) > 1 && (
                        <div className="ml-auto flex items-center gap-2 mb-1">
                          <span className="text-[11px] text-stone-500">Service</span>
                          <select
                            value={applyService}
                            onChange={(e) => setApplyService(e.target.value)}
                            className="rounded-md border border-[#2E2A27] bg-[#121110] px-2 py-1 font-mono text-xs text-orange-300 focus:border-orange-500 focus:outline-none"
                          >
                            <option value="">— sélectionner —</option>
                            {composeValidation?.services.map((svc) => (
                              <option key={svc} value={svc}>
                                {svc}
                              </option>
                            ))}
                          </select>
                        </div>
                      )}
                    </div>

                    {composeTab === 'compose' ? (
                      <CodeEditor
                        value={composeYaml}
                        onChange={setComposeYaml}
                        height="min(38vh, 420px)"
                        errorLines={errorLinesOf(composeValidation)}
                      />
                    ) : (
                      <CodeEditor
                        value={composeEnv}
                        onChange={setComposeEnv}
                        placeholder="CLE=VALEUR"
                        tone="env"
                        height="min(38vh, 420px)"
                      />
                    )}

                    <ComposeValidationPanel
                      validation={composeValidation}
                      validating={composeValidating}
                      envText={composeEnv}
                      onEnvChange={setComposeEnv}
                      onEnvUsed={() => setComposeEnvExists(true)}
                    />


                    {composeApplyResult && (
                      <div
                        className={`mt-3 rounded-lg border p-3 text-xs ${
                          composeApplyResult.status === 'applied'
                            ? 'border-emerald-500/30 bg-emerald-950/20 text-emerald-300'
                            : 'border-red-500/30 bg-red-950/25 text-red-300'
                        }`}
                      >
                        <p className="font-medium mb-1">
                          {composeApplyResult.status === 'applied'
                            ? 'Conteneur recréé et démarré.'
                            : 'Échec — conteneur précédent restauré (rollback).'}
                        </p>
                        {composeApplyResult.message && <p className="opacity-80">{composeApplyResult.message}</p>}
                        {composeApplyResult.ai_diagnosis && (
                          <p className="mt-2 opacity-90">
                            <span className="font-medium">Diagnostic IA : </span>
                            {composeApplyResult.ai_diagnosis}
                          </p>
                        )}
                        {composeApplyResult.logs && (
                          <pre className="mt-2 max-h-32 overflow-auto rounded bg-[#121110] p-2 font-mono text-[10px] text-stone-300 whitespace-pre-wrap">
                            {composeApplyResult.logs}
                          </pre>
                        )}
                      </div>
                    )}
                  </>
                )}
              </div>

              <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-t border-[#2E2A27] p-4">
                <p className="text-xs text-stone-500">
                  {composeTab === 'env'
                    ? 'Fichier .env associé au service. Enregistrez pour le conserver.'
                    : composeSource === 'generated'
                    ? 'Aperçu généré depuis le conteneur. Enregistrez pour conserver vos modifications.'
                    : 'Définition enregistrée pour ce service.'}
                </p>
                <div className="flex items-center gap-3">
                  {composeMsg && <span className="text-xs text-stone-300">{composeMsg}</span>}
                  <button
                    onClick={saveCompose}
                    disabled={composeSaving || composeApplying}
                    className="inline-flex items-center gap-2 px-3 py-2 rounded-lg bg-[#2E2A27] hover:bg-[#3a3531] text-sm font-medium disabled:opacity-50 transition-colors"
                  >
                    <Save className="w-4 h-4" />
                    {composeSaving ? 'Enregistrement...' : 'Enregistrer'}
                  </button>
                  <button
                    onClick={applyCompose}
                    disabled={
                      composeSaving ||
                      composeApplying ||
                      composeValidating ||
                      (composeValidation !== null && !composeValidation.valid) ||
                      ((composeValidation?.services.length ?? 0) > 1 && !applyService)
                    }
                    title={
                      composeValidation && !composeValidation.valid
                        ? 'Corrigez les erreurs de validation avant d’appliquer'
                        : 'Enregistrer puis recréer le conteneur (rollback automatique)'
                    }
                    className="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-orange-600 hover:bg-orange-500 text-sm font-medium disabled:opacity-50 transition-colors"
                  >
                    <Rocket className={`w-4 h-4 ${composeApplying ? 'animate-pulse' : ''}`} />
                    {composeApplying ? 'Application...' : 'Enregistrer et Appliquer'}
                  </button>
                </div>
              </div>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}
