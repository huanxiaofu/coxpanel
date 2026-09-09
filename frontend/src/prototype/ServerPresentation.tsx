import { Progress, Tag } from 'antd';
import { Icon } from './Icon';
import { formatBytes, nextTrafficReset, serverStatusLabels, trafficCycleLabel } from './model';
import type { ServerAsset, ServerStatus, ServerTraffic } from './model';

export function ServerStatusBadge({ status }: { status: ServerStatus }) {
  return <span className={`su-server-status is-${status}`}><Icon name={status === 'online' ? 'check' : status === 'maintenance' ? 'pause' : 'offline'} size={13} />{serverStatusLabels[status]}</span>;
}

export function ServerTags({ tags }: { tags: string[] }) {
  const colors = ['blue', 'cyan', 'purple', 'geekblue', 'gold', 'green'];
  return <div className="su-server-tags">{tags.length ? tags.map(tag => {
    const hash = Array.from(tag).reduce((value, character) => value + (character.codePointAt(0) ?? 0), 0);
    return <Tag color={colors[hash % colors.length]} key={tag}>{tag}</Tag>;
  }) : <span className="su-muted">暂无标签</span>}</div>;
}

export function ServerResources({ server }: { server: ServerAsset }) {
  const resources = server.resources;
  if (!resources) return <div className="su-server-resources"><p className="su-muted">CPU / 内存 / 负载：未上报</p><small className="su-muted">离线资源不记为 0，也不计入资源均值。</small></div>;
  const memoryPercent = resources.memTotalMb > 0 ? resources.memUsedMb / resources.memTotalMb * 100 : 0;
  return <div className="su-server-resources">
    <div className="su-resource-row"><span>CPU <strong>{resources.cpuPercent}%</strong></span><Progress aria-label={`${server.name} CPU 使用率`} percent={resources.cpuPercent} showInfo={false} size="small" strokeColor={resources.cpuPercent >= 80 ? 'var(--su-amber)' : 'var(--su-blue)'} /></div>
    <div className="su-resource-row"><span>内存 <strong>{formatBytes(resources.memUsedMb * 1024 ** 2)} / {formatBytes(resources.memTotalMb * 1024 ** 2)}</strong></span><Progress aria-label={`${server.name} 内存使用率`} percent={Math.round(memoryPercent)} showInfo={false} size="small" strokeColor={memoryPercent >= 80 ? 'var(--su-amber)' : 'var(--su-blue)'} /></div>
    <div className="su-resource-meta"><span>负载（1 min） <strong>{resources.load.toFixed(2)}</strong></span><small>{server.status === 'online' ? '合成资源快照 · 非实时' : '最后快照 · 非实时'}</small></div>
  </div>;
}

export function ServerTrafficSummary({ traffic, now = new Date() }: { traffic: ServerTraffic; now?: Date }) {
  const reset = nextTrafficReset(traffic, now);
  const validReset = Number.isFinite(reset.getTime());
  const minutes = validReset ? Math.max(1, Math.ceil((reset.getTime() - now.getTime()) / 60_000)) : 0;
  const countdown = minutes >= 1440 ? `${Math.floor(minutes / 1440)} 天 ${Math.floor(minutes % 1440 / 60)} 小时` : minutes >= 60 ? `${Math.floor(minutes / 60)} 小时 ${minutes % 60} 分钟` : `${minutes} 分钟`;
  const percent = traffic.limitBytes > 0 ? traffic.usedBytes / traffic.limitBytes * 100 : 0;
  return <div className="su-server-traffic">
    <div className="su-traffic-heading"><span>周期流量</span><strong>{formatBytes(traffic.usedBytes)} <small>/ {traffic.limitBytes ? formatBytes(traffic.limitBytes) : '不限额'}</small></strong></div>
    {traffic.limitBytes > 0 ? <><Progress aria-label="流量使用率" percent={Math.min(100, Math.round(percent))} showInfo={false} size="small" strokeColor={percent >= 100 ? 'var(--su-red)' : percent >= 80 ? 'var(--su-amber)' : 'var(--su-blue)'} /><div className="su-traffic-meta"><span>{percent.toFixed(1)}% 已用</span><span>{percent >= 100 ? '已达限额 · 仅提示，不阻断' : `剩余 ${formatBytes(traffic.limitBytes - traffic.usedBytes)}`}</span></div></> : <p className="su-traffic-meta">不设上限 · 仍记录已用流量</p>}
    <div className="su-traffic-meta"><span>{trafficCycleLabel(traffic)}刷新</span><span>{traffic.statsMode === 'total' ? '入站 + 出站合计' : '仅出站'}</span></div>
    <div className="su-traffic-reset"><Icon name="reset" size={12} /><span>下次 {validReset ? reset.toISOString().slice(0, 16).replace('T', ' ') : '—'} UTC<small>{validReset ? `还有 ${countdown}` : '请检查刷新配置'} · 仅预览，不自动清零</small></span></div>
  </div>;
}
