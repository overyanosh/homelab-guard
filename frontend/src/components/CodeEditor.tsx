import { useRef } from 'react';

interface Props {
  value: string;
  onChange: (value: string) => void;
  /** 1-based line numbers to highlight (e.g. validation errors). */
  errorLines?: number[];
  placeholder?: string;
  height?: string;
  /** Colour class for the text (monospace). */
  tone?: 'yaml' | 'env';
}

/**
 * Minimal code editor with a synchronised line-number gutter. Validation errors
 * are highlighted in the gutter so the reported line can be found instantly.
 */
export function CodeEditor({
  value,
  onChange,
  errorLines = [],
  placeholder,
  height = '45vh',
  tone = 'yaml',
}: Props) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const gutterRef = useRef<HTMLDivElement>(null);

  const lines = value.split('\n');
  const errors = new Set(errorLines);

  const syncScroll = () => {
    if (textareaRef.current && gutterRef.current) {
      gutterRef.current.scrollTop = textareaRef.current.scrollTop;
    }
  };

  return (
    <div
      className="flex overflow-hidden rounded-lg border border-[#2E2A27] bg-[#121110] focus-within:border-orange-500"
      style={{ height }}
    >
      <div
        ref={gutterRef}
        aria-hidden
        className="w-12 shrink-0 select-none overflow-hidden border-r border-[#2E2A27] bg-[#161412] py-4 pr-2 text-right"
      >
        {lines.map((_, i) => (
          <div
            key={i}
            className={`font-mono text-xs leading-5 ${
              errors.has(i + 1) ? 'font-bold text-red-400' : 'text-stone-600'
            }`}
          >
            {i + 1}
          </div>
        ))}
      </div>
      <textarea
        ref={textareaRef}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onScroll={syncScroll}
        wrap="off"
        spellCheck={false}
        placeholder={placeholder}
        className={`flex-1 resize-none overflow-auto whitespace-pre bg-transparent p-4 font-mono text-xs leading-5 focus:outline-none ${
          tone === 'env' ? 'text-orange-200' : 'text-emerald-300'
        }`}
      />
    </div>
  );
}
