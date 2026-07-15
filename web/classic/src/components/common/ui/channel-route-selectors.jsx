/*
Copyright (C) 2025 QuantumNous
...license header...
*/

import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Checkbox, Col, Input, Row, Select, Space, Spin, Tag, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconSearch } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError } from '../../../helpers';

const { Text } = Typography;

const ENDPOINT_TEMPLATES = [
  { label: 'OpenAI Chat', path: '/v1/chat/completions' },
  { label: 'OpenAI Responses', path: '/v1/responses' },
  { label: 'OpenAI Responses Compact', path: '/v1/responses/compact' },
  { label: 'Anthropic Messages', path: '/v1/messages' },
  { label: 'Gemini Generate', path: '/v1beta/models/{model}:generateContent' },
  { label: 'Embeddings', path: '/v1/embeddings' },
  { label: 'Rerank', path: '/v1/rerank' },
  { label: 'Image Generation', path: '/v1/images/generations' },
  { label: 'Audio Transcription', path: '/v1/audio/transcriptions' },
  { label: 'Audio Speech', path: '/v1/audio/speech' },
  { label: 'Moderations', path: '/v1/moderations' },
  { label: 'Files', path: '/v1/files' },
];

const MATCH_MODES = [
  { label: '精确匹配', value: 0 },
  { label: '前缀匹配', value: 1 },
  { label: '包含匹配', value: 2 },
  { label: '后缀匹配', value: 3 },
];

const CHANNEL_TYPE_NAMES = {
  0: 'Unknown', 1: 'OpenAI', 2: 'Midjourney', 3: 'Azure', 4: 'Ollama',
  5: 'Custom', 6: 'Telegram Bot', 7: 'DALL-E', 8: 'Claude', 9: 'Google PaLM',
  10: 'Zhipu', 11: 'Baidu', 12: 'Tencent', 13: 'Ali', 14: 'Anthropic',
  15: 'Xunfei', 16: 'Moonshot', 17: 'Perplexity', 18: 'Aws', 19: 'DeepSeek',
  20: 'Volc Engine', 21: 'Gemini', 22: 'Mistral', 23: 'OpenRouter', 24: 'Cloudflare',
  25: 'Cohere', 26: 'Hugging Face', 27: 'Xai', 28: 'Coze', 29: 'Jimeng',
  30: 'Vertex AI', 31: 'Silicon Flow', 32: 'Vidu', 33: 'Kling', 34: 'Submodel',
  35: 'MiniMax', 36: 'Replicate', 37: 'Codex', 38: 'Doubao', 39: 'Sora',
  40: 'Moka AI', 41: 'Lingyiwanwu', 42: 'Stability', 43: 'Hailuo', 44: 'Suno',
  45: 'Kuaishou', 46: 'Zhipu_4v', 47: 'Curl', 48: 'Dify', 49: 'OA2',
};

function nameRuleToRegex(name, rule) {
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  switch (rule) {
    case 0: return `^${escaped}$`;
    case 1: return `^${escaped}`;
    case 2: return escaped;
    case 3: return `${escaped}$`;
    default: return `^${escaped}$`;
  }
}

// Extract plain model names (exact-match entries only) from a multi-line
// model_regex text. Only `^name$` style entries can be queried via the
// /api/channel/bound API which does an exact IN match on abilities.model.
export function extractExactModelNames(modelRegexText) {
  const out = [];
  const seen = new Set();
  for (const line of (modelRegexText || '').split('\n')) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    if (!(trimmed.startsWith('^') && trimmed.endsWith('$'))) continue;
    const inner = trimmed.slice(1, -1);
    // Skip entries that still contain unescaped regex metacharacters after
    // stripping ^$ (e.g. `^gpt-(4o|4o-mini)$`) — they are real regexes, not
    // literal model names, and would return nothing from the exact-match API.
    if (/(?:^|[^\\])(?:\\\\)*[.*+?^${}()|[\]]/.test(inner)) continue;
    const name = inner.replace(/\\([.*+?^${}()|[\]\\])/g, '$1');
    if (!name || seen.has(name)) continue;
    seen.add(name);
    out.push(name);
  }
  return out;
}

