/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useState } from 'react';
import {
  Banner,
  Button,
  Card,
  Descriptions,
  Input,
  Modal,
  Space,
  Spin,
  Switch,
  Table,
  Tag,
  TextArea,
  Tooltip,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh, IconUpload } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import {
  API,
  showError,
  showSuccess,
  showWarning,
  timestamp2string,
} from '../../helpers';
import TaskPluginIcon from './TaskPluginIcon';
import {
  encodePluginIconBytes,
  findRuntimeFailure,
  localizedText,
  MAX_PLUGIN_SOURCE_BYTES,
} from './marketplace';

const { Text } = Typography;

const pluginPath = (key) => `/api/plugin/task/${encodeURIComponent(key)}`;

// 启用插件或删除在用版本时，后端在仍有渠道绑定/任务进行中会返回 success=false 并附带占用情况
const usageOf = (res) => {
  const data = res.data?.data;
  return data && Array.isArray(data.channels) ? data : null;
};

const TaskPluginInstalled = ({ refreshKey, onChanged }) => {
  const { t, i18n } = useTranslation();
  const [plugins, setPlugins] = useState([]);
  const [loading, setLoading] = useState(false);
  const [masterEnabled, setMasterEnabled] = useState(null);
  const [masterSaving, setMasterSaving] = useState(false);
  const [busyKey, setBusyKey] = useState('');
  const [versionsKey, setVersionsKey] = useState('');
  const [versions, setVersions] = useState([]);
  const [versionsLoading, setVersionsLoading] = useState(false);
  const [detail, setDetail] = useState(null);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [upload, setUpload] = useState({
    source: '',
    remark: '',
    icon: '',
    enabled: true,
  });
  const [uploading, setUploading] = useState(false);

  const loadPlugins = async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/plugin/task');
      if (!res.data.success) {
        showError(res.data.message);
        return [];
      }
      const list = res.data.data || [];
      setPlugins(list);
      return list;
    } finally {
      setLoading(false);
    }
  };

  const loadMasterSwitch = async () => {
    const res = await API.get('/api/option/');
    if (res.data.success) {
      const option = (res.data.data || []).find(
        (item) => item.key === 'TaskPluginEnabled',
      );
      setMasterEnabled(option ? option.value === 'true' : true);
    }
  };

  useEffect(() => {
    loadPlugins();
  }, [refreshKey]);

  useEffect(() => {
    loadMasterSwitch();
  }, []);

  const afterChange = async () => {
    const list = await loadPlugins();
    onChanged?.();
    return list;
  };

  const saveMasterSwitch = async (enabled) => {
    setMasterSaving(true);
    try {
      const res = await API.put('/api/option/', {
        key: 'TaskPluginEnabled',
        value: String(enabled),
      });
      if (res.data.success) {
        setMasterEnabled(enabled);
        showSuccess(t('保存成功'));
      } else {
        showError(res.data.message);
      }
    } finally {
      setMasterSaving(false);
    }
  };

  const setPluginStatus = async (item, enabled, params) => {
    setBusyKey(item.meta.key);
    try {
      const res = await API.post(
        `${pluginPath(item.meta.key)}/status`,
        { enabled },
        { params },
      );
      if (res.data.success) {
        const disabledChannels = res.data.data?.disabled_channels || 0;
        if (disabledChannels > 0) {
          showWarning(
            t('已停用插件，并禁用了 {{count}} 个绑定渠道', {
              count: disabledChannels,
            }),
          );
        } else {
          showSuccess(enabled ? t('已启用') : t('已停用'));
        }
        await afterChange();
        return;
      }
      const usage = usageOf(res);
      if (!enabled && usage && !params) {
        Modal.confirm({
          title: t('插件仍在使用中'),
          content: t(
            '有 {{channels}} 个渠道绑定了该插件，{{inflight}} 个任务仍在进行中。继续停用会同时禁用这些渠道，进行中的任务在插件重新启用前不会继续轮询。',
            {
              channels: usage.channels.length,
              inflight: usage.in_flight_count,
            },
          ),
          okText: t('仍然停用'),
          okButtonProps: { type: 'danger' },
          onOk: () =>
            setPluginStatus(item, false, { cascade: true, force: true }),
        });
        return;
      }
      showError(res.data.message);
    } finally {
      setBusyKey('');
    }
  };

  const loadVersions = async (key) => {
    setVersionsLoading(true);
    try {
      const res = await API.get(`${pluginPath(key)}/versions`);
      if (res.data.success) {
        setVersions(res.data.data || []);
      } else {
        showError(res.data.message);
      }
    } finally {
      setVersionsLoading(false);
    }
  };

  const openVersions = (key) => {
    setVersionsKey(key);
    setVersions([]);
    loadVersions(key);
  };

  const activateVersion = async (record) => {
    const res = await API.post(`${pluginPath(record.key)}/activate`, {
      version: record.version,
    });
    if (!res.data.success) {
      showError(res.data.message);
      return;
    }
    showSuccess(t('已切换到 v{{version}}', { version: record.version }));
    await loadVersions(record.key);
    await afterChange();
  };

  const deleteVersion = async (record, force = false) => {
    const res = await API.delete(
      `${pluginPath(record.key)}/versions/${encodeURIComponent(record.version)}`,
      { params: force ? { force: true } : undefined },
    );
    if (res.data.success) {
      showSuccess(t('删除成功'));
      await loadVersions(record.key);
      await afterChange();
      return;
    }
    const usage = usageOf(res);
    if (usage && !force) {
      Modal.confirm({
        title: t('插件仍在使用中'),
        content: t(
          '有 {{channels}} 个渠道绑定了该插件，{{inflight}} 个任务仍在进行中。删除当前版本后这些渠道将无法提交请求，确定继续？',
          {
            channels: usage.channels.length,
            inflight: usage.in_flight_count,
          },
        ),
        okText: t('仍然删除'),
        okButtonProps: { type: 'danger' },
        onOk: () => deleteVersion(record, true),
      });
      return;
    }
    showError(res.data.message);
  };

  const openDetail = async (key) => {
    const res = await API.get(pluginPath(key));
    if (res.data.success) {
      setDetail(res.data.data);
    } else {
      showError(res.data.message);
    }
  };

  const readSourceFile = async (file) => {
    if (!file) return;
    if (file.size > MAX_PLUGIN_SOURCE_BYTES) {
      showError(t('插件源码超过 8 MiB 上限'));
      return;
    }
    const text = await file.text();
    setUpload((prev) => ({ ...prev, source: text }));
  };

  const readIconFile = async (file) => {
    if (!file) return;
    const icon = encodePluginIconBytes(
      file.name,
      new Uint8Array(await file.arrayBuffer()),
    );
    if (!icon) {
      showError(t('图标仅支持 512 KiB 以内的 SVG 或 PNG 文件'));
      return;
    }
    setUpload((prev) => ({ ...prev, icon }));
  };

  const submitUpload = async () => {
    if (!upload.source.trim()) {
      showError(t('请填写插件源码'));
      return;
    }
    setUploading(true);
    try {
      const res = await API.post('/api/plugin/task', {
        source: upload.source,
        remark: upload.remark,
        enabled: upload.enabled,
        icon: upload.icon || undefined,
      });
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      const uploaded = res.data.data;
      // 同一插件已有生效版本时新版本默认不生效，启用上传时一并切换过去
      if (upload.enabled && uploaded.plugin && !uploaded.plugin.active) {
        const activateRes = await API.post(
          `${pluginPath(uploaded.meta.key)}/activate`,
          { version: uploaded.meta.version },
        );
        if (!activateRes.data.success) {
          showError(activateRes.data.message);
        }
      }
      setUploadOpen(false);
      setUpload({ source: '', remark: '', icon: '', enabled: true });
      const list = await afterChange();
      const failure = findRuntimeFailure(list, uploaded.meta.key);
      if (failure) {
        showError(t('插件已安装，但未能加载：{{error}}', { error: failure }));
        return;
      }
      showSuccess(
        t('已上传 {{name}} v{{version}}', {
          name: uploaded.meta.name,
          version: uploaded.meta.version,
        }),
      );
    } finally {
      setUploading(false);
    }
  };

  const renderSource = (item) => {
    if (item.source === 'factory') return <Tag color='blue'>{t('内置')}</Tag>;
    if (item.source === 'override_over_factory') {
      return (
        <Tooltip
          content={t('内置版本 v{{version}}', {
            version: item.factory_meta?.version,
          })}
        >
          <Tag color='purple'>{t('覆盖内置')}</Tag>
        </Tooltip>
      );
    }
    return <Tag color='cyan'>{t('已安装')}</Tag>;
  };

  const renderRuntime = (item) => {
    switch (item.runtime_status) {
      case 'registered':
        return <Tag color='green'>{t('运行中')}</Tag>;
      case 'disabled':
        return <Tag color='grey'>{t('已停用')}</Tag>;
      case 'disabled_fallback':
        // 后端对所有停用的已安装版本都报 disabled_fallback，只有存在内置版本时才真的有人接管
        return item.source === 'override_over_factory' ? (
          <Tag color='orange'>{t('已停用，由内置版本接管')}</Tag>
        ) : (
          <Tag color='grey'>{t('已停用')}</Tag>
        );
      case 'compile_failed':
        return (
          <Tooltip content={item.runtime_error}>
            <Tag color='red'>{t('加载失败')}</Tag>
          </Tooltip>
        );
      default:
        return (
          <Tooltip content={item.runtime_error}>
            <Tag color='orange'>{t('未注册')}</Tag>
          </Tooltip>
        );
    }
  };

  const columns = [
    {
      title: t('插件'),
      key: 'plugin',
      render: (_, item) => (
        <div className='flex items-center gap-3 min-w-0'>
          <TaskPluginIcon
            pluginKey={item.meta.key}
            name={item.meta.name}
            icon={item.meta.icon}
            hasIcon={item.has_icon}
          />
          <div className='min-w-0'>
            <div className='font-medium truncate'>{item.meta.name}</div>
            <Text type='tertiary' size='small'>
              {item.meta.key}
            </Text>
            <div className='text-xs text-[var(--semi-color-text-2)] line-clamp-2 max-w-md'>
              {localizedText(item.meta.description, i18n.language)}
            </div>
          </div>
        </div>
      ),
    },
    {
      title: t('版本'),
      key: 'version',
      render: (_, item) => `v${item.meta.version}`,
    },
    {
      title: t('来源'),
      key: 'source',
      render: (_, item) => renderSource(item),
    },
    {
      title: t('运行状态'),
      key: 'runtime',
      render: (_, item) => renderRuntime(item),
    },
    {
      title: t('占用'),
      key: 'usage',
      render: (_, item) =>
        item.source === 'factory' ? (
          <Text type='tertiary'>-</Text>
        ) : (
          <Text size='small'>
            {t('{{channels}} 个渠道 / {{inflight}} 个进行中任务', {
              channels: item.channel_count,
              inflight: item.in_flight_count,
            })}
          </Text>
        ),
    },
    {
      title: t('启用'),
      key: 'enabled',
      render: (_, item) => (
        <Switch
          checked={item.enabled}
          loading={busyKey === item.meta.key}
          onChange={(checked) => setPluginStatus(item, checked)}
        />
      ),
    },
    {
      title: t('操作'),
      key: 'actions',
      render: (_, item) => (
        <Space>
          <Button size='small' onClick={() => openDetail(item.meta.key)}>
            {t('详情')}
          </Button>
          {item.source !== 'factory' && (
            <Button size='small' onClick={() => openVersions(item.meta.key)}>
              {t('版本管理')}
            </Button>
          )}
        </Space>
      ),
    },
  ];

  const versionColumns = [
    {
      title: t('版本'),
      dataIndex: 'version',
      render: (version, record) => (
        <Space>
          <span>v{version}</span>
          {record.active && <Tag color='green'>{t('生效中')}</Tag>}
          {!record.enabled && <Tag color='grey'>{t('已停用')}</Tag>}
        </Space>
      ),
    },
    {
      title: t('上传时间'),
      dataIndex: 'created_at',
      render: (value) => timestamp2string(value),
    },
    { title: t('备注'), dataIndex: 'remark' },
    {
      title: t('操作'),
      key: 'actions',
      render: (_, record) => (
        <Space>
          {!record.active && (
            <Button size='small' onClick={() => activateVersion(record)}>
              {t('切换到此版本')}
            </Button>
          )}
          <Button
            size='small'
            type='danger'
            onClick={() =>
              Modal.confirm({
                title: t('确定删除 v{{version}}？', {
                  version: record.version,
                }),
                okButtonProps: { type: 'danger' },
                onOk: () => deleteVersion(record),
              })
            }
          >
            {t('删除')}
          </Button>
        </Space>
      ),
    },
  ];

  const meta = detail?.meta;

  return (
    <div className='space-y-4'>
      <Card bodyStyle={{ padding: 16 }}>
        <div className='flex flex-col md:flex-row md:items-center gap-3 justify-between'>
          <div className='flex items-center gap-3'>
            <Switch
              checked={masterEnabled === true}
              disabled={masterEnabled === null}
              loading={masterSaving}
              onChange={(checked) => {
                if (checked) {
                  saveMasterSwitch(true);
                  return;
                }
                Modal.confirm({
                  title: t('确定关闭任务插件？'),
                  content: t(
                    '关闭后所有异步任务渠道（视频、音乐等）立即停止服务',
                  ),
                  okButtonProps: { type: 'danger' },
                  onOk: () => saveMasterSwitch(false),
                });
              }}
            />
            <div>
              <div className='font-medium'>{t('启用任务插件')}</div>
              <Text type='tertiary' size='small'>
                {t(
                  '总开关。可灵、即梦、Sora 等异步任务渠道均由插件驱动，关闭后这些渠道全部停止服务，进行中的任务在重新开启前不会继续轮询',
                )}
              </Text>
            </div>
          </div>
          <Space>
            <Button icon={<IconRefresh />} onClick={loadPlugins}>
              {t('刷新')}
            </Button>
            <Button
              theme='solid'
              icon={<IconUpload />}
              onClick={() => setUploadOpen(true)}
            >
              {t('上传插件')}
            </Button>
          </Space>
        </div>
      </Card>

      <Table
        rowKey={(item) => item.meta.key}
        columns={columns}
        dataSource={plugins}
        loading={loading}
        pagination={false}
      />

      <Modal
        title={t('版本管理 - {{key}}', { key: versionsKey })}
        visible={Boolean(versionsKey)}
        onCancel={() => setVersionsKey('')}
        footer={null}
        width={760}
      >
        <Table
          rowKey={(record) => `${record.key}-${record.version}`}
          columns={versionColumns}
          dataSource={versions}
          loading={versionsLoading}
          pagination={false}
          size='small'
        />
      </Modal>

      <Modal
        title={meta ? `${meta.name} v${meta.version}` : ''}
        visible={Boolean(detail)}
        onCancel={() => setDetail(null)}
        footer={null}
        width={900}
      >
        {meta && (
          <div className='space-y-3'>
            <Descriptions
              row
              size='small'
              data={[
                { key: t('标识'), value: meta.key },
                {
                  key: t('作者'),
                  value: meta.author?.name || '-',
                },
                { key: t('默认地址'), value: meta.baseUrl || '-' },
                {
                  key: t('允许访问的主机'),
                  value: (meta.allowedHosts || []).join(', ') || '-',
                },
                {
                  key: t('模型'),
                  value: (meta.models || []).join(', ') || '-',
                },
                {
                  key: t('原生路由'),
                  value:
                    (meta.routes || [])
                      .map((route) => `${route.method} ${route.path}`)
                      .join('\n') || '-',
                },
                {
                  key: t('协议'),
                  value:
                    (meta.protocols || [])
                      .map((protocol) => protocol.name)
                      .join(', ') || '-',
                },
              ]}
            />
            <pre className='text-xs bg-[var(--semi-color-fill-0)] rounded-lg p-3 max-h-[50vh] overflow-auto whitespace-pre'>
              {detail.source}
            </pre>
          </div>
        )}
      </Modal>

      <Modal
        title={t('上传插件')}
        visible={uploadOpen}
        onCancel={() => setUploadOpen(false)}
        onOk={submitUpload}
        okText={t('上传')}
        confirmLoading={uploading}
        width={760}
      >
        <div className='space-y-3'>
          <Banner
            type='warning'
            closeIcon={null}
            description={t(
              '插件在沙箱中运行，但会接触渠道密钥与请求内容，只安装来源可信的插件',
            )}
          />
          <div className='flex flex-wrap gap-3 items-center'>
            <Text>{t('从文件读取')}</Text>
            <input
              type='file'
              accept='.js,text/javascript'
              onChange={(event) => readSourceFile(event.target.files?.[0])}
            />
          </div>
          <TextArea
            rows={12}
            value={upload.source}
            placeholder={t('粘贴 plugin.js 源码')}
            onChange={(value) =>
              setUpload((prev) => ({ ...prev, source: value }))
            }
            style={{ fontFamily: 'monospace' }}
          />
          <div className='flex flex-wrap gap-3 items-center'>
            <Text>{t('图标（可选）')}</Text>
            <input
              type='file'
              accept='.svg,.png,image/svg+xml,image/png'
              onChange={(event) => readIconFile(event.target.files?.[0])}
            />
            {upload.icon && (
              <TaskPluginIcon iconSrc={upload.icon} name='icon' size={24} />
            )}
          </div>
          <Input
            value={upload.remark}
            placeholder={t('备注')}
            onChange={(value) =>
              setUpload((prev) => ({ ...prev, remark: value }))
            }
          />
          <div className='flex items-center gap-2'>
            <Switch
              checked={upload.enabled}
              onChange={(checked) =>
                setUpload((prev) => ({ ...prev, enabled: checked }))
              }
            />
            <Text>{t('上传后立即启用并切换到该版本')}</Text>
          </div>
          {uploading && <Spin />}
        </div>
      </Modal>
    </div>
  );
};

export default TaskPluginInstalled;
