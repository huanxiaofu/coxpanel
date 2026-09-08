import { useEffect, useRef, useState } from 'react';
import { Alert, Card, Empty, InputNumber, Select, Space, Table, Typography } from 'antd';
import { isAdminRole, useAuth } from '../auth/AuthContext';
import { DataState, useP2Data } from '../components/P2Data';

interface Point { at: string; upBytes: string; downBytes: string; complete: boolean }
interface TrafficData {
  grain: string; summary: { upBytes: string; downBytes: string; totalBytes: string };
  series: Array<{ key: string; points: Point[] }>;
  quality: { status: string; estimated: boolean; availableFrom: string | null; lastReceivedAt: string | null };
  quota?: { epoch: number; limitBytes: string; usedBytes: string; unlimited: boolean; periodStart: string | null };
}

function TrafficChart({ data }: { data: TrafficData }) {
  const container = useRef<HTMLDivElement>(null);
  const instance = useRef<import('echarts').ECharts | null>(null);
  useEffect(() => {
    let cancelled = false;
    let observer: ResizeObserver | undefined;
    void import('echarts/core').then(async (core) => {
      const [{ LineChart }, { GridComponent, TooltipComponent, LegendComponent, DataZoomComponent }, { CanvasRenderer }] = await Promise.all([import('echarts/charts'), import('echarts/components'), import('echarts/renderers')]);
      if (cancelled || !container.current) return;
      core.use([LineChart, GridComponent, TooltipComponent, LegendComponent, DataZoomComponent, CanvasRenderer]);
      instance.current = core.init(container.current);
      update();
      observer = new ResizeObserver(() => instance.current?.resize());
      observer.observe(container.current);
    });
    const update = () => { instance.current?.setOption({ animation: false, tooltip: { trigger: 'axis' }, legend: {}, grid: { left: 65, right: 20, bottom: 75 }, xAxis: { type: 'time' }, yAxis: { type: 'value', name: 'MiB / 桶' }, dataZoom: [{ type: 'inside' }, { type: 'slider' }], series: data.series.slice(0, 12).flatMap(series => ['upBytes', 'downBytes'].map(direction => ({ name: `${series.key} ${direction === 'upBytes' ? '上传' : '下载'}`, type: 'line', showSymbol: false, connectNulls: false, data: series.points.map(point => [point.at, Number(point[direction as 'upBytes' | 'downBytes']) / 1048576]) }))) }); };
    return () => { cancelled = true; observer?.disconnect(); instance.current?.dispose(); instance.current = null; };
  }, []);
  useEffect(() => {
    instance.current?.setOption({ series: data.series.slice(0, 12).flatMap(series => ['upBytes', 'downBytes'].map(direction => ({ name: `${series.key} ${direction === 'upBytes' ? '上传' : '下载'}`, type: 'line', showSymbol: false, connectNulls: false, data: series.points.map(point => [point.at, Number(point[direction as 'upBytes' | 'downBytes']) / 1048576]) }))) }, { replaceMerge: ['series'] });
  }, [data]);
  return <div ref={container} role="img" aria-label="真实采集流量趋势，下方表格提供精确字节数" style={{ height: 340, minWidth: 0, width: '100%' }} />;
}

export default function Traffic() {
  const { user } = useAuth();
  const [view, setView] = useState('my');
  const [subject, setSubject] = useState(1);
  const [days, setDays] = useState(1);
  const [grain, setGrain] = useState('auto');
  const [range, setRange] = useState(() => ({ from: new Date(Date.now() - 86400000).toISOString(), to: new Date().toISOString() }));
  const path = view === 'my' ? '/api/my/traffic' : view === 'node' ? `/api/nodes/${subject}/traffic` : `/api/traffic/${subject}`;
  const query = useP2Data<TrafficData>(`${path}?from=${range.from}&to=${range.to}&grain=${grain}`, true);
  const changeDays = (value: number) => { setDays(value); setRange({ from: new Date(Date.now() - value * 86400000).toISOString(), to: new Date().toISOString() }); };
  const points = query.data?.series.flatMap(series => series.points.map(point => ({ ...point, series: series.key, key: `${series.key}:${point.at}` }))) || [];
  return <Space orientation="vertical" size="large" style={{ width: '100%' }}><Typography.Title level={2}>流量统计</Typography.Title><Space wrap>
    {isAdminRole(user?.role) && <><Select aria-label="统计对象" value={view} onChange={setView} options={[{ value: 'my', label: '本人入口用量' }, { value: 'user', label: '指定用户入口' }, { value: 'node', label: '节点全部入站吞吐' }]} />{view !== 'my' && <InputNumber aria-label="用户或节点 ID" value={subject} min={1} onChange={value => setSubject(value || 1)} />}</>}
    <Select aria-label="日期范围" value={days} onChange={changeDays} options={[{ value: 1, label: '近24小时 UTC' }, { value: 7, label: '近7天 UTC' }, { value: 30, label: '近30天 UTC' }, { value: 180, label: '近180天 UTC' }]} /><Select aria-label="聚合粒度" value={grain} onChange={setGrain} options={['auto', '5m', '1h', '1d'].map(value => ({ value, label: value }))} />
  </Space><DataState {...query} retry={query.load} />{query.data && <><Alert showIcon type={query.data.quality.status === 'complete' ? 'success' : 'warning'} title={`数据质量：${query.data.quality.status}${query.data.quality.estimated ? '（含跨桶估算）' : ''}`} description={`最后收到：${query.data.quality.lastReceivedAt || '尚无真实采样'}。节点吞吐与用户入口用量分别计量，不能相加。缺口不代表零流量。`} />
    <Space wrap><Card title="范围内已知用量"><strong>{query.data.summary.totalBytes} bytes</strong></Card>{query.data.quota && <Card title={`当前配额期 #${query.data.quota.epoch}`}><strong>{query.data.quota.usedBytes} / {query.data.quota.unlimited ? '不限' : `${query.data.quota.limitBytes} bytes`}</strong><div>起始：{query.data.quota.periodStart || '计量尚未启用'}</div></Card>}</Space>
    {points.length ? <Card><TrafficChart data={query.data} /></Card> : <Empty description="当前范围没有可信流量记录，不填充假数据" />}
    <Table rowKey="key" dataSource={points} scroll={{ x: 600 }} columns={[{ title: '系列', dataIndex: 'series' }, { title: 'UTC 桶起点', dataIndex: 'at' }, { title: '上传 bytes', dataIndex: 'upBytes' }, { title: '下载 bytes', dataIndex: 'downBytes' }, { title: '完整', dataIndex: 'complete', render: (value: boolean) => value ? '是' : '估计 / 缺口' }]} />
  </>}</Space>;
}
