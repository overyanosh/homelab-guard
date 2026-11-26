# 🎨 Homelab Guard - Design System v0.3.0
## "Industrial Vigilance"

Une charte graphique optimisée pour une surveillance prolongée d'interface avec un confort visuel amélioré.

---

## Palette de Couleurs

### Fond & Surfaces

| Rôle | Nom | Code Hex | Tailwind | Usage |
|------|-----|----------|----------|-------|
| Fond principal | **Obsidian Warm** | `#121110` | `stone-950` | Arrière-plan global WebUI |
| Surface / Cartes | **Rack Charcoal** | `#1C1A18` | `stone-900` | Sidebar, cartes VM, tableaux |
| Bordures / Survols | **Anodized Taupe** | `#2E2A27` | `stone-800` | Séparateurs, contours hover |
| Texte principal | **Warm Alabaster** | `#F5F2EF` | `stone-100` | Titres, noms conteneurs |
| Texte secondaire | **Muted Ash** | `#A8A29E` | `stone-400` | Labels, métadonnées |

### Accents

| Rôle | Nom | Code Hex | Tailwind | Usage |
|------|-----|----------|----------|-------|
| Accent primaire | **Guard Amber** | `#F97316` | `orange-500` | Boutons actions, onglet actif, curseur SSH |
| Accent intense | **Signal Blaze** | `#EA580C` | `orange-600` | États :hover, badges alerte sandbox |
| Accent doux | **Ember Glow** | Orange 500/15 | `orange-500/15` | Fond éléments actifs navigation |

### Sémantique

| État | Nom | Code Hex | Tailwind | Usage |
|------|-----|----------|----------|-------|
| ✅ Sain | **Matrix Emerald** | `#10B981` | `emerald-500` | Container running, test validé (promoted) |
| ❌ Échec | **Crimson Alert** | `#EF4444` | `red-500` | Rolled-back, erreur SSH, santé critique |
| ⚠️ Attention | **Amber Warning** | `#F97316` | `amber-500` | Mises à jour disponibles, snapshots |

---

## Typographie

### Interface & Titres
- **Police**: Inter ou Plus Jakarta Sans
- **Graisses**: 500 (medium), 600 (semibold), 700 (bold)
- **Tracking**: `tracking-tight` sur le nom de marque pour compacité

### Code, Labels & Terminal
- **Police**: JetBrains Mono
- **Usage**: Affichage des labels guard.*, hashes de conteneurs, YAML généré par IA
- **Taille**: 13px minimum pour lisibilité prolongée

---

## Composants Clés

### Logo - "Le Bouclier Modulaire"

**Concept**: Synthèse des 4 piliers (Docker, Proxmox, Bastion SSH, Agent IA)

**Éléments**:
- **Hexagone/Bouclier** (`#D6D3D1`) → Périmètre Zero-Trust, coffre-fort AES-256-GCM
- **Chevrons empilés** (`#78716C`) → Rack serveur, empilement conteneurs/VM
- **Noyau losange** (`#F97316`) → Cœur orchestrateur Go + œil agent IA

**Variante Horzontale**: Icône 28x28px + texte HOMELAB (bold #F5F2EF) + GUARD (orange)

**Variante Monochrome**: Version grise (#A8A29E) pour CLI/Terminal SSH

### Favicon Tab Icon

Design simplifié pour 16x16px:
- Silhouette bouclier pleine `#292524` bordure claire
- Noyau orange saturé (#F97316) occupant 50% centre
- Contraste fort pour identification instantanée

**UX Dynamique** (React):
- Normal: Noyau fixe orange
- Test Sandbox: Point lumineux pulsant en haut-droit
- Rollback: Pastille rouge tant que non-acquitte

---

## Applications Directes

### Dans App.tsx

```tsx
// Backgrounds & Surfaces
<div className="min-h-screen bg-[#121110] text-stone-100">
  <aside className="bg-[#1C1A18]/70 border-[#2E2A27]">
    // Actif
    {tab === 'docker' ? 'bg-orange-500/15 text-orange-500' : 'text-stone-400'}
    // Hover
    'hover:bg-[#2E2A27]'
  </aside>
  
  // Tableaux
  <div className="border-[#2E2A27] bg-[#1C1A18]/40">
    <thead className="border-b-[#2E2A27] text-stone-400">
    <tbody className="divide-y-[#2E2A27]/60">
    
  // Badges état
  <span className={state === 'running' 
    ? 'bg-emerald-500/15 text-emerald-500' 
    : 'bg-[#2E2A27] text-stone-300'}>
    {state}
  </span>
  
  // Boutons actions
  <button className="bg-orange-600 hover:bg-orange-500 transition-colors">
    Test & Update
  </button>
  
  // Mise à jour badge
  {hasUpdate && (
    <span className="bg-orange-500/15 text-orange-500">
      Nouvelle version
    </span>
  )}
</div>
```

### Dans SshTerminal.tsx

```tsx
const term = new Terminal({
  theme: {
    background: '#121110',
    foreground: '#f5f2ef',
    cursor: '#f97316',
    selectionBackground: '#f9731640',
  },
});
```

### Assets

- **Logo**: `frontend/public/logo.svg` (48x48px, horizontal)
- **Favicon**: `frontend/public/favicon.svg` (32x32px, carré)
- **Index.html**: `<link rel="icon" type="image/svg+xml" href="/favicon.svg" />`

---

## Principles

1. **Confort Visuel Prolongé**: Gris chauds réduisent la fatigue oculaire vs gris bleutés froids
2. **Hiérarchie Claire**: Orange attire immédiatement l'œil sur actions critiques
3. **Isolation Sécurité**: Bordures distinctes, surfaces contrastées pour séparation logique
4. **Performance Perçue**: Transitions douces, états clairs, feedback immédiat
5. **Accessibilité**: Contraste 4.5:1 minimum, couleurs sensibles daltonisme évitées

---

*Développé avec ❤️ pour la communauté Homelab*
