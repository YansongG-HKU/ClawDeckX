import React, { useState, useEffect, useCallback } from 'react';
import { MiniDonut, MiniBarChart } from './MiniChart';

interface SessionInfo {
  model?: string;
  modelProvider?: string;
  totalTokens?: number;
  maxContextTokens?: number;
  compacted?: boolean;
  thinkingLevel?: string;
  messageCount?: number;
  lastLatencyMs?: number | null;
  liveElapsed?: number;
  runPhase?: string;
}

interface UsagePanelProps {
  sessionKey: string;
  gwReady: boolean;
  loadUsage: (key: string) => Promise<any>;
  labels: Record<string, string>;
  session?: SessionInfo;
}

const fmtTok = (n: number) => n >= 1000 ? `${(n / 1000).toFixed(1)}K` : String(n || 0);
const fmtCost = (n: number) => n >= 1 ? `$${n.toFixed(2)}` : n > 0 ? `$${n.toFixed(4)}` : '$0';

export const UsagePanel: React.FC<UsagePanelProps> = ({ sessionKey, gwReady, loadUsage, labels: a, session: s }) => {
  const [usage, setUsage] = useState<any>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [collapsed, setCollapsed] = useState(() => {
    try { return localStorage.getItem('usage-panel-collapsed') === '1'; } catch { return false; }
  });

  const load = useCallback(async () => {
    if (!gwReady || !sessionKey) return;
    setLoading(true);
    setError(null);
    try {
      const data = await loadUsage(sessionKey);
      setUsage(data);
    } catch (e: any) {
      setError(e?.message || 'Failed to load');
    }
    setLoading(false);
  }, [gwReady, sessionKey, loadUsage]);

  useEffect(() => { load(); }, [load]);

  const toggle = () => {
    setCollapsed(v => {
      try { localStorage.setItem('usage-panel-collapsed', v ? '0' : '1'); } catch {}
      return !v;
    });
  };

  if (collapsed) {
    return (
      <button onClick={toggle}
        className="w-8 h-full border-s border-slate-200/60 dark:border-white/[0.06] flex items-center justify-center
                   bg-white/50 dark:bg-white/[0.02] hover:bg-slate-50 dark:hover:bg-white/[0.04] transition">
        <span className="material-symbols-outlined text-[14px] text-slate-400">analytics</span>
      </button>
    );
  }

  const u = usage;
  const tokenSlices = u ? [
    { value: u.input || 0, color: '#3b82f6' },
    { value: u.output || 0, color: '#f59e0b' },
    ...(u.cacheRead ? [{ value: u.cacheRead, color: '#8b5cf6' }] : []),
  ].filter(d => d.value > 0) : [];

  const dailyData = u?.dailyBreakdown?.slice(-7) || [];

  // Context usage percentage
  const ctxPct = s?.totalTokens && s?.maxContextTokens
    ? Math.min(100, (s.totalTokens / s.maxContextTokens) * 100) : 0;
  const ctxClr = ctxPct > 90 ? 'bg-red-500' : ctxPct > 70 ? 'bg-amber-500' : 'bg-emerald-500';
  const ctxTxtClr = ctxPct > 90 ? 'text-red-500' : ctxPct > 70 ? 'text-amber-500' : 'text-emerald-500';

  return (
    <div className="w-56 shrink-0 border-s border-slate-200/60 dark:border-white/[0.06]
                    bg-white/50 dark:bg-white/[0.02] flex flex-col overflow-y-auto">
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-slate-100/60 dark:border-white/[0.04]">
        <span className="text-[10px] font-bold text-slate-500 dark:text-white/40 uppercase">{a.usage || 'Usage'}</span>
        <div className="flex items-center gap-1">
          <button onClick={load} disabled={loading} className="p-0.5 text-slate-400 hover:text-primary">
            <span className={`material-symbols-outlined text-[12px] ${loading ? 'animate-spin' : ''}`}>refresh</span>
          </button>
          <button onClick={toggle} className="p-0.5 text-slate-400 hover:text-primary">
            <span className="material-symbols-outlined text-[12px]">chevron_right</span>
          </button>
        </div>
      </div>

      <div className="p-3 space-y-3">
        {/* Session Info Section */}
        {s?.model && (
          <div>
            <div className="text-[9px] font-bold text-slate-400 dark:text-white/30 uppercase mb-1.5">{a.model || 'Model'}</div>
            <div className="px-2 py-1.5 rounded-lg bg-gradient-to-r from-purple-500/10 to-blue-500/10 border border-purple-500/10">
              <div className="text-[10px] font-bold text-purple-600 dark:text-purple-400 truncate">{s.model}</div>
              {s.modelProvider && <div className="text-[8px] text-slate-400 dark:text-white/25 mt-0.5">{s.modelProvider}</div>}
            </div>
          </div>
        )}

        {/* Context Window */}
        {s?.totalTokens ? (
          <div>
            <div className="text-[9px] font-bold text-slate-400 dark:text-white/30 uppercase mb-1.5">{a.context || 'Context'}</div>
            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <span className="text-[10px] font-mono tabular-nums text-slate-600 dark:text-white/60">{fmtTok(s.totalTokens)}</span>
                {s.maxContextTokens ? (
                  <span className="text-[9px] text-slate-400 dark:text-white/25">/ {fmtTok(s.maxContextTokens)}</span>
                ) : null}
              </div>
              {s.maxContextTokens ? (
                <div className="flex items-center gap-1.5">
                  <div className="flex-1 h-1.5 rounded-full bg-slate-200/60 dark:bg-white/10 overflow-hidden">
                    <div className={`h-full rounded-full ${ctxClr} transition-all`} style={{ width: `${ctxPct}%` }} />
                  </div>
                  <span className={`text-[9px] font-bold tabular-nums ${ctxTxtClr}`}>{ctxPct.toFixed(0)}%</span>
                  {s.compacted && <span className="material-symbols-outlined text-[11px] text-amber-500" title={a.ctxCompacted || 'Compacted'}>compress</span>}
                </div>
              ) : null}
            </div>
          </div>
        ) : null}

        {/* Quick Stats Row */}
        {(s?.messageCount || s?.thinkingLevel) ? (
          <div>
            <div className="text-[9px] font-bold text-slate-400 dark:text-white/30 uppercase mb-1.5">{a.session || 'Session'}</div>
            <div className="grid grid-cols-2 gap-x-2 gap-y-1 text-[9px]">
              {s?.messageCount ? (
                <div className="text-slate-500 dark:text-white/35">
                  <span className="material-symbols-outlined text-[10px] align-middle me-0.5">chat</span>
                  {s.messageCount} msg
                </div>
              ) : null}
              {s?.thinkingLevel ? (
                <div className="text-slate-500 dark:text-white/35">
                  <span className="material-symbols-outlined text-[10px] align-middle me-0.5">psychology</span>
                  {s.thinkingLevel}
                </div>
              ) : null}
              {(s?.runPhase === 'streaming' || s?.runPhase === 'sending') && s?.liveElapsed ? (
                <div className="text-primary font-mono tabular-nums">
                  <span className="material-symbols-outlined text-[10px] align-middle me-0.5">timer</span>
                  {(s.liveElapsed / 1000).toFixed(1)}s
                </div>
              ) : s?.lastLatencyMs && s?.runPhase === 'idle' ? (
                <div className="text-slate-500 dark:text-white/35 font-mono tabular-nums">
                  <span className="material-symbols-outlined text-[10px] align-middle me-0.5">speed</span>
                  {(s.lastLatencyMs / 1000).toFixed(1)}s
                </div>
              ) : null}
            </div>
          </div>
        ) : null}

        {/* Divider before usage data */}
        {(s?.model || s?.totalTokens) && (u || error) && (
          <div className="border-t border-slate-100/60 dark:border-white/[0.04]" />
        )}

        {/* Cost */}
        <div>
          <div className="text-[9px] font-bold text-slate-400 dark:text-white/30 uppercase mb-1">{a.cost || 'Cost'}</div>
          <div className="text-lg font-bold text-slate-700 dark:text-white/80 tabular-nums">{fmtCost(u?.totalCost || 0)}</div>
          {u && (
            <div className="text-[8px] text-slate-400 dark:text-white/25">
              {a.input || 'In'}: {fmtCost(u.inputCost || 0)} · {a.output || 'Out'}: {fmtCost(u.outputCost || 0)}
            </div>
          )}
        </div>

        {/* Token Donut */}
        {tokenSlices.length > 0 && (
          <div>
            <div className="text-[9px] font-bold text-slate-400 dark:text-white/30 uppercase mb-1">{a.tokens || 'Tokens'}</div>
            <div className="flex items-center gap-2">
              <MiniDonut size={56} slices={tokenSlices} innerRadius={0.6} />
              <div className="text-[9px] space-y-0.5">
                <div><span className="text-blue-500">●</span> {a.input || 'In'}: {fmtTok(u.input)}</div>
                <div><span className="text-amber-500">●</span> {a.output || 'Out'}: {fmtTok(u.output)}</div>
                {u.cacheRead > 0 && <div><span className="text-purple-500">●</span> {a.cache || 'Cache'}: {fmtTok(u.cacheRead)}</div>}
              </div>
            </div>
          </div>
        )}

        {/* Error state */}
        {error && (
          <div className="flex flex-col items-center gap-2 p-3 text-center rounded-lg bg-red-50/50 dark:bg-red-500/5">
            <span className="material-symbols-outlined text-[16px] text-red-400">error</span>
            <p className="text-[9px] text-red-400">{error}</p>
            <button onClick={load} className="text-[9px] px-2 py-1 rounded-lg bg-primary/10 text-primary hover:bg-primary/20 transition">
              {a.retry || 'Retry'}
            </button>
          </div>
        )}

        {/* Messages */}
        {u?.messageCounts && (
          <div>
            <div className="text-[9px] font-bold text-slate-400 dark:text-white/30 uppercase mb-1">{a.messages || 'Messages'}</div>
            <div className="grid grid-cols-2 gap-1 text-[9px]">
              <div className="text-slate-500 dark:text-white/35">{a.user || 'User'}: <b>{u.messageCounts.user}</b></div>
              <div className="text-slate-500 dark:text-white/35">{a.assistant || 'Asst'}: <b>{u.messageCounts.assistant}</b></div>
              <div className="text-slate-500 dark:text-white/35">{a.toolCall || 'Tools'}: <b>{u.messageCounts.toolCalls}</b></div>
              <div className="text-slate-500 dark:text-white/35">{a.error || 'Errors'}: <b>{u.messageCounts.errors}</b></div>
            </div>
          </div>
        )}

        {/* Latency */}
        {u?.latency && u.latency.count > 0 && (
          <div>
            <div className="text-[9px] font-bold text-slate-400 dark:text-white/30 uppercase mb-1">{a.latency || 'Latency'}</div>
            <div className="text-[9px] text-slate-500 dark:text-white/35 space-y-0.5">
              <div>Avg: <b>{(u.latency.sum / u.latency.count / 1000).toFixed(1)}s</b></div>
              <div>P95: <b>{(u.latency.p95Max / 1000).toFixed(1)}s</b></div>
            </div>
          </div>
        )}

        {/* Daily bar chart */}
        {dailyData.length > 0 && (
          <div>
            <div className="text-[9px] font-bold text-slate-400 dark:text-white/30 uppercase mb-1">{a.trend7d || '7-Day Trend'}</div>
            <MiniBarChart values={dailyData.map((d: any) => d.tokens || 0)} height={48} color="#3b82f6" />
          </div>
        )}

        {/* Model usage */}
        {u?.modelUsage?.length > 0 && (
          <div>
            <div className="text-[9px] font-bold text-slate-400 dark:text-white/30 uppercase mb-1">{a.model || 'Models'}</div>
            {u.modelUsage.slice(0, 3).map((m: any, i: number) => (
              <div key={i} className="text-[9px] text-slate-500 dark:text-white/35 truncate">{m.model}: {m.count}x</div>
            ))}
          </div>
        )}

        {/* Loading placeholder when no usage data yet */}
        {!u && !error && loading && (
          <div className="flex items-center justify-center py-4 text-[10px] text-slate-400 dark:text-white/20">
            {a.loading || 'Loading...'}
          </div>
        )}
      </div>
    </div>
  );
};
