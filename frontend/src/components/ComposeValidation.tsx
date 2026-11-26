import { AlertCircle, CheckCircle2, Loader2, Lock } from 'lucide-react';

export interface ValidationIssue {
  severity: string;
  line?: number;
  field?: string;
  message: string;
}

export interface ValidationResult {
  valid: boolean;
  errors: ValidationIssue[];
  warnings: ValidationIssue[];
  services: string[];
  missing_env?: string[];
  empty_env?: string[];
}

export const isSensitiveKey = (key: string) =>
  /KEY|SECRET|TOKEN|PASSWORD|PASSWD|PWD/i.test(key);

/** Returns the keys declared in a .env text. */
export const envKeys = (text: string): string[] =>
  text
    .split('\n')
    .map((line) => line.match(/^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=/))
    .filter((m): m is RegExpMatchArray => m !== null)
    .map((m) => m[1]);

/** Sets KEY=value in a .env text, replacing the line or appending it. */
export const setEnvValue = (text: string, key: string, value: string): string => {
  const lines = text.split('\n');
  let found = false;
  const updated = lines.map((line) => {
    const m = line.match(/^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=/);
    if (m && m[1] === key) {
      found = true;
      return `${key}=${value}`;
    }
    return line;
  });
  if (!found) updated.push(`${key}=${value}`);
  return updated.join('\n');
};

/** Appends KEY= for every key not already declared. */
export const addEnvKeys = (text: string, keys: string[]): string => {
  const existing = new Set(envKeys(text));
  const toAdd = keys.filter((k) => !existing.has(k));
  if (toAdd.length === 0) return text;
  const base = text.trim().length > 0 ? text.replace(/\s*$/, '') + '\n' : '';
  return base + toAdd.map((k) => `${k}=`).join('\n') + '\n';
};

interface Props {
  validation: ValidationResult | null;
  validating: boolean;
  envText: string;
  onEnvChange: (next: string) => void;
  /** Called when the user edits a value, so the .env tab can be enabled. */
  onEnvUsed?: () => void;
}

/**
 * Shared compose validation feedback: syntax/semantic errors with line numbers,
 * warnings, and inline inputs for missing or empty .env variables.
 */
export function ComposeValidationPanel({
  validation,
  validating,
  envText,
  onEnvChange,
  onEnvUsed,
}: Props) {
  if (validating) {
    return (
      <div className="mt-3 flex items-center gap-2 rounded-lg border border-[#2E2A27] bg-[#121110] p-3 text-xs text-stone-400">
        <Loader2 className="w-3.5 h-3.5 animate-spin text-orange-500" />
        Analyse du YAML…
      </div>
    );
  }
  if (!validation) return null;

  const missing = validation.missing_env ?? [];
  const empty = validation.empty_env ?? [];
  const envGapKeys = [...new Set([...missing, ...empty])];

  const values: Record<string, string> = {};
  envText.split('\n').forEach((line) => {
    const m = line.match(/^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$/);
    if (m) values[m[1]] = m[2].trim();
  });

  return (
    <div className="mt-3 space-y-2">
      {validation.valid ? (
        <div className="flex items-center gap-2 rounded-lg border border-emerald-500/30 bg-emerald-950/20 p-3 text-xs text-emerald-300">
          <CheckCircle2 className="w-4 h-4 shrink-0" />
          <span>
            Compose valide
            {validation.services.length > 0 && ` — service(s) : ${validation.services.join(', ')}`}
          </span>
        </div>
      ) : (
        <div className="rounded-lg border border-red-500/30 bg-red-950/25 p-3 text-xs text-red-300">
          <p className="font-medium mb-1.5 flex items-center gap-2">
            <AlertCircle className="w-4 h-4" /> Compose invalide — {validation.errors.length}{' '}
            erreur(s)
          </p>
          <ul className="space-y-1">
            {validation.errors.map((e, i) => (
              <li key={`err-${i}`} className="flex gap-2">
                <span className="font-mono text-[11px] text-red-400/80 shrink-0">
                  {e.line ? `ligne ${e.line}` : '—'}
                </span>
                <span>
                  {e.field && (
                    <code className="font-mono text-[11px] text-red-200/90">{e.field}</code>
                  )}
                  {e.field ? ' : ' : ''}
                  {e.message}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {validation.warnings.length > 0 && (
        <div className="rounded-lg border border-orange-500/30 bg-orange-950/20 p-3 text-xs text-orange-200">
          <p className="font-medium mb-1.5 flex items-center gap-2">
            <AlertCircle className="w-4 h-4" /> {validation.warnings.length} avertissement(s)
          </p>
          <ul className="space-y-1">
            {validation.warnings.map((w, i) => (
              <li key={`warn-${i}`} className="flex gap-2">
                <span className="font-mono text-[11px] text-orange-300/80 shrink-0">
                  {w.line ? `ligne ${w.line}` : '—'}
                </span>
                <span>
                  {w.field && (
                    <code className="font-mono text-[11px] text-orange-100/90">{w.field}</code>
                  )}
                  {w.field ? ' : ' : ''}
                  {w.message}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {envGapKeys.length > 0 && (
        <div className="rounded-lg border border-orange-500/30 bg-[#1C1A18]/70 p-3">
          <p className="text-xs font-medium text-orange-200 mb-2 flex items-center gap-2">
            <Lock className="w-4 h-4" />
            Variables requises par le compose ({envGapKeys.length})
          </p>
          <div className="space-y-2">
            {envGapKeys.map((key) => {
              const isMissing = missing.includes(key);
              return (
                <div key={key} className="flex items-center gap-2">
                  <code className="w-44 shrink-0 truncate font-mono text-[11px] text-orange-300">
                    {key}
                  </code>
                  <input
                    type={isSensitiveKey(key) ? 'password' : 'text'}
                    value={values[key] ?? ''}
                    onChange={(e) => {
                      onEnvUsed?.();
                      onEnvChange(setEnvValue(envText, key, e.target.value));
                    }}
                    placeholder="valeur à renseigner…"
                    className="flex-1 rounded-md border border-[#2E2A27] bg-[#121110] px-2 py-1 font-mono text-[11px] text-stone-100 placeholder-stone-600 focus:border-orange-500 focus:outline-none"
                  />
                  <span
                    className={`shrink-0 px-1.5 py-0.5 rounded text-[10px] ${
                      isMissing
                        ? 'bg-red-500/15 text-red-300'
                        : 'bg-orange-500/15 text-orange-300'
                    }`}
                  >
                    {isMissing ? 'absente' : 'valeur manquante'}
                  </span>
                </div>
              );
            })}
          </div>
          <p className="mt-2 text-[11px] text-stone-500">
            Les clés sont ajoutées automatiquement au <code className="font-mono">.env</code> ;
            renseignez leur valeur ci-dessus (elle est aussi éditable dans l'onglet .env).
          </p>
        </div>
      )}
    </div>
  );
}

/** Small counter badge for the .env tab. */
export function EnvGapBadge({ validation }: { validation: ValidationResult | null }) {
  const gaps = (validation?.missing_env?.length ?? 0) + (validation?.empty_env?.length ?? 0);
  if (gaps === 0) return null;
  return (
    <span
      title={`${gaps} variable(s) sans valeur`}
      className="mb-1 ml-1 inline-flex items-center gap-1 px-1.5 py-0.5 rounded-full text-[10px] bg-orange-500/20 text-orange-300"
    >
      <AlertCircle className="w-3 h-3" /> {gaps}
    </span>
  );
}
