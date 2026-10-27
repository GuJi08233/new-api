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
  Collapse,
  Descriptions,
  Empty,
  Input,
  Modal,
  Select,
  Space,
  Spin,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  IconDelete,
  IconPlus,
  IconRefresh,
  IconSetting,
} from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';
import TaskPluginIcon from './TaskPluginIcon';
import {
  deriveInstallState,
  fetchMarketplaceIndex,
  fetchPluginIconDataUri,
  fetchPluginSourceText,
  findRuntimeFailure,
  localizedText,
  resolvePluginSourceUrl,
  sha256Hex,
} from './marketplace';

const { Text, Title } = Typography;

// 项目维护的两个官方索引，其它源都标记为第三方
const OFFICIAL_INDEX_URLS = [
  'https://www.newapi.ai/api/v1/plugins/index.json',
  'https://raw.githubusercontent.com/QuantumNous/new-api-plugins/main/index.json',
];
const DEFAULT_SOURCES = [
  { name: 'Official', index_url: OFFICIAL_INDEX_URLS[0] },
  { name: 'GitHub', index_url: OFFICIAL_INDEX_URLS[1] },
];

const TaskPluginMarketplace = ({ refreshKey, onChanged }) => {
  const { t, i18n } = useTranslation();
  const [sources, setSources] = useState([]);
  const [sourceUrl, setSourceUrl] = useState('');
  const [index, setIndex] = useState(null);
  const [indexError, setIndexError] = useState('');
  const [indexLoading, setIndexLoading] = useState(false);
  const [installed, setInstalled] = useState([]);
  const [sourcesOpen, setSourcesOpen] = useState(false);
  const [editingSources, setEditingSources] = useState([]);
  const [savingSources, setSavingSources] = useState(false);
  const [target, setTarget] = useState(null);
  const [targetVersion, setTargetVersion] = useState('');
  const [fetched, setFetched] = useState(null);
  const [fetchError, setFetchError] = useState('');
  const [fetching, setFetching] = useState(false);
  const [installing, setInstalling] = useState(false);

  const source = sources.find((item) => item.index_url === sourceUrl);

  const loadSources = async () => {
    const res = await API.get('/api/plugin/task/marketplace/sources');
    if (!res.data.success) {
      showError(res.data.message);
      return;
    }
    const list = res.data.data || [];
    setSources(list);
    setSourceUrl((current) =>
      list.some((item) => item.index_url === current)
        ? current
        : list[0]?.index_url || '',
    );
  };

  const loadInstalled = async () => {
    const res = await API.get('/api/plugin/task');
    if (!res.data.success) return [];
    const list = res.data.data || [];
    setInstalled(list);
    return list;
  };

  const loadIndex = async (url) => {
    if (!url) {
      setIndex(null);
      return;
    }
    setIndexLoading(true);
    setIndexError('');
    try {
      setIndex(await fetchMarketplaceIndex(url));
    } catch (error) {
      setIndex(null);
      setIndexError(error?.message || String(error));
    } finally {
      setIndexLoading(false);
    }
  };

  useEffect(() => {
    loadSources();
  }, []);

  useEffect(() => {
    loadInstalled();
  }, [refreshKey]);

  useEffect(() => {
    loadIndex(sourceUrl);
  }, [sourceUrl]);

  // 版本切换时重新拉取源码并在浏览器端先比对一次 sha256
  useEffect(() => {
    if (!target || !targetVersion) return undefined;
    const entry = target.plugin.versions.find(
      (item) => item.version === targetVersion,
    );
    let cancelled = false;
    setFetched(null);
    setFetchError('');
    setFetching(true);
    (async () => {
      const url = resolvePluginSourceUrl(target.source.index_url, entry.path);
      if (!url) {
        throw new Error(t('插件路径不在索引所在的站点内，已拒绝下载'));
      }
      const text = await fetchPluginSourceText(url);
      const digest = await sha256Hex(new TextEncoder().encode(text));
      const iconUrl = target.plugin.iconFile
        ? resolvePluginSourceUrl(
            target.source.index_url,
            target.plugin.iconFile.path,
          )
        : null;
      const icon = iconUrl
        ? await fetchPluginIconDataUri(iconUrl, target.plugin.iconFile.sha256)
        : null;
      return { entry, text, digest, icon };
    })()
      .then((result) => {
        if (!cancelled) setFetched(result);
      })
      .catch((error) => {
        if (cancelled) return;
        setFetchError(
          error instanceof TypeError
            ? t('浏览器无法获取插件源码，源站可能不允许跨域访问或无法连接')
            : error?.message || String(error),
        );
      })
      .finally(() => {
        if (!cancelled) setFetching(false);
      });
    return () => {
      cancelled = true;
    };
  }, [target, targetVersion]);

  const openInstall = (plugin) => {
    setTarget({
      plugin,
      source,
      installState: deriveInstallState(plugin, installed),
    });
    setTargetVersion(plugin.latest);
  };

  const digestMismatch = Boolean(
    fetched?.entry.sha256 &&
      fetched.digest &&
      fetched.digest.toLowerCase() !== fetched.entry.sha256.toLowerCase(),
  );

  const install = async () => {
    if (!fetched || digestMismatch) return;
    setInstalling(true);
    try {
      // 不传 force：路由冲突必须让管理员在已安装列表里处理，而不是静默覆盖
      const res = await API.post('/api/plugin/task', {
        source: fetched.text,
        sourceSha256: fetched.entry.sha256,
        enabled: true,
        remark: `${target.source.name} v${fetched.entry.version}`,
        icon: fetched.icon || undefined,
      });
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      const detail = res.data.data;
      if (detail.plugin && !detail.plugin.active) {
        const activateRes = await API.post(
          `/api/plugin/task/${encodeURIComponent(detail.meta.key)}/activate`,
          { version: detail.meta.version },
        );
        if (!activateRes.data.success) {
          showError(activateRes.data.message);
          return;
        }
      }
      setTarget(null);
      const list = await loadInstalled();
      onChanged?.();
      const failure = findRuntimeFailure(list, detail.meta.key);
      if (failure) {
        showError(t('插件已安装，但未能加载：{{error}}', { error: failure }));
        return;
      }
      showSuccess(
        t('已安装 {{name}} v{{version}}', {
          name: detail.meta.name,
          version: detail.meta.version,
        }),
      );
    } finally {
      setInstalling(false);
    }
  };

  const saveSources = async () => {
    setSavingSources(true);
    try {
      const payload = editingSources
        .map((item) => ({
          name: item.name.trim(),
          index_url: item.index_url.trim(),
        }))
        .filter((item) => item.name || item.index_url);
      const res = await API.put(
        '/api/plugin/task/marketplace/sources',
        payload,
      );
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      showSuccess(t('保存成功'));
      setSourcesOpen(false);
      await loadSources();
    } finally {
      setSavingSources(false);
    }
  };

  const updateEditingSource = (position, field, value) => {
    setEditingSources((prev) =>
      prev.map((item, i) =>
        i === position ? { ...item, [field]: value } : item,
      ),
    );
  };

  const renderInstallState = (state) => {
    switch (state.status) {
      case 'up_to_date':
        return <Tag color='green'>{t('已是最新')}</Tag>;
      case 'modified':
        return (
          <Tag color='blue'>
            {t('v{{version}} 本地修改版', {
              version: state.installedVersion,
            })}
          </Tag>
        );
      case 'upgradable':
        return (
          <Tag color='orange'>
            {t('可升级（当前 v{{version}}）', {
              version: state.installedVersion,
            })}
          </Tag>
        );
      case 'diverged':
        return (
          <Tag color='purple'>
            {t('已安装 v{{version}}（不在此源中）', {
              version: state.installedVersion,
            })}
          </Tag>
        );
      default:
        return null;
    }
  };

  const installButtonText = (state) => {
    if (state.status === 'not_installed') return t('安装');
    if (state.status === 'upgradable') return t('升级');
    return t('查看');
  };

  const isOfficial = OFFICIAL_INDEX_URLS.includes(sourceUrl);
  const missingHashes =
    index?.plugins.length > 0 &&
    index.plugins.some((plugin) =>
      plugin.versions.some((version) => !version.sha256),
    );
  const targetEntry = target?.plugin.versions.find(
    (item) => item.version === targetVersion,
  );

  return (
    <div className='space-y-4'>
      <Card bodyStyle={{ padding: 16 }}>
        <div className='flex flex-col md:flex-row md:items-center gap-3 justify-between'>
          <Space wrap>
            <Text>{t('插件源')}</Text>
            <Select
              value={sourceUrl}
              style={{ width: 280 }}
              optionList={sources.map((item) => ({
                value: item.index_url,
                label: item.name,
              }))}
              onChange={setSourceUrl}
              emptyContent={t('未配置插件源')}
            />
            <Button
              icon={<IconRefresh />}
              onClick={() => {
                loadIndex(sourceUrl);
                loadInstalled();
              }}
            >
              {t('刷新')}
            </Button>
          </Space>
          <Button
            icon={<IconSetting />}
            onClick={() => {
              setEditingSources(sources.map((item) => ({ ...item })));
              setSourcesOpen(true);
            }}
          >
            {t('管理插件源')}
          </Button>
        </div>
        {sourceUrl && (
          <Text type='tertiary' size='small' className='block mt-2 break-all'>
            {sourceUrl}
          </Text>
        )}
      </Card>

      {sourceUrl && !isOfficial && (
        <Banner
          type='warning'
          closeIcon={null}
          description={t(
            '这是第三方插件源，插件会接触渠道密钥与请求内容，安装前请审阅源码，风险自负',
          )}
        />
      )}
      {missingHashes && (
        <Banner
          type='warning'
          closeIcon={null}
          description={t(
            '该源部分版本未提供 sha256，安装时无法校验源码是否被篡改',
          )}
        />
      )}
      {indexError && (
        <Banner
          type='danger'
          closeIcon={null}
          description={t('无法加载插件索引：{{error}}', { error: indexError })}
        />
      )}

      <Spin spinning={indexLoading}>
        {index && index.plugins.length === 0 && (
          <Empty description={t('该源没有可用的插件')} />
        )}
        <div className='grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4'>
          {(index?.plugins || []).map((plugin) => {
            const state = deriveInstallState(plugin, installed);
            return (
              <Card
                key={plugin.key}
                className='h-full'
                bodyStyle={{
                  padding: 16,
                  height: '100%',
                  display: 'flex',
                  flexDirection: 'column',
                }}
              >
                <div className='flex gap-3'>
                  <TaskPluginIcon
                    pluginKey={plugin.key}
                    name={plugin.name}
                    icon={plugin.icon}
                    size={40}
                  />
                  <div className='min-w-0 flex-1'>
                    <div className='flex items-center gap-2 flex-wrap'>
                      <Title heading={6} className='!mb-0'>
                        {plugin.name}
                      </Title>
                      <Tag size='small'>v{plugin.latest}</Tag>
                    </div>
                    <Text type='tertiary' size='small'>
                      {plugin.key}
                    </Text>
                  </div>
                </div>
                <div className='text-sm text-[var(--semi-color-text-1)] mt-3 min-h-[40px] line-clamp-3'>
                  {localizedText(plugin.description, i18n.language)}
                </div>
                {plugin.models.length > 0 && (
                  <div className='flex flex-wrap gap-1 mt-2'>
                    {plugin.models.slice(0, 6).map((model) => (
                      <Tag key={model} size='small' color='white'>
                        {model}
                      </Tag>
                    ))}
                    {plugin.models.length > 6 && (
                      <Tag size='small' color='white'>
                        +{plugin.models.length - 6}
                      </Tag>
                    )}
                  </div>
                )}
                <div className='flex items-center justify-between gap-2 mt-auto pt-3'>
                  <div>{renderInstallState(state)}</div>
                  <Space>
                    {plugin.website && (
                      <Button
                        size='small'
                        theme='borderless'
                        onClick={() =>
                          window.open(
                            plugin.website,
                            '_blank',
                            'noopener,noreferrer',
                          )
                        }
                      >
                        {t('官网')}
                      </Button>
                    )}
                    <Button
                      size='small'
                      theme={
                        state.status === 'not_installed' ||
                        state.status === 'upgradable'
                          ? 'solid'
                          : 'light'
                      }
                      onClick={() => openInstall(plugin)}
                    >
                      {installButtonText(state)}
                    </Button>
                  </Space>
                </div>
              </Card>
            );
          })}
        </div>
      </Spin>

      <Modal
        title={
          target
            ? t('安装 {{name}}', { name: target.plugin.name })
            : t('安装插件')
        }
        visible={Boolean(target)}
        onCancel={() => setTarget(null)}
        onOk={install}
        okText={t('安装并启用')}
        okButtonProps={{ disabled: !fetched || fetching || digestMismatch }}
        confirmLoading={installing}
        width={860}
      >
        {target && (
          <div className='space-y-3'>
            <div className='flex items-center gap-3 flex-wrap'>
              <TaskPluginIcon
                pluginKey={target.plugin.key}
                name={target.plugin.name}
                icon={target.plugin.icon}
                iconSrc={fetched?.icon || undefined}
                size={36}
              />
              <Select
                value={targetVersion}
                style={{ width: 240 }}
                disabled={installing}
                optionList={target.plugin.versions.map((version) => ({
                  value: version.version,
                  label:
                    version.version === target.plugin.latest
                      ? `v${version.version} · ${t('最新')}`
                      : `v${version.version}`,
                }))}
                onChange={setTargetVersion}
              />
              {target.installState.status !== 'not_installed' && (
                <Text type='tertiary' size='small'>
                  {t('当前 v{{from}} → 安装 v{{to}}', {
                    from: target.installState.installedVersion,
                    to: targetVersion,
                  })}
                </Text>
              )}
            </div>
            <Descriptions
              row
              size='small'
              data={[
                { key: t('标识'), value: target.plugin.key },
                { key: t('来源'), value: target.source.name },
                {
                  key: t('默认地址'),
                  value: targetEntry?.baseUrl || '-',
                },
                {
                  key: t('允许访问的主机'),
                  value: (targetEntry?.allowedHosts || []).join(', ') || '-',
                },
                {
                  key: t('模型'),
                  value: target.plugin.models.join(', ') || '-',
                },
              ]}
            />
            {(target.installState.status === 'modified' ||
              target.installState.modified) && (
              <Banner
                type='warning'
                closeIcon={null}
                description={t(
                  '当前生效的 v{{version}} 与市场同版本源码不同（内置插件可能带有本地安全修补），安装市场版本会覆盖这些修改',
                  { version: target.installState.installedVersion },
                )}
              />
            )}
            {fetching && (
              <div className='flex items-center gap-2'>
                <Spin size='small' />
                <Text>{t('正在获取插件源码...')}</Text>
              </div>
            )}
            {fetchError && (
              <Banner type='danger' closeIcon={null} description={fetchError} />
            )}
            {fetched && digestMismatch && (
              <Banner
                type='danger'
                closeIcon={null}
                description={t(
                  '源码 sha256 与索引不一致，可能已被篡改，已禁止安装',
                )}
              />
            )}
            {fetched && !digestMismatch && fetched.entry.sha256 && (
              <Banner
                type={fetched.digest ? 'success' : 'info'}
                closeIcon={null}
                description={
                  fetched.digest
                    ? t('源码 sha256 校验通过')
                    : t(
                        '当前页面不是安全上下文，浏览器无法计算 sha256，将由服务端校验',
                      )
                }
              />
            )}
            {fetched && !fetched.entry.sha256 && (
              <Banner
                type='warning'
                closeIcon={null}
                description={t('该版本未提供 sha256，无法校验源码完整性')}
              />
            )}
            {fetched && (
              <Collapse>
                <Collapse.Panel header={t('查看完整源码')} itemKey='source'>
                  <pre className='text-xs bg-[var(--semi-color-fill-0)] rounded-lg p-3 max-h-[45vh] overflow-auto whitespace-pre'>
                    {fetched.text}
                  </pre>
                </Collapse.Panel>
              </Collapse>
            )}
          </div>
        )}
      </Modal>

      <Modal
        title={t('管理插件源')}
        visible={sourcesOpen}
        onCancel={() => setSourcesOpen(false)}
        onOk={saveSources}
        confirmLoading={savingSources}
        width={760}
      >
        <div className='space-y-3'>
          <Text type='tertiary' size='small'>
            {t(
              '插件索引由浏览器直接拉取，源站需要允许跨域访问；源码路径必须与索引同站',
            )}
          </Text>
          {editingSources.map((item, position) => (
            <div key={position} className='flex gap-2 items-center'>
              <Input
                value={item.name}
                placeholder={t('名称')}
                style={{ width: 160 }}
                onChange={(value) =>
                  updateEditingSource(position, 'name', value)
                }
              />
              <Input
                value={item.index_url}
                placeholder='https://example.com/index.json'
                onChange={(value) =>
                  updateEditingSource(position, 'index_url', value)
                }
              />
              <Button
                icon={<IconDelete />}
                type='danger'
                theme='borderless'
                onClick={() =>
                  setEditingSources((prev) =>
                    prev.filter((_, i) => i !== position),
                  )
                }
              />
            </div>
          ))}
          <Space>
            <Button
              icon={<IconPlus />}
              onClick={() =>
                setEditingSources((prev) => [
                  ...prev,
                  { name: '', index_url: '' },
                ])
              }
            >
              {t('添加插件源')}
            </Button>
            <Button
              onClick={() =>
                setEditingSources(DEFAULT_SOURCES.map((item) => ({ ...item })))
              }
            >
              {t('恢复默认')}
            </Button>
          </Space>
        </div>
      </Modal>
    </div>
  );
};

export default TaskPluginMarketplace;