// ---------------------------------------------------------------------------
// GroupSelector
// ---------------------------------------------------------------------------

export function GroupSelector({ value, onChange }) {
  const { t } = useTranslation();
  const [groups, setGroups] = useState([]);
  const [loading, setLoading] = useState(false);
  const [loaded, setLoaded] = useState(false);

  const selectedGroups = useMemo(() => {
    const names = [];
    for (const line of (value || '').split('\n').filter(Boolean)) {
      const m = line.match(/^\^?(.+?)\$?$/);
      if (m) names.push(m[1]);
    }
    return names;
  }, [value]);

  const loadGroups = useCallback(() => {
    if (loaded) return;
    setLoading(true);
    API.get('/api/group/')
      .then((res) => { const { success, data } = res.data; if (success && Array.isArray(data)) setGroups(data); })
      .catch(() => {})
      .finally(() => { setLoading(false); setLoaded(true); });
  }, [loaded]);

  const handleToggle = (groupName) => {
    const next = selectedGroups.includes(groupName) ? selectedGroups.filter((g) => g !== groupName) : [...selectedGroups, groupName];
    onChange(next.map((g) => `^${g}$`).join('\n'));
  };

  return (
    <div>
      <Text size='small' strong style={{ display: 'block', marginBottom: 4 }}>{t('匹配分组')}</Text>
      <div onClick={loadGroups} style={{ border: '1px solid var(--semi-color-border)', borderRadius: 4, padding: '8px 12px', cursor: 'pointer', minHeight: 32 }}>
        {loading ? <Spin size='small' /> : selectedGroups.length > 0 ? (
          <Space wrap size={4}>{selectedGroups.map((g) => <Tag key={g} closable onClose={() => handleToggle(g)} size='small'>{g}</Tag>)}</Space>
        ) : <Text type='tertiary'>{t('所有分组（不筛选）')}</Text>}
      </div>
      {loaded && !loading && (
        <div style={{ marginTop: 8, border: '1px solid var(--semi-color-border)', borderRadius: 4, padding: 8, maxHeight: 150, overflowY: 'auto' }}>
          {groups.map((group) => (
            <div key={group} style={{ padding: '4px 0' }}>
              <Checkbox checked={selectedGroups.includes(group)} onChange={() => handleToggle(group)}>{group}</Checkbox>
            </div>
          ))}
          {groups.length === 0 && <Text type='tertiary' size='small'>{t('无可用分组')}</Text>}
        </div>
      )}
      <Text type='tertiary' size='small'>{t('留空表示不区分分组。')}</Text>
    </div>
  );
}

// ---------------------------------------------------------------------------
// ModelNameMatcher
// ---------------------------------------------------------------------------

