import React from 'react';
import { useGatewayStatus } from '../hooks/useGatewayStatus';
import type { Language } from '../types';
import { getTranslation } from '../locales';

// ---------------------------------------------------------------------------
// i18n keys — reuse existing Scheduler strings where possible, with fallbacks.
// ---------------------------------------------------------------------------

function getStrings(language: Language) {
  const t = getTranslation(language) as any;
  const s = t?.s ?? {};
  return {
    title: s.gwNotReady || 'Gateway Disconnected',
    desc: s.gwNotReadyDesc || 'Cannot connect to OpenClaw Gateway. Please check if the gateway is running.',
    retry: s.retry || t?.cm?.retry || 'Retry',
  };
}

// ---------------------------------------------------------------------------
// Skeleton placeholder (matches Scheduler's loading skeleton)
// ---------------------------------------------------------------------------

const GatewaySkeleton: React.FC = () => (
  <main className="flex-1 overflow-y-auto p-4 md:p-5 custom-scrollbar neon-scrollbar bg-slate-50/50 dark:bg-transparent">
    <div className="space-y-4 max-w-6xl animate-pulse">
      <div className="h-8 w-48 bg-slate-200 dark:bg-white/5 rounded-lg" />
      <div className="grid grid-cols-3 gap-3">
        {[1, 2, 3].map(i => (
          <div key={i} className="h-24 bg-slate-200 dark:bg-white/5 rounded-xl" />
        ))}
      </div>
      <div className="h-64 bg-slate-200 dark:bg-white/5 rounded-xl" />
    </div>
  </main>
);

// ---------------------------------------------------------------------------
// GatewayGuard — wraps a page that requires gateway connectivity.
//
// Usage:
//   <GatewayGuard language={language}>
//     <MyPage ... />
//   </GatewayGuard>
//
// Or with render prop for pages that need the status object:
//   <GatewayGuard language={language}>
//     {(gw) => <MyPage gwReady={gw.ready} ... />}
//   </GatewayGuard>
// ---------------------------------------------------------------------------

export interface GatewayGuardProps {
  language: Language;
  children: React.ReactNode | ((status: ReturnType<typeof useGatewayStatus>) => React.ReactNode);
  /** Custom skeleton to show while first check is in progress */
  skeleton?: React.ReactNode;
  /** If true, render children even when gateway is offline (children can inspect status themselves) */
  passthrough?: boolean;
}

const GatewayGuard: React.FC<GatewayGuardProps> = ({ language, children, skeleton, passthrough }) => {
  const gw = useGatewayStatus();
  const str = getStrings(language);

  // Still checking — show skeleton
  if (!gw.checked) {
    return <>{skeleton ?? <GatewaySkeleton />}</>;
  }

  // Gateway not ready — show offline banner (unless passthrough)
  if (!gw.ready && !passthrough) {
    return (
      <main className="flex-1 overflow-y-auto p-4 md:p-5 custom-scrollbar neon-scrollbar bg-slate-50/50 dark:bg-transparent">
        <div className="flex flex-col items-center justify-center py-20 text-slate-400 dark:text-white/30">
          <span className="material-symbols-outlined text-[48px] mb-4 text-mac-yellow">cloud_off</span>
          <p className="text-sm font-bold mb-1">{str.title}</p>
          <p className="text-[11px] text-center mb-4">{str.desc}</p>
          <button
            onClick={gw.refresh}
            className="px-4 py-1.5 rounded-lg bg-primary text-white text-[11px] font-bold hover:opacity-90 transition-opacity"
          >
            {str.retry}
          </button>
        </div>
      </main>
    );
  }

  // Gateway ready — render children
  if (typeof children === 'function') {
    return <>{children(gw)}</>;
  }
  return <>{children}</>;
};

export default GatewayGuard;
export { GatewaySkeleton };
