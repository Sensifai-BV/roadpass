import * as DM from "@radix-ui/react-dropdown-menu";
import { Download as DownloadIcon, FileSpreadsheet, FileText, FileType2, Braces, Sheet } from "lucide-react";
import { useRange } from "../lib/hooks";

const FORMATS = [
  { f: "pdf", label: "PDF", sub: "Formatted, ready to send", icon: FileText },
  { f: "docx", label: "Word", sub: "Editable document", icon: FileType2 },
  { f: "xlsx", label: "Excel", sub: "Every table on its own sheet", icon: FileSpreadsheet },
  { f: "csv", label: "CSV", sub: "The main table", icon: Sheet },
  { f: "json", label: "JSON", sub: "For other systems", icon: Braces },
] as const;

/**
 * Downloads a server-rendered report (/api/v1/reports/{kind}) in any format,
 * over the page's time window unless `range` is false.
 */
export function Download({ kind, params = {}, range = true, label = "Download" }: {
  kind: string; params?: Record<string, string | number | undefined>; range?: boolean; label?: string;
}) {
  const r = useRange();
  const href = (format: string) => {
    const q = new URLSearchParams({ format });
    if (range) { q.set("from", r.from); q.set("to", r.to); }
    for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== "") q.set(k, String(v));
    return `/api/v1/reports/${encodeURIComponent(kind)}?${q}`;
  };
  return (
    <DM.Root>
      <DM.Trigger className="btn" aria-label={/report|register/i.test(label) ? label : `${label} report`}><DownloadIcon size={14} /> <span className="hidden sm:inline">{label}</span></DM.Trigger>
      <DM.Portal><DM.Content align="end" sideOffset={6} className="z-50 w-[230px] rounded-xl bg-[var(--bg-elevated)] p-1 shadow-[var(--shadow-pop)]">
        {FORMATS.map(({ f, label: l, sub, icon: I }) => (
          <DM.Item key={f} asChild>
            <a href={href(f)} download className="flex cursor-pointer items-center gap-2.5 rounded-lg px-3 py-2 outline-none data-[highlighted]:bg-surface-3">
              <I size={15} className="shrink-0 text-text-2" aria-hidden />
              <span className="flex flex-col"><span className="text-[13px] font-semibold">{l}</span><span className="text-[11.5px] text-muted">{sub}</span></span>
            </a>
          </DM.Item>
        ))}
      </DM.Content></DM.Portal>
    </DM.Root>
  );
}
