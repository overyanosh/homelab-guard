import { Shield, LogIn } from 'lucide-react';

/**
 * Login gate shown only when the server reports OIDC enabled and the visitor is
 * not authenticated. The button starts the standard OIDC authorization-code
 * flow handled by the backend (/auth/login).
 */
export function LoginPage({ error }: { error?: string | null }) {
  return (
    <div className="min-h-screen bg-[#121110] text-stone-100 flex items-center justify-center p-6">
      <div className="w-full max-w-sm rounded-2xl border border-[#2E2A27] bg-[#1C1A18] p-8 shadow-2xl">
        <div className="flex items-center justify-center gap-2.5 mb-6">
          <Shield className="w-8 h-8 text-orange-500" />
          <span className="font-bold tracking-tight text-xl">
            HOMELAB<span className="text-orange-500"> GUARD</span>
          </span>
        </div>

        <h1 className="text-center text-lg font-semibold mb-1">Connexion requise</h1>
        <p className="text-center text-sm text-stone-400 mb-6">
          Authentifiez-vous avec votre fournisseur d'identité pour accéder à la console.
        </p>

        {error && (
          <div className="mb-5 rounded-lg border border-red-500/30 bg-red-950/25 p-3 text-xs text-red-300">
            {error}
          </div>
        )}

        <a
          href="/auth/login"
          className="flex w-full items-center justify-center gap-2 rounded-lg bg-orange-600 hover:bg-orange-500 px-4 py-2.5 text-sm font-medium text-white transition-colors"
        >
          <LogIn className="w-4 h-4" /> Se connecter avec OIDC
        </a>

        <p className="mt-6 text-center text-[11px] text-stone-600">
          Accès restreint — la session est signée et expire automatiquement.
        </p>
      </div>
    </div>
  );
}
