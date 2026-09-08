import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Space, Spin } from 'antd';
import { api } from '../api';

export function useP2Data<T>(path: string, refresh = false) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const sequence = useRef(0);
  const load = useCallback(async () => {
    const current = ++sequence.current;
    setLoading(true);
    try {
      const result = await api.request(path);
      if (current === sequence.current) { setData(result); setError(''); }
    } catch (reason) {
      if (current === sequence.current) setError(reason instanceof Error ? reason.message : '读取失败');
    } finally { if (current === sequence.current) setLoading(false); }
  }, [path]);
  useEffect(() => {
    void load();
    const visible = () => { if (!document.hidden) void load(); };
    const timer = refresh ? window.setInterval(visible, 60000) : undefined;
    if (refresh) document.addEventListener('visibilitychange', visible);
    return () => { sequence.current++; clearInterval(timer); document.removeEventListener('visibilitychange', visible); };
  }, [load, refresh]);
  return { data, error, loading, load };
}

export function DataState({ loading, error, retry }: { loading: boolean; error: string; retry: () => unknown }) {
  return <Space orientation="vertical" style={{ width: '100%' }}>{loading && <Spin tip="读取中" size="small"><span aria-live="polite">正在读取数据…</span></Spin>}{error && <Alert type="error" showIcon title="数据读取失败，保留已有内容" description={error} action={<Button onClick={() => void retry()}>重试</Button>} />}</Space>;
}