export function ModelNameMatcher({ value, onChange }) {
  const { t } = useTranslation();
  const [inputValue, setInputValue] = useState('');
  const [matchMode, setMatchMode] = useState(0);

  const entries = useMemo(() => {
    return (value || '').split('\n').filter(Boolean).map((regex) => {
      try {
        if (regex.startsWith('^') && regex.endsWith('$')) return { name: regex.slice(1, -1).replace(/\\./g, '.'), mode: 0 };
        if (regex.startsWith('^')) return { name: regex.slice(1).replace(/\\./g, '.'), mode: 1 };
        if (regex.endsWith('$')) return { name: regex.slice(0, -1).replace(/\\./g, '.'), mode: 3 };
        return { name: regex.replace(/\\./g, '.'), mode: 2 };
      } catch { return { name: regex, mode: 2 }; }
    });
  }, [value]);

  const handleAdd = () => {
    const name = inputValue.trim();
    if (!name) return;
    const newRegex = nameRuleToRegex(name, matchMode);
    const current = (value || '').split('\n').filter(Boolean);
    if (current.includes(newRegex)) return;
    onChange([...current, newRegex].join('\n'));
    setInputValue('');
  };

  const handleRemove = (index) => {
    onChange((value || '').split('\n').filter(Boolean).filter((_, i) => i !== index).join('\n'));
  };

  return (
    <div>
      <Text size='small' strong style={{ display: 'block', marginBottom: 4 }}>{t('模型匹配')} *</Text>
      <Row gutter={8}>
        <Col span={8}>
          <Select value={matchMode} onChange={(v) => setMatchMode(v)} style={{ width: '100%' }} optionList={MATCH_MODES.map((m) => ({ label: t(m.label), value: m.value }))} />
        </Col>
        <Col span={12}>
          <Input value={inputValue} onChange={(v) => setInputValue(v)} onPressEnter={handleAdd} placeholder={t('输入模型名称')} />
        </Col>
        <Col span={4}>
          <Button icon={<IconPlus />} onClick={handleAdd} disabled={!inputValue.trim()} style={{ width: '100%' }} />
        </Col>
      </Row>
      {entries.length > 0 && (
        <div style={{ marginTop: 8 }}>
          <Space wrap size={4}>
            {entries.map((entry, i) => (
              <Tag key={i} closable onClose={() => handleRemove(i)} size='small'>
                {entry.name}
                <Text type='tertiary' size='small' style={{ marginLeft: 4 }}>({MATCH_MODES[entry.mode]?.label || '?'})</Text>
              </Tag>
            ))}
          </Space>
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// PathSelector
// ---------------------------------------------------------------------------

export function PathSelector({ value, onChange }) {
  const { t } = useTranslation();
  const [customPath, setCustomPath] = useState('');

  const entries = useMemo(() => {
    return (value || '').split('\n').filter(Boolean).map((regex) => {
      try {
        if (regex.includes('generateContent')) {
          return { path: '/v1beta/models/{model}:generateContent', gemini: true };
        }
        const m = regex.match(/^\^(.+?)\$$/);
        return { path: m ? m[1].replace(/\\\./g, '.') : regex, gemini: false };
      } catch { return { path: regex, gemini: false }; }
    });
  }, [value]);

  const addPath = (path) => {
    let regex;
    if (path.includes('{model}')) {
      const afterVersion = path.replace(/^\/v1(alpha|beta)?\//, '');
      const versionGroup = '(v1|v1beta|v1alpha)';
      let body = afterVersion.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      body = body.replace(/\\\{model\\\}/, '[^/:]+');
      body = body.replace(/generateContent/, '(stream)?generateContent');
      regex = `^\\/${versionGroup}\\/${body}(\\?.*)?$`;
    } else {
      regex = `^${path.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`;
    }
    const current = (value || '').split('\n').filter(Boolean);
    if (current.includes(regex)) return;
    onChange([...current, regex].join('\n'));
  };

  const handleRemove = (index) => {
    onChange((value || '').split('\n').filter(Boolean).filter((_, i) => i !== index).join('\n'));
  };

  return (
    <div>
      <Text size='small' strong style={{ display: 'block', marginBottom: 4 }}>{t('匹配路径')}</Text>
      <Select placeholder={t('选择端点...')} onChange={(v) => addPath(v)} style={{ width: '100%', marginBottom: 8 }} filter optionList={ENDPOINT_TEMPLATES.map((ep) => ({ label: `${ep.label} — ${ep.path}`, value: ep.path }))} />
      <Row gutter={8}>
        <Col span={20}>
          <Input value={customPath} onChange={(v) => setCustomPath(v)} onPressEnter={() => { if (customPath.trim()) { addPath(customPath.trim()); setCustomPath(''); } }} placeholder={t('或输入自定义路径...')} />
        </Col>
        <Col span={4}>
          <Button icon={<IconPlus />} onClick={() => { if (customPath.trim()) { addPath(customPath.trim()); setCustomPath(''); } }} disabled={!customPath.trim()} style={{ width: '100%' }} />
        </Col>
      </Row>
      {entries.length > 0 && (
        <div style={{ marginTop: 8 }}>
          <Space wrap size={4}>
            {entries.map((entry, i) => (
              <Tag key={i} closable onClose={() => handleRemove(i)} size='small'>
                <span style={{ fontFamily: 'monospace' }}>{entry.path}</span>
              </Tag>
            ))}
          </Space>
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// ChannelSelector
// ---------------------------------------------------------------------------

export function ChannelSelector({ value, onChange, compact = false, modelNames }) {
  const { t } = useTranslation();
  const [channels, setChannels] = useState([]);
  const [loading, setLoading] = useState(false);
  const [search, setSearch] = useState('');
  const [showList, setShowList] = useState(false);
  const [loadedAll, setLoadedAll] = useState(false);
  const [boundLoaded, setBoundLoaded] = useState(false);
  // true when the full list was loaded because the bound query came back empty
  const [autoLoadedAll, setAutoLoadedAll] = useState(false);

  const selectedIds = useMemo(() => {
    const ids = [];
    for (const tok of (value || '').split(/[\n,]/).map((s) => s.trim()).filter(Boolean)) {
      const n = Number(tok); if (Number.isInteger(n) && n > 0) ids.push(n);
    }
    return [...new Set(ids)];
  }, [value]);

  const channelMap = useMemo(() => { const m = new Map(); for (const ch of channels) m.set(ch.id, ch); return m; }, [channels]);

  // modelNames is an array of exact model names extracted from the upstream
  // ModelNameMatcher. Stringify it to use as a stable dependency / cache key.
  const normalizedModels = useMemo(() => {
    if (!Array.isArray(modelNames)) return [];
    return [...new Set(modelNames.map((s) => String(s || '').trim()).filter(Boolean))].sort();
  }, [modelNames]);
  const modelKey = normalizedModels.join(',');
  const modelKeyRef = useRef(modelKey);
  modelKeyRef.current = modelKey;

  // Auto-load channels bound to the upstream model names whenever they change.
  // Uses /api/channel/bound which already filters by abilities.enabled=true and
  // channels.status=1 (i.e. enabled channels that actually serve the model).
  useEffect(() => {
    if (!modelKey) {
      setChannels([]);
      setBoundLoaded(false);
      setLoadedAll(false);
      setAutoLoadedAll(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setBoundLoaded(false);
    setAutoLoadedAll(false);
    API.get(`/api/channel/bound?model=${encodeURIComponent(modelKey)}`)
      .then((res) => {
        if (cancelled) return;
        const { success, data, message } = res.data;
        if (success && Array.isArray(data)) {
          setChannels(data.map((ch) => ({ id: ch.id, name: ch.name, type: ch.type, status: 1 })));
        } else if (!success) {
          showError(message || t('查询绑定渠道失败'));
        }
      })
      .catch(() => {})
      .finally(() => {
        if (cancelled) return;
        setLoading(false);
        setBoundLoaded(true);
        setLoadedAll(false);
      });
    return () => { cancelled = true; };
  }, [modelKey, t]);

  // Fallback: load the full channel list — used when there are no exact model
  // names to query (e.g. only prefix / contains / suffix matches), when the
  // bound-channel query returns nothing, or on demand via the button below so
  // the user can always pick channels not yet bound to the model.
  const loadAllChannels = useCallback(() => {
    if (loadedAll) return;
    // Guard against a stale response landing after modelKey changed and the
    // bound-channel query already refreshed the list.
    const keyAtRequest = modelKeyRef.current;
    setLoading(true);
    API.get('/api/channel/?p=0&page_size=500&id_sort=true')
      .then((res) => {
        if (modelKeyRef.current !== keyAtRequest) return;
        const { success, data } = res.data;
        if (success && data?.items) {
          setChannels(data.items.map((ch) => ({ id: ch.id, name: ch.name, type: ch.type, status: ch.status })));
        }
      })
      .catch(() => {})
      .finally(() => {
        if (modelKeyRef.current !== keyAtRequest) return;
        setLoading(false);
        setLoadedAll(true);
      });
  }, [loadedAll]);

  // Auto-fallback: if the bound-channel query came back empty, load the full
  // channel list so the tier is still configurable.
  useEffect(() => {
    if (modelKey && boundLoaded && !loadedAll && channels.length === 0) {
      setAutoLoadedAll(true);
      loadAllChannels();
    }
  }, [modelKey, boundLoaded, loadedAll, channels.length, loadAllChannels]);

  const filteredChannels = useMemo(() => {
    if (!search) return channels;
    const q = search.toLowerCase();
    return channels.filter((ch) => ch.name.toLowerCase().includes(q) || String(ch.id).includes(q) || (CHANNEL_TYPE_NAMES[ch.type] || '').toLowerCase().includes(q));
  }, [channels, search]);

  const handleToggle = (id) => {
    const next = selectedIds.includes(id) ? selectedIds.filter((i) => i !== id) : [...selectedIds, id];
    onChange(next.join('\n'));
  };

  const selectedDisplay = useMemo(() => selectedIds.map((id) => { const ch = channelMap.get(id); return { id, name: ch?.name || `#${id}`, type: ch?.type }; }), [selectedIds, channelMap]);

  const hintText = modelKey
    ? loadedAll && autoLoadedAll
      ? t('未找到该模型的绑定渠道，已显示全部渠道')
      : boundLoaded && !loadedAll && channels.length === 0
        ? t('未找到该模型的启用渠道')
        : t('匹配模型：{{model}}', { model: modelKey })
    : t('请先在上方"模型匹配"中添加精确匹配的模型名以自动筛选渠道');

  return (
    <div>
      <Text size='small' strong style={{ display: 'block', marginBottom: 4 }}>{compact ? t('渠道池') : t('兜底渠道池')}</Text>
      <Text type='tertiary' size='small' style={{ display: 'block', marginBottom: 8 }}>{hintText}</Text>
      <div onClick={() => { setShowList(!showList); if (!modelKey) loadAllChannels(); }} style={{ border: '1px solid var(--semi-color-border)', borderRadius: 4, padding: '8px 12px', cursor: 'pointer', minHeight: 32 }}>
        {selectedIds.length > 0 ? <Text>{t('已选择 {{count}} 个渠道', { count: selectedIds.length })}</Text> : <Text type='tertiary'>{t('点击选择渠道...')}</Text>}
      </div>
      {showList && (
        <div style={{ marginTop: 8, border: '1px solid var(--semi-color-border)', borderRadius: 4, padding: 8 }}>
          <Input value={search} onChange={(v) => setSearch(v)} placeholder={t('搜索渠道...')} prefix={<IconSearch size='small' />} showClear style={{ marginBottom: 8 }} />
          <div style={{ maxHeight: 200, overflowY: 'auto' }}>
            {loading ? <div style={{ textAlign: 'center', padding: 16 }}><Spin size='small' /></div>
            : filteredChannels.length === 0 ? <Text type='tertiary' size='small' style={{ display: 'block', textAlign: 'center', padding: 16 }}>{modelKey && !loadedAll ? t('未找到该模型的启用渠道') : t('无匹配渠道')}</Text>
            : filteredChannels.map((ch) => (
              <div key={ch.id} style={{ padding: '4px 0', display: 'flex', alignItems: 'center', gap: 8 }}>
                <Checkbox checked={selectedIds.includes(ch.id)} onChange={() => handleToggle(ch.id)}><span>{ch.name}</span></Checkbox>
                <Tag size='small' style={{ marginLeft: 'auto' }}>{CHANNEL_TYPE_NAMES[ch.type] || `Type ${ch.type}`}</Tag>
              </div>
            ))}
          </div>
          {!loadedAll && !loading && (
            <Button size='small' theme='borderless' onClick={loadAllChannels} style={{ marginTop: 8 }}>{t('加载全部渠道')}</Button>
          )}
        </div>
      )}
      {selectedDisplay.length > 0 && (
        <div style={{ marginTop: 8 }}>
          <Space wrap size={4}>
            {selectedDisplay.map((ch) => (
              <Tag key={ch.id} closable onClose={() => handleToggle(ch.id)} size='small'>
                {ch.name}
                {ch.type != null && <Text type='tertiary' size='small' style={{ marginLeft: 4 }}>({CHANNEL_TYPE_NAMES[ch.type] || ch.type})</Text>}
              </Tag>
            ))}
          </Space>
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// useChannelNameMap
// ---------------------------------------------------------------------------

export function useChannelNameMap() {
  const [map, setMap] = useState(new Map());
  useEffect(() => {
    API.get('/api/channel/?p=0&page_size=500&id_sort=true')
      .then((res) => { const { success, data } = res.data; if (success && data?.items) { const m = new Map(); for (const ch of data.items) m.set(ch.id, ch.name); setMap(m); } })
      .catch(() => {});
  }, []);
  return useCallback((id) => map.get(id) || `#${id}`, [map]);
}
