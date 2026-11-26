import { useEffect, useState } from 'react';
import { Shield, Box, Server, Terminal as TermIcon, Bot, RefreshCw, Play, Square, Camera, KeyRound, Eye, PackageCheck } from 'lucide-react';
import { SshTerminal } from './components/SshTerminal';

type Tab = 'docker' | 'proxmox' | 'ssh' | 'ai';

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
  newer_version_available?: boolean;
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

export default function App() {
  const [tab, setTab] = useState<Tab>('docker');
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

  const loadDocker = async () => {
    const res = await fetch('/api/docker/containers');
    if (res.ok) {
      const data = await res.json();
      setContainers(data);
      
      // Check for newer versions after containers are loaded
      await checkForNewerVersions(data);
    }
  };

  const checkForNewerVersions = async (containersToCheck: ContainerItem[] = containers) => {
    const checks: Promise<{id: string, hasUpdate: boolean}>[] = [];
    
    containersToCheck.forEach(c => {
      if (c.newer_version_available === undefined && !checkingUpdates[c.id]) {
        setCheckingUpdates(prev => ({...prev, [c.id]: true}));
        checks.push(
          fetch('/api/docker/images/check-update', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({image_name: c.image}),
          })
            .then(res => res.json())
            .then((data: {has_update: boolean}) => ({id: c.id, hasUpdate: data.has_update}))
            .catch(() => ({id: c.id, hasUpdate: false}))
            .finally(() => setCheckingUpdates(prev => ({...prev, [c.id]: false}))
            )
        );
      }
    });
    
    if (checks.length > 0) {
      const results = await Promise.all(checks);
      const updatedSet = new Set(newerVersions);
      results.forEach(r => {
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
      const res = await fetch(`/api/docker/containers/${id}/logs`);
      if (res.ok) {
        const text = await res.text();
        setContainerLogs(text);
      }
    } catch (err) {
      console.error('Failed to load logs:', err);
    }
    setLoading(false);
  };

  const loadProxmox = async () => {
    const clustersRes = await fetch('/api/proxmox/clusters');
    if (!clustersRes.ok) return;
    const clusters = await clustersRes.json();
    if (clusters?.length > 0) {
      const res = await fetch(`/api/proxmox/clusters/${clusters[0].id}/resources`);
      if (res.ok) setPveResources(await res.json());
    }
  };

  const loadSshHosts = async () => {
    const res = await fetch('/api/ssh/hosts');
    if (res.ok) setSshHosts(await res.json());
  };

  useEffect(() => {
    loadDocker();
    loadProxmox();
    loadSshHosts();
  }, []);

  const triggerUpgrade = async (id: string) => {
    setLoading(true);
    setStatusBanner('Exécution du pipeline Sandbox & Non-Régression...');
    const res = await fetch(`/api/docker/containers/${id}/upgrade?force=true`, { method: 'POST' });
    const data = await res.json();
    setStatusBanner(
      data.status === 'rolled-back'
        ? `Rollback effectué sur ${data.container_name} — Diagnostic IA : ${data.ai_diagnosis}`
        : `Résultat (${data.container_name}) : ${data.status}`
    );
    setLoading(false);
    loadDocker();
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

  return (
    <div className="min-h-screen bg-[#121110] text-stone-100 flex">
      <aside className="w-64 border-r border-[#2E2A27] bg-[#1C1A18]/70 p-4 flex flex-col gap-2">
        <div className="flex items-center gap-2.5 px-2 py-3 mb-4 border-b border-[#2E2A27]">
          <Shield className="w-6 h-6 text-orange-500" />
          <span className="font-bold tracking-tight text-lg">HOMELAB<span className="text-orange-500"> GUARD</span></span>
        </div>
        <button onClick={() => setTab('docker')} className={`flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium ${tab === 'docker' ? 'bg-orange-500/15 text-orange-500' : 'text-stone-400 hover:bg-[#2E2A27]'}`}>
          <Box className="w-4 h-4" /> Docker & Swarm
        </button>
        <button onClick={() => setTab('proxmox')} className={`flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium ${tab === 'proxmox' ? 'bg-orange-500/15 text-orange-500' : 'text-stone-400 hover:bg-[#2E2A27]'}`}>
          <Server className="w-4 h-4" /> Proxmox VE
        </button>
        <button onClick={() => setTab('ssh')} className={`flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium ${tab === 'ssh' ? 'bg-orange-500/15 text-orange-500' : 'text-stone-400 hover:bg-[#2E2A27]'}`}>
          <TermIcon className="w-4 h-4" /> Bastion SSH
        </button>
        <button onClick={() => setTab('ai')} className={`flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium ${tab === 'ai' ? 'bg-orange-500/15 text-orange-500' : 'text-stone-400 hover:bg-[#2E2A27]'}`}>
          <Bot className="w-4 h-4" /> Agent IA Ops
        </button>
      </aside>

      <main className="flex-1 p-6 overflow-y-auto">
        {statusBanner && (
          <div className="mb-6 rounded-lg border border-orange-500/30 bg-orange-950/40 p-4 text-sm text-orange-200 flex justify-between items-start">
            <pre className="whitespace-pre-wrap font-sans">{statusBanner}</pre>
            <button onClick={() => setStatusBanner(null)} className="text-xs text-stone-400 hover:text-white ml-4">Fermer</button>
          </div>
        )}

        {tab === 'docker' && (
          <section>
            <div className="flex justify-between items-center mb-6">
              <h1 className="text-xl font-semibold">Conteneurs Docker & Podman</h1>
              <button onClick={loadDocker} className="flex items-center gap-2 px-3 py-1.5 rounded-md bg-[#2E2A27] hover:bg-orange-600 text-sm transition-colors">
                <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} /> Actualiser
              </button>
            </div>
            <div className="overflow-x-auto rounded-xl border border-[#2E2A27] bg-[#1C1A18]/40">
              <table className="w-full text-left text-sm">
                <thead className="border-b border-[#2E2A27] text-stone-400">
                  <tr>
                    <th className="p-3">Service</th>
                    <th className="p-3">Image</th>
                    <th className="p-3">État</th>
                    <th className="p-3">Moteur</th>
                    <th className="p-3">Mode / Test</th>
                    <th className="p-3">Mise à jour</th>
                    <th className="p-3">Logs</th>
                    <th className="p-3 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[#2E2A27]/60">
                  {containers.map((c) => (
                    <tr key={c.id} className="hover:bg-[#1C1A18]/70">
                      <td className="p-3 font-medium">{c.names[0]?.replace('/', '')}</td>
                      <td className="p-3 font-mono text-xs text-stone-400">{c.image}</td>
                      <td className="p-3">
                        <span className={`px-2 py-0.5 rounded-full text-xs ${c.state === 'running' ? 'bg-emerald-500/15 text-emerald-500' : 'bg-[#2E2A27] text-stone-300'}`}>
                          {c.state}
                        </span>
                      </td>
                      <td className="p-3 text-xs text-stone-300 capitalize">
                        {c.runtime || 'inconnu'}
                      </td>
                      <td className="p-3 text-xs text-stone-300">
                        {c.guard.enabled ? `${c.guard.update_mode} (${c.guard.test_strategy})` : 'Non géré'}
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
                        <button disabled={loading} onClick={() => triggerUpgrade(c.id)} className="px-2.5 py-1 rounded bg-orange-600 hover:bg-orange-500 text-xs font-medium disabled:opacity-50 transition-colors">
                          Test & Update
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        )}

        {tab === 'proxmox' && (
          <section>
            <h1 className="text-xl font-semibold mb-6">Cluster Proxmox VE — Nœuds, VM & LXC</h1>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {pveResources.filter((r) => r.type === 'qemu' || r.type === 'lxc').map((vm) => (
                <div key={vm.id} className="rounded-xl border border-[#2E2A27] bg-[#1C1A18]/50 p-4 flex flex-col justify-between gap-3">
                  <div>
                    <div className="flex justify-between items-center">
                      <span className="font-semibold">{vm.name} <span className="text-xs text-stone-500">#{vm.vmid}</span></span>
                      <span className={`text-xs px-2 py-0.5 rounded-full ${vm.status === 'running' ? 'bg-emerald-500/15 text-emerald-500' : 'bg-orange-500/15 text-orange-500'}`}>
                        {vm.status}
                      </span>
                    </div>
                    <p className="text-xs text-stone-400 mt-1">Nœud : {vm.node} • Type : {vm.type.toUpperCase()}</p>
                  </div>
                  <div className="flex gap-2 pt-2 border-t border-[#2E2A27]/80">
                    <button className="p-1.5 rounded bg-[#2E2A27] hover:bg-emerald-500/30 text-emerald-500 transition-colors" title="Démarrer"><Play className="w-3.5 h-3.5" /></button>
                    <button className="p-1.5 rounded bg-[#2E2A27] hover:bg-red-500/30 text-red-500 transition-colors" title="Arrêter"><Square className="w-3.5 h-3.5" /></button>
                    <button className="p-1.5 rounded bg-[#2E2A27] hover:bg-orange-500/30 text-orange-500 transition-colors" title="Snapshot"><Camera className="w-3.5 h-3.5" /></button>
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
              <div className="flex gap-2">
                {sshHosts.map((h) => (
                  <button
                    key={h.id}
                    onClick={() => setActiveSsh(h)}
                    className={`flex items-center gap-2 px-3 py-1.5 rounded-md text-xs font-medium ${activeSsh?.id === h.id ? 'bg-orange-600 text-white' : 'bg-[#2E2A27] text-stone-300'}`}
                  >
                    <KeyRound className="w-3.5 h-3.5" /> {h.username}@{h.name}
                  </button>
                ))}
              </div>
            </div>
            {activeSsh ? (
              <SshTerminal hostId={activeSsh.id} hostLabel={`${activeSsh.username}@${activeSsh.host}`} />
            ) : (
              <div className="rounded-xl border border-dashed border-[#2E2A27] p-12 text-center text-stone-500 text-sm">
                Sélectionnez un hôte Linux enregistré pour ouvrir une session terminal chiffrée sans mot de passe.
              </div>
            )}
          </section>
        )}

        {tab === 'ai' && (
          <section className="max-w-3xl">
            <h1 className="text-xl font-semibold mb-4">Générateur de Stacks Compose & Labels guard.*</h1>
            <textarea
              rows={3}
              value={aiPrompt}
              onChange={(e) => setAiPrompt(e.target.value)}
              placeholder="Ex: Déploie une instance Redis avec persistance AOF et healthcheck TCP..."
              className="w-full rounded-lg border border-[#2E2A27] bg-[#1C1A18] p-3 text-sm focus:border-orange-500 focus:outline-none"
            />
            <button disabled={loading || !aiPrompt.trim()} onClick={generateCompose} className="mt-3 px-4 py-2 rounded-lg bg-orange-600 hover:bg-orange-500 text-sm font-medium disabled:opacity-50 transition-colors">
              {loading ? 'Génération en cours...' : 'Générer le docker-compose.yml'}
            </button>
            {aiYaml && (
              <pre className="mt-6 rounded-xl border border-[#2E2A27] bg-[#1C1A18] p-4 text-xs font-mono text-emerald-300 overflow-x-auto">
                {aiYaml}
              </pre>
            )}
          </section>
        )}

        {/* Logs Modal */}
        {logsModalOpen && (
          <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm">
            <div className="w-full max-w-4xl bg-[#1C1A18] rounded-xl border border-[#2E2A27] shadow-2xl">
              <div className="flex items-center justify-between p-4 border-b border-[#2E2A27]">
                <h3 className="text-lg font-semibold text-stone-100">Logs du conteneur</h3>
                <button onClick={() => setLogsModalOpen(false)} className="text-stone-400 hover:text-white transition-colors">
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
      </main>
    </div>
  );
}
