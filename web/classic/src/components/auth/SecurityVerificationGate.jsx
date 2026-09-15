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

import { useEffect, useRef, useState } from 'react';
import {
  Button,
  Input,
  Modal,
  Select,
  Space,
  Typography,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import TelegramLoginButton from 'react-telegram-login';
import { API } from '../../helpers/api';
import {
  buildAssertionResult,
  prepareCredentialRequestOptions,
} from '../../helpers/passkey';
import {
  registerSecurityVerification,
  securityOAuthUrl,
} from '../../services/securityChallenge';

// 每次仅处理一个具体请求。证明只传给该请求，不写入全局请求头或持久化存储。
export default function SecurityVerificationGate() {
  const { t } = useTranslation();
  const pending = useRef(null);
  const popup = useRef(null);
  const [challenge, setChallenge] = useState(null);
  const [methods, setMethods] = useState([]);
  const [method, setMethod] = useState('');
  const [telegramBotName, setTelegramBotName] = useState('');
  const [code, setCode] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const finish = (proof, failure) => {
    const request = pending.current;
    pending.current = null;
    popup.current?.window?.close();
    popup.current = null;
    setChallenge(null);
    setCode('');
    setLoading(false);
    if (failure) request?.reject(failure);
    else request?.resolve(proof);
  };

  useEffect(() => {
    const unregister = registerSecurityVerification(
      (request) =>
        new Promise((resolve, reject) => {
          if (pending.current) {
            reject(new Error(t('请先完成当前安全验证')));
            return;
          }
          const entry = { resolve, reject };
          pending.current = entry;
          setChallenge(request);
          setCode('');
          setError('');
          setLoading(true);
          API.get('/api/verify/requirements', { skipErrorHandler: true })
            .then(({ data }) => {
              if (pending.current !== entry) return;
              if (!data.success) throw new Error(data.message);
              const available = [];
              if (data.data.has2FA)
                available.push({ value: '2fa', label: t('两步验证') });
              if (data.data.hasPasskey && window.PublicKeyCredential)
                available.push({ value: 'passkey', label: 'Passkey' });
              if (data.data.hasPassword)
                available.push({ value: 'password', label: t('密码') });
              if (data.data.hasTelegram) {
                available.push({ value: 'telegram', label: 'Telegram' });
                setTelegramBotName(data.data.telegramBotName);
              }
              if (data.data.hasWeChat)
                available.push({ value: 'wechat', label: t('微信验证码') });
              if (!data.data.has2FA && !data.data.hasPasskey) {
                for (const provider of data.data.oauthProviders || [])
                  available.push({
                    value: `oauth:${provider.slug}`,
                    label: provider.name,
                  });
              }
              setMethods(available);
              setMethod(available[0]?.value || '');
              if (!available.length)
                setError(t('没有可用的验证方式，请联系管理员'));
            })
            .catch((failure) => {
              if (pending.current === entry) setError(failure.message);
            })
            .finally(() => {
              if (pending.current === entry) setLoading(false);
            });
        }),
    );
    const receive = (event) => {
      const current = popup.current;
      if (
        !current ||
        event.origin !== window.location.origin ||
        event.source !== current.window ||
        event.data?.type !== 'security-verification' ||
        event.data.state !== current.state
      )
        return;
      if (typeof event.data.proof !== 'string') return;
      finish(event.data.proof);
    };
    window.addEventListener('message', receive);
    const timer = window.setInterval(() => {
      if (popup.current?.window?.closed) {
        popup.current = null;
        setLoading(false);
        setError(t('安全验证已取消'));
      }
    }, 500);
    return () => {
      unregister();
      window.clearInterval(timer);
      window.removeEventListener('message', receive);
      pending.current?.reject(new Error('安全验证已取消'));
      popup.current?.window?.close();
    };
  }, [t]);

  const verify = async (telegramCode) => {
    setLoading(true);
    setError('');
    const entry = pending.current;
    const operation = {
      scope: challenge.scope,
      context_hash: challenge.context_hash,
    };
    try {
      if (method.startsWith('oauth:')) {
        const child = window.open(
          'about:blank',
          'security-verification',
          'popup,width=560,height=720',
        );
        if (!child) throw new Error(t('请允许浏览器打开验证窗口'));
        popup.current = { window: child, state: '' };
        const slug = method.slice(6);
        const [stateResponse, statusResponse] = await Promise.all([
          API.get('/api/oauth/state', {
            params: { ...operation, provider: slug, verification: 'true' },
            skipErrorHandler: true,
          }),
          API.get('/api/status'),
        ]);
        if (!stateResponse.data.success)
          throw new Error(stateResponse.data.message);
        if (pending.current !== entry || !popup.current || child.closed) return;
        popup.current.state = stateResponse.data.data;
        child.location.replace(
          securityOAuthUrl(
            slug,
            stateResponse.data.data,
            statusResponse.data.data,
          ),
        );
        return;
      }
      let response;
      if (method === 'passkey') {
        const begin = await API.post(
          '/api/user/passkey/verify/begin',
          operation,
          { skipErrorHandler: true },
        );
        if (!begin.data.success) throw new Error(begin.data.message);
        const assertion = await navigator.credentials.get({
          publicKey: prepareCredentialRequestOptions(begin.data.data.options),
        });
        if (!assertion) throw new Error(t('安全验证已取消'));
        response = await API.post(
          '/api/user/passkey/verify/finish',
          buildAssertionResult(assertion),
          { skipErrorHandler: true },
        );
      } else {
        response = await API.post(
          '/api/verify',
          {
            ...operation,
            method,
            code: method === 'telegram' ? telegramCode : code,
            password: method === 'password' ? code : undefined,
          },
          { skipErrorHandler: true },
        );
      }
      if (!response.data.success || !response.data.data?.proof_token)
        throw new Error(response.data.message || t('验证失败'));
      if (pending.current === entry) finish(response.data.data.proof_token);
    } catch (failure) {
      if (pending.current !== entry) return;
      popup.current?.window?.close();
      popup.current = null;
      setError(
        failure.response?.data?.message || failure.message || t('验证失败'),
      );
      setLoading(false);
    }
  };

  return (
    <Modal
      title={t('安全验证')}
      visible={!!challenge}
      onCancel={() => finish(null, new Error(t('安全验证已取消')))}
      footer={null}
      width={420}
    >
      <Space vertical align='start' style={{ width: '100%' }}>
        <Typography.Text>{t('请验证当前账户身份以继续操作')}</Typography.Text>
        <Select
          optionList={methods}
          value={method}
          onChange={(value) => {
            setMethod(value);
            setCode('');
          }}
          style={{ width: '100%' }}
          disabled={loading}
        />
        {method &&
          !method.startsWith('oauth:') &&
          method !== 'passkey' &&
          method !== 'telegram' && (
            <Input
              mode={method === 'password' ? 'password' : 'input'}
              value={code}
              onChange={setCode}
              autoComplete={
                method === 'password' ? 'current-password' : 'one-time-code'
              }
              placeholder={t(
                method === 'password' ? '请输入密码' : '请输入验证码或备用码',
              )}
              onEnterPress={() => {
                if (!loading && code) verify();
              }}
            />
          )}
        {method === 'telegram' && (
          <TelegramLoginButton
            botName={telegramBotName}
            dataOnauth={(payload) =>
              verify(new URLSearchParams(payload).toString())
            }
          />
        )}
        {!!error && <Typography.Text type='danger'>{error}</Typography.Text>}
        <Button
          theme='solid'
          loading={loading}
          disabled={
            !method ||
            (!code && method !== 'passkey' && !method.startsWith('oauth:'))
          }
          onClick={() => verify()}
        >
          {t('验证')}
        </Button>
      </Space>
    </Modal>
  );
}
