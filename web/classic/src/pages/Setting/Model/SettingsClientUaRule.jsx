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

import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  Banner,
  Button,
  Col,
  Divider,
  Form,
  Modal,
  Row,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconEdit, IconPlus } from '@douyinfe/semi-icons';
import {
  API,
  compareObjects,
  showError,
  showSuccess,
  showWarning,
  verifyJSON,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';
import {
  ChannelSelector,
  GroupSelector,
  ModelNameMatcher,
  PathSelector,
  extractExactModelNames,
  useChannelNameMap,
} from '../../../components/common/ui/channel-route-selectors';

const { Text } = Typography;

const KEY_ENABLED = 'client_ua_rule_setting.enabled';
const KEY_RULES = 'client_ua_rule_setting.rules';

const defaultInputs = {
  [KEY_ENABLED]: false,
  [KEY_RULES]: '[]',
};

// UA 条目可能含逗号（如 "(external, cli)"），因此一律按行拆分，不按逗号拆分。
const splitLines = (text) =>
  (text || '')
    .split('\n')
    .map((item) => item.trim())
    .filter((item) => item.length > 0);

const rulesExample = JSON.stringify(
  [
    {
      name: 'Opus 仅限 Claude Code CLI',
      model_regex: ['^claude-opus'],
      ua_allow: ['(external, cli)'],
    },
    {
      name: '渠道 12 只给 Claude Code',
      channel_ids: [12],
      ua_allow: ['re:^claude-cli/'],
      message: '该线路仅供 Claude Code 使用',
    },
    {
      name: '禁止 Claude Code SDK 模式',
      model_regex: ['^claude-'],
      ua_deny: ['(external, sdk-cli)'],
      status_code: 403,
    },
  ],
  null,
  2,
);

const parseRules = (jsonString) => {
  try {
    const parsed = JSON.parse(jsonString || '[]');
    if (!Array.isArray(parsed)) return [];
    return parsed.map((rule, index) => ({ ...(rule || {}), id: index }));
  } catch (e) {
    return [];
  }
};

const rulesToJson = (rules) =>
  JSON.stringify(
    (rules || []).map(({ id, ...rest }) => rest),
    null,
    2,
  );

const renderTagList = (list, color) =>
  (list || []).length > 0 ? (
    <Space wrap spacing={4}>
      {(list || []).slice(0, 3).map((item, index) => (
        <Tag key={`${item}-${index}`} color={color}>
          {item}
        </Tag>
      ))}
      {(list || []).length > 3 && (
        <Text type='tertiary'>+{list.length - 3}</Text>
      )}
    </Space>
  ) : (
    <Text type='tertiary'>-</Text>
  );

export default function SettingsClientUaRule(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState(defaultInputs);
  const [inputsRow, setInputsRow] = useState(defaultInputs);
  const [editMode, setEditMode] = useState('visual');
  const [rules, setRules] = useState([]);
  const [modalVisible, setModalVisible] = useState(false);
  const [editingRule, setEditingRule] = useState(null);
  const [selectorGroupRegex, setSelectorGroupRegex] = useState('');
  const [selectorModelRegex, setSelectorModelRegex] = useState('');
  const [selectorPathRegex, setSelectorPathRegex] = useState('');
  const [selectorChannelIds, setSelectorChannelIds] = useState('');
  const refForm = useRef();
  const modalFormRef = useRef();
  const getChannelName = useChannelNameMap();

  const editingModelNames = useMemo(
    () => extractExactModelNames(selectorModelRegex),
    [selectorModelRegex],
  );

  const updateRulesState = (nextRules) => {
    const normalized = (nextRules || []).map((rule, index) => ({
      ...rule,
      id: index,
    }));
    const jsonString = rulesToJson(normalized);
    setRules(normalized);
    setInputs((prev) => ({ ...prev, [KEY_RULES]: jsonString }));
    refForm.current?.setValue(KEY_RULES, jsonString);
  };

  const openModal = (rule) => {
    const nextRule = rule ? { ...rule } : null;
    setEditingRule(nextRule);
    setSelectorGroupRegex((nextRule?.group_regex || []).join('\n'));
    setSelectorModelRegex((nextRule?.model_regex || []).join('\n'));
    setSelectorPathRegex((nextRule?.path_regex || []).join('\n'));
    setSelectorChannelIds((nextRule?.channel_ids || []).join('\n'));
    setModalVisible(true);
  };

  const applyPreset = (preset) => {
    if (preset === 'cli-only') {
      modalFormRef.current?.setValue('ua_allow_text', '(external, cli)');
      modalFormRef.current?.setValue('ua_deny_text', '');
    } else {
      modalFormRef.current?.setValue('ua_allow_text', '');
      modalFormRef.current?.setValue('ua_deny_text', '(external, sdk-cli)');
    }
  };

  const handleModalSave = async () => {
    let values;
    try {
      values = await modalFormRef.current.validate();
    } catch (error) {
      return showError(t('请检查输入'));
    }
    const channelIds = splitLines(selectorChannelIds.replace(/,/g, '\n')).map(
      Number,
    );
    if (channelIds.some((id) => !Number.isInteger(id) || id <= 0)) {
      return showError(t('渠道 ID 必须为正整数'));
    }
    const rule = {
      name: (values.name || '').trim(),
      disabled: !!values.disabled || undefined,
      model_regex: splitLines(selectorModelRegex),
      group_regex: splitLines(selectorGroupRegex),
      path_regex: splitLines(selectorPathRegex),
      channel_ids: [...new Set(channelIds)],
      ua_allow: splitLines(values.ua_allow_text),
      ua_deny: splitLines(values.ua_deny_text),
      message: (values.message || '').trim(),
      status_code: values.status_code || undefined,
    };
    if (!rule.name) return showError(t('名称不能为空'));
    if (rule.ua_allow.length === 0 && rule.ua_deny.length === 0) {
      return showError(t('UA 白名单与黑名单不能同时为空'));
    }
    Object.keys(rule).forEach((key) => {
      const value = rule[key];
      if (
        value === undefined ||
        value === '' ||
        (Array.isArray(value) && value.length === 0)
      ) {
        delete rule[key];
      }
    });

    const nextRules = [...rules];
    if (editingRule) {
      const index = nextRules.findIndex((item) => item.id === editingRule.id);
      if (index < 0) return showError(t('规则未找到，请刷新后重试'));
      nextRules[index] = rule;
    } else {
      nextRules.push(rule);
    }
    if (new Set(nextRules.map((item) => item.name)).size !== nextRules.length) {
      return showError(t('规则名称不能重复'));
    }
    updateRulesState(nextRules);
    setModalVisible(false);
    setEditingRule(null);
  };

  const switchToVisualMode = () => {
    const raw = inputs[KEY_RULES] || '[]';
    if (!verifyJSON(raw) || !Array.isArray(JSON.parse(raw))) {
      return showError(t('规则 JSON 格式不正确'));
    }
    setRules(parseRules(raw));
    setEditMode('visual');
  };

  const switchToJsonMode = () => {
    if (editMode === 'json') return;
    const jsonString = rulesToJson(rules);
    setInputs((prev) => ({ ...prev, [KEY_RULES]: jsonString }));
    refForm.current?.setValue(KEY_RULES, jsonString);
    setEditMode('json');
  };

  const onSubmit = () => {
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    let compactRules;
    try {
      compactRules = JSON.stringify(JSON.parse(inputs[KEY_RULES] || '[]'));
    } catch (error) {
      return showError(t('规则 JSON 格式不正确'));
    }
    const requestQueue = updateArray.map((item) => {
      const value = item.key === KEY_RULES ? compactRules : inputs[item.key];
      return API.put('/api/option/', { key: item.key, value: String(value) });
    });
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (res.includes(undefined)) {
          if (requestQueue.length > 1) showError(t('部分保存失败，请重试'));
          return;
        }
        const failed = res.find((item) => !item?.data?.success);
        if (failed) {
          showError(failed.data?.message || t('保存失败，请重试'));
          return;
        }
        showSuccess(t('保存成功'));
        props.refresh();
      })
      .catch(() => showError(t('保存失败，请重试')))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    const currentInputs = { ...defaultInputs };
    if (props.options[KEY_ENABLED] !== undefined) {
      currentInputs[KEY_ENABLED] = props.options[KEY_ENABLED];
    }
    if (props.options[KEY_RULES] !== undefined) {
      currentInputs[KEY_RULES] = props.options[KEY_RULES] || '[]';
    }
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    setRules(parseRules(currentInputs[KEY_RULES]));
    refForm.current?.setValues(currentInputs);
  }, [props.options]);

  const columns = [
    {
      title: t('名称'),
      dataIndex: 'name',
      render: (text, record) => (
        <Space>
          <Text>{text || '-'}</Text>
          {record.disabled && <Tag color='grey'>{t('已停用')}</Tag>}
        </Space>
      ),
    },
    {
      title: t('模型正则'),
      dataIndex: 'model_regex',
      render: (list) => renderTagList(list),
    },
    {
      title: t('范围'),
      render: (_, record) => (
        <Space vertical align='start' spacing={2}>
          {(record.group_regex || []).length > 0 && (
            <Text size='small'>
              {t('分组')}: {record.group_regex.join(', ')}
            </Text>
          )}
          {(record.path_regex || []).length > 0 && (
            <Text size='small'>
              {t('路径')}: {record.path_regex.join(', ')}
            </Text>
          )}
          {(record.channel_ids || []).length > 0 && (
            <Text size='small'>
              {t('渠道')}:{' '}
              {record.channel_ids.map((id) => getChannelName(id)).join(', ')}
            </Text>
          )}
          {!(record.group_regex || []).length &&
            !(record.path_regex || []).length &&
            !(record.channel_ids || []).length && (
              <Text type='tertiary'>{t('全部请求')}</Text>
            )}
        </Space>
      ),
    },
    {
      title: t('UA 白名单'),
      dataIndex: 'ua_allow',
      render: (list) => renderTagList(list, 'green'),
    },
    {
      title: t('UA 黑名单'),
      dataIndex: 'ua_deny',
      render: (list) => renderTagList(list, 'red'),
    },
    {
      title: t('拒绝状态码'),
      dataIndex: 'status_code',
      render: (value) => <Text>{value || 403}</Text>,
    },
    {
      title: t('操作'),
      render: (_, record) => (
        <Space>
          <Button
            icon={<IconEdit />}
            theme='borderless'
            onClick={() => openModal(record)}
          />
          <Button
            icon={<IconDelete />}
            theme='borderless'
            type='danger'
            onClick={() =>
              updateRulesState(rules.filter((rule) => rule.id !== record.id))
            }
          />
        </Space>
      ),
    },
  ];

  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
          style={{ marginBottom: 15 }}
        >
          <Form.Section text={t('客户端 UA 规则')}>
            <Banner
              type='info'
              fullMode={false}
              description={t(
                '按请求的 User-Agent 限制哪些客户端可以调用指定模型、分组、路径或渠道，不符合的请求直接返回 4xx，不重试、不计费，也不计入错误防护封禁。拦截记录可在风控事件中查看。',
              )}
            />
            <Banner
              type='warning'
              fullMode={false}
              style={{ marginTop: 8 }}
              description={t(
                'User-Agent 由客户端自行填写，可以伪造，本规则只用于约束正常客户端，不能作为安全边界。建议先在风控设置中开启记录 UA，从使用日志确认各客户端的真实 UA；Claude Code 版本更新频繁，请按入口类型（如 "(external, cli)"）而不是版本号匹配。',
              )}
            />
            <Divider style={{ marginTop: 12, marginBottom: 12 }} />
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8}>
                <Form.Switch
                  field={KEY_ENABLED}
                  label={t('启用客户端 UA 规则')}
                  checkedText='|'
                  uncheckedText='O'
                  onChange={(value) =>
                    setInputs({ ...inputs, [KEY_ENABLED]: value })
                  }
                />
              </Col>
            </Row>

            <Row style={{ marginTop: 12, marginBottom: 12 }}>
              <Space>
                <Button
                  type={editMode === 'visual' ? 'primary' : 'tertiary'}
                  onClick={switchToVisualMode}
                >
                  {t('可视化编辑')}
                </Button>
                <Button
                  type={editMode === 'json' ? 'primary' : 'tertiary'}
                  onClick={switchToJsonMode}
                >
                  JSON
                </Button>
              </Space>
            </Row>

            {editMode === 'visual' ? (
              <>
                <Row style={{ marginBottom: 12 }}>
                  <Space>
                    <Button icon={<IconPlus />} onClick={() => openModal(null)}>
                      {t('新增规则')}
                    </Button>
                    <Text type='tertiary'>
                      {t(
                        '所有命中的规则都必须通过；按顺序第一条不通过的规则决定拒绝信息。',
                      )}
                    </Text>
                  </Space>
                </Row>
                <Table
                  dataSource={rules}
                  columns={columns}
                  pagination={false}
                  rowKey='id'
                  empty={t('暂无规则')}
                />
              </>
            ) : (
              <Form.TextArea
                field={KEY_RULES}
                label={t('客户端 UA 规则')}
                rows={18}
                placeholder={rulesExample}
                rules={[
                  {
                    validator: (rule, value) => verifyJSON(value || '[]'),
                    message: t('不是合法的 JSON 字符串'),
                  },
                ]}
                extraText={t(
                  '字段支持：name、disabled、model_regex、group_regex、path_regex、channel_ids、ua_allow、ua_deny、message、status_code。',
                )}
                onChange={(value) =>
                  setInputs({ ...inputs, [KEY_RULES]: value })
                }
              />
            )}

            <Row style={{ marginTop: 16 }}>
              <Button onClick={onSubmit}>{t('保存客户端 UA 规则')}</Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>

      <Modal
        title={editingRule ? t('编辑规则') : t('新增规则')}
        visible={modalVisible}
        onCancel={() => {
          setModalVisible(false);
          setEditingRule(null);
        }}
        onOk={handleModalSave}
        width={800}
      >
        <Form
          key={editingRule ? `edit-${editingRule.id}` : `add-${rules.length}`}
          initValues={{
            name: editingRule?.name || '',
            disabled: !!editingRule?.disabled,
            ua_allow_text: (editingRule?.ua_allow || []).join('\n'),
            ua_deny_text: (editingRule?.ua_deny || []).join('\n'),
            message: editingRule?.message || '',
            status_code: editingRule?.status_code || undefined,
          }}
          getFormApi={(formAPI) => (modalFormRef.current = formAPI)}
          labelPosition='top'
        >
          <Form.Input
            field='name'
            label={t('规则名称')}
            placeholder={t('例如：Opus 仅限 Claude Code CLI')}
            rules={[{ required: true, message: t('名称不能为空') }]}
          />
          <Text
            type='tertiary'
            size='small'
            style={{ display: 'block', marginBottom: 12 }}
          >
            {t('以下条件留空表示不限；全部满足时规则才生效。')}
          </Text>
          <div style={{ marginBottom: 12 }}>
            <ModelNameMatcher
              value={selectorModelRegex}
              onChange={setSelectorModelRegex}
              required={false}
            />
          </div>
          <div style={{ marginBottom: 12 }}>
            <GroupSelector
              value={selectorGroupRegex}
              onChange={setSelectorGroupRegex}
            />
          </div>
          <div style={{ marginBottom: 12 }}>
            <PathSelector
              value={selectorPathRegex}
              onChange={setSelectorPathRegex}
            />
          </div>
          <div style={{ marginBottom: 12 }}>
            <ChannelSelector
              value={selectorChannelIds}
              onChange={setSelectorChannelIds}
              compact
              label={t('限定渠道')}
              modelNames={editingModelNames}
            />
            <Text
              type='tertiary'
              size='small'
              style={{ display: 'block', marginTop: 4 }}
            >
              {t(
                '选择渠道后，仅当请求被分配到这些渠道时才校验 UA，不符合的客户端直接被拒绝，不会改换其他渠道。',
              )}
            </Text>
          </div>

          <Space style={{ marginBottom: 8 }}>
            <Text size='small' strong>
              {t('快速模板')}
            </Text>
            <Button size='small' onClick={() => applyPreset('cli-only')}>
              {t('仅允许 Claude Code CLI')}
            </Button>
            <Button size='small' onClick={() => applyPreset('deny-sdk-cli')}>
              {t('禁止 Claude Code SDK 模式')}
            </Button>
          </Space>
          <Form.TextArea
            field='ua_allow_text'
            label={t('UA 白名单（每行一条）')}
            rows={3}
            placeholder={'(external, cli)\nre:^claude-cli/'}
            extraText={t(
              '非空时 UA 必须命中其中一条。默认按不区分大小写的子串匹配；以 re: 开头按正则匹配（不区分大小写）。空 UA 不会命中任何白名单。',
            )}
          />
          <Form.TextArea
            field='ua_deny_text'
            label={t('UA 黑名单（每行一条）')}
            rows={3}
            placeholder={'(external, sdk-cli)'}
            extraText={t('命中任一条即拒绝，优先于白名单。')}
          />
          <Row gutter={16}>
            <Col span={16}>
              <Form.Input
                field='message'
                label={t('拒绝提示')}
                placeholder={t('自定义报错信息（可选，留空使用默认）')}
              />
            </Col>
            <Col span={8}>
              <Form.InputNumber
                field='status_code'
                label={t('拒绝状态码')}
                min={400}
                max={499}
                precision={0}
                placeholder='403'
              />
            </Col>
          </Row>
          <Form.Switch field='disabled' label={t('停用此规则')} />
        </Form>
      </Modal>
    </>
  );
}
