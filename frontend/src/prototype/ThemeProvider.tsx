import { createContext, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { App, ConfigProvider, theme } from 'antd';
import zhCN from 'antd/locale/zh_CN';

type ThemePreference = 'system' | 'light' | 'dark';
const ThemeContext = createContext<{ dark: boolean; preference: ThemePreference; setPreference: (value: ThemePreference) => void } | null>(null);

function readPreference(): ThemePreference {
  try {
    const saved = localStorage.getItem('sing-ui-theme');
    return saved === 'dark' || saved === 'light' ? saved : 'system';
  } catch {
    return 'system';
  }
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreference] = useState<ThemePreference>(readPreference);
  const [systemDark, setSystemDark] = useState(() => window.matchMedia('(prefers-color-scheme: dark)').matches);
  const dark = preference === 'system' ? systemDark : preference === 'dark';
  useEffect(() => {
    const query = window.matchMedia('(prefers-color-scheme: dark)');
    const update = () => setSystemDark(query.matches);
    query.addEventListener('change', update);
    return () => query.removeEventListener('change', update);
  }, []);
  useEffect(() => {
    document.documentElement.dataset.suTheme = dark ? 'dark' : 'light';
    try { localStorage.setItem('sing-ui-theme', preference); } catch { return; }
  }, [dark, preference]);
  const value = useMemo(() => ({ dark, preference, setPreference }), [dark, preference]);
  return <ThemeContext.Provider value={value}>
    <ConfigProvider locale={zhCN} theme={{
      algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
      token: {
        colorPrimary: dark ? '#6b9eff' : '#0958d9', fontSize: 14, borderRadius: 8,
        fontFamily: 'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
        colorBgLayout: dark ? '#1a1b1f' : '#f5f6f8',
        colorBgContainer: dark ? '#23252b' : '#ffffff',
        colorBgElevated: dark ? '#2d2f37' : '#ffffff',
      },
    }}><App>{children}</App></ConfigProvider>
  </ThemeContext.Provider>;
}

export function useTheme() {
  const context = useContext(ThemeContext);
  if (!context) throw new Error('ThemeProvider is required');
  return context;
}
