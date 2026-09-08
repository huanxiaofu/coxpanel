import { Alert, Button, Descriptions, Space, Table, Typography, message } from 'antd';
import { api } from '../api';
import { DataState, useP2Data } from '../components/P2Data';

export default function MailSettings() {
  const settings = useP2Data<Record<string, any>>('/api/settings/smtp', true);
  const queue = useP2Data<Array<Record<string, any>>>('/api/notifications?kind=smtp_test', true);
  const test = async () => { try { await api.request('/api/settings/smtp/test', 'POST', {}); message.success('测试已入队，仅发送到当前 owner 的账号邮箱'); void queue.load(); } catch (error) { message.error(error instanceof Error ? error.message : '发送失败'); } };
  return <Space orientation="vertical" size="large" style={{ width: '100%' }}><Typography.Title level={2}>邮件设置</Typography.Title><Alert showIcon type="info" title="环境配置只读，587 STARTTLS" description="浏览器不保存或修改 SMTP 密码。sent 表示邮局已接收，待确认收件，不代表进入收件箱。" /><DataState {...settings} retry={settings.load} />{settings.data && <Descriptions bordered items={['host', 'port', 'security', 'fromMasked', 'configured', 'passwordConfigured', 'emailVerificationRequired'].map(key => ({ key, label: key, children: String(settings.data?.[key] ?? '未设置') }))} />}<Button type="primary" disabled={!settings.data?.configured} onClick={() => void test()}>确认发送测试邮件到本人邮箱</Button><DataState {...queue} retry={queue.load} /><Table rowKey="id" dataSource={queue.data || []} columns={[{ title: 'ID', dataIndex: 'id' }, { title: '状态', dataIndex: 'state' }, { title: '错误类别', dataIndex: 'errorCode' }, { title: '时间 UTC', dataIndex: 'createdAt' }]} /></Space>;
}
