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
  Input,
  InputNumber,
  Modal,
  Radio,
  RadioGroup,
  Space,
  Spin,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess, timestamp2string } from '../../helpers';
import RiskUserState from './RiskUserState';
import { RISK_MAX_BAN_MINUTES, riskBanDurations } from './riskLabels';

const RiskDispositionModal = ({ target, onCancel, onSuccess }) => {
  const { t } = useTranslation();
  const [value, setValue] = useState(target.value || '');
  const [reason, setReason] = useState('');
  const [duration, setDuration] = useState(0);
  const [minutes, setMinutes] = useState(120);
  const [state, setState] = useState(null);
  const [validatedValue, setValidatedValue] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const isUser = target.type === 'user';
  const isRelease = target.action === 'enable' || target.action === 'unban_ip';
  // 同一目标已有临时封禁时,这次操作只能延长它。
  const exactBan = state?.ip_bans?.find((ban) => ban.target === state.value);
  const extending = !isUser && !isRelease && exactBan?.expires_at > 0;
  const title = isUser
    ? isRelease
      ? t('启用用户')
      : state?.user?.status === 2
        ? t('转为永久禁用')
        : t('永久禁用用户')
    : isRelease
      ? t('解除封禁')
      : extending
        ? t('延长封禁')
        : t('封禁 IP');
  const okText = isUser
    ? isRelease
      ? t('确认启用')
      : t('确认禁用')
    : isRelease
      ? t('确认解除')
      : t('确认封禁');

  useEffect(() => {
    setState(null);
    setValidatedValue('');
    setError('');
    if (!value.trim()) {
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    setLoading(true);
    const timer = setTimeout(async () => {
      try {
        const params = new URLSearchParams({
          type: target.type,
          value: value.trim(),
        });
        const res = await API.get(`/api/risk/state?${params}`, {
          signal: controller.signal,
          skipErrorHandler: true,
        });
        if (controller.signal.aborted) return;
        if (!res.data.success) throw new Error(res.data.message);
        setState(res.data.data);
        setValidatedValue(value.trim());
      } catch (e) {
        if (!controller.signal.aborted)
          setError(e.response?.data?.message || e.message);
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    }, 200);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [target.type, value]);

  const banMinutes = duration === 'custom' ? minutes : duration;
  const customInvalid =
    duration === 'custom' &&
    (!Number.isInteger(minutes) ||
      minutes < 1 ||
      minutes > RISK_MAX_BAN_MINUTES);
  const permitted = isUser
    ? isRelease
      ? state?.user?.can_enable
      : state?.user?.can_disable
    : isRelease
      ? state?.ip_bans?.some((ban) => ban.id === target.banId)
      : !state?.is_current_ip;

  const confirm = async () => {
    if (!state || !permitted || saving || validatedValue !== value.trim())
      return;
    if (!isUser && !isRelease && customInvalid) {
      showError(t('临时封禁时长必须为 1 到 43200 分钟'));
      return;
    }
    setSaving(true);
    try {
      let res;
      if (isUser) {
        res = await API.post(
          '/api/user/manage',
          { id: Number(value), action: target.action, reason: reason.trim() },
          { skipErrorHandler: true },
        );
      } else if (isRelease) {
        res = await API.delete(`/api/risk/ip_bans/${target.banId}`, {
          skipErrorHandler: true,
        });
      } else {
        res = await API.post(
          '/api/risk/ip_bans',
          {
            target: value.trim(),
            reason: reason.trim(),
            expire_minutes: banMinutes,
          },
          { skipErrorHandler: true },
        );
      }
      if (!res.data.success) throw new Error(res.data.message);
      showSuccess(t('操作成功'));
      onSuccess();
    } catch (e) {
      showError(e.response?.data?.message || e.message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      title={title}
      width={520}
      visible
      onOk={confirm}
      onCancel={onCancel}
      okText={okText}
      okType={isRelease ? 'primary' : 'danger'}
      confirmLoading={saving}
      okButtonProps={{
        'aria-label': okText,
        disabled:
          loading || !state || !permitted || validatedValue !== value.trim(),
      }}
      cancelButtonProps={{ disabled: saving, 'aria-label': t('取消') }}
      closable={!saving}
      maskClosable={false}
    >
      <Space vertical align='start' className='w-full'>
        {target.editable ? (
          <Input
            value={value}
            onChange={setValue}
            placeholder={t('请输入要封禁的 IP 或 CIDR')}
            disabled={saving}
          />
        ) : (
          <Typography.Text strong>
            {state?.user
              ? `${state.user.username} (#${state.user.id})`
              : target.label
                ? `${target.label} (#${value})`
                : value}
          </Typography.Text>
        )}
        <Spin spinning={loading}>
          {state?.user && <RiskUserState state={state.user} />}
          {state?.ip_bans && (
            <Typography.Text>
              {t('当前生效封禁数')}: {state.ip_bans.length}
            </Typography.Text>
          )}
        </Spin>
        {error && <Banner type='danger' description={error} />}
        {state && !permitted && (
          <Banner
            type='warning'
            description={
              isUser
                ? t('当前状态或权限不允许此操作')
                : isRelease
                  ? t('该封禁已失效或已解除')
                  : t('不能封禁当前请求来源的 IP')
            }
          />
        )}
        {isUser && !isRelease && (
          <Banner
            type='warning'
            description={t(
              '该账号将永久禁用，直到管理员手动启用；其令牌将无法继续调用。',
            )}
          />
        )}
        {!isUser && !isRelease && (
          <>
            <Typography.Text>{t('封禁期限')}</Typography.Text>
            <RadioGroup
              type='button'
              value={duration}
              onChange={(event) => setDuration(event.target.value)}
              disabled={saving}
              aria-label={t('封禁期限')}
            >
              {riskBanDurations.map((preset) => (
                <Radio key={preset.minutes} value={preset.minutes}>
                  {t(preset.label)}
                </Radio>
              ))}
              <Radio value='custom'>{t('自定义')}</Radio>
            </RadioGroup>
            <Space wrap>
              {duration === 'custom' && (
                <InputNumber
                  value={minutes}
                  onChange={setMinutes}
                  min={1}
                  max={RISK_MAX_BAN_MINUTES}
                  suffix={t('分钟')}
                  disabled={saving}
                />
              )}
              <Typography.Text type='tertiary'>
                {customInvalid
                  ? t('临时封禁时长必须为 1 到 43200 分钟')
                  : banMinutes
                    ? `${t('到期时间')}: ${timestamp2string(
                        Math.floor(Date.now() / 1000) + banMinutes * 60,
                      )}`
                    : t('永久封禁，需手动解除')}
              </Typography.Text>
            </Space>
            <Typography.Text type='tertiary'>
              {t(
                '该 IP 或网段的请求将被拦截；已认证的全局白名单账号仍按豁免规则处理。',
              )}
            </Typography.Text>
          </>
        )}
        {state?.blacklist_blocked && (
          <Banner
            type='warning'
            description={t('静态黑名单仍在生效，解除动态封禁后仍会被拦截。')}
          />
        )}
        {!isUser && !isRelease && state?.ip_bans?.length > 0 && (
          <Banner
            type='info'
            description={
              exactBan
                ? t(
                    '该目标已在封禁中（{{expires}}）。封禁只会延长，选择更短的期限不会生效。',
                    {
                      expires: exactBan.expires_at
                        ? t('到 {{time}} 解除', {
                            time: timestamp2string(exactBan.expires_at),
                          })
                        : t('永久'),
                    },
                  )
                : t('已存在覆盖该目标的封禁，新记录不会解除或缩短已有封禁。')
            }
          />
        )}
        {!isRelease || isUser ? (
          <TextArea
            value={reason}
            onChange={setReason}
            disabled={saving}
            rows={3}
            placeholder={isRelease ? t('解禁原因(可选)') : t('封禁原因(可选)')}
          />
        ) : null}
        <Typography.Text type='tertiary'>
          {t('操作将写入风控事件，完成后自动刷新当前状态。')}
        </Typography.Text>
      </Space>
    </Modal>
  );
};

export default RiskDispositionModal;
