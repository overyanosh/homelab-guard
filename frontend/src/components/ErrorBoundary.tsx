import { Component } from 'react';
import type { ErrorInfo, ReactNode } from 'react';

interface Props {
  children: ReactNode;
}

interface State {
  error: Error | null;
}

/**
 * Catches render errors so a single faulty panel never blanks the whole UI.
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('Render error:', error, info.componentStack);
  }

  render() {
    if (this.state.error) {
      return (
        <div className="min-h-screen bg-[#121110] text-stone-100 flex items-center justify-center p-6">
          <div className="w-full max-w-2xl rounded-xl border border-red-500/30 bg-[#1C1A18] p-6">
            <h1 className="text-lg font-semibold text-red-300 mb-2">
              Erreur d'affichage de l'interface
            </h1>
            <p className="text-sm text-stone-400 mb-4">
              Un composant a levé une exception. Les données du serveur ne sont pas affectées.
            </p>
            <pre className="rounded-lg bg-[#121110] p-3 font-mono text-xs text-red-300 whitespace-pre-wrap break-all max-h-48 overflow-auto">
              {this.state.error.message}
            </pre>
            <div className="mt-4 flex items-center gap-3">
              <button
                onClick={() => this.setState({ error: null })}
                className="px-3 py-1.5 rounded-md bg-[#2E2A27] hover:bg-[#3a3531] text-sm transition-colors"
              >
                Réessayer
              </button>
              <button
                onClick={() => window.location.reload()}
                className="px-3 py-1.5 rounded-md bg-orange-600 hover:bg-orange-500 text-white text-sm font-medium transition-colors"
              >
                Recharger la page
              </button>
            </div>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
