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

import React, { useContext, useEffect, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  API,
  showError,
  showSuccess,
  updateAPI,
  setUserData,
} from '../../helpers';
import { UserContext } from '../../context/User';
import Loading from '../common/ui/Loading';
import { Button, Card, Input } from '@douyinfe/semi-ui';
import { IconKey } from '@douyinfe/semi-icons';
import Title from '@douyinfe/semi-ui/lib/es/typography/title';
import Text from '@douyinfe/semi-ui/lib/es/typography/text';

const OAuth2Callback = (props) => {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const [, userDispatch] = useContext(UserContext);
  const navigate = useNavigate();
  // 账号未注册且需要邀请码时，切换为补填邀请码界面
  const [needInvitationCode, setNeedInvitationCode] = useState(false);
  const [invitationCode, setInvitationCode] = useState('');
  const [submitting, setSubmitting] = useState(false);

  // 防止 React 18 Strict Mode 下重复执行
  const hasExecuted = useRef(false);

  // 最大重试次数
  const MAX_RETRIES = 3;

  const loginSuccess = (data) => {
    userDispatch({ type: 'login', payload: data });
    localStorage.setItem('user', JSON.stringify(data));
    setUserData(data);
    updateAPI();
  };

  const sendCode = async (code, state, retry = 0) => {
    try {
      const { data: resData } = await API.get(
        `/api/oauth/${props.type}?code=${encodeURIComponent(code)}&state=${encodeURIComponent(state)}`,
      );

      const { success, message, data } = resData;

      if (!success) {
        if (data?.reason === 'invitation_code_required') {
          // 账号未注册：身份已由后端暂存，补填邀请码即可完成注册，无需重新授权
          setInvitationCode(localStorage.getItem('invitation_code') || '');
          setNeedInvitationCode(true);
          return;
        }
        // 业务错误不重试，显示错误并离开回调页
        showError(message || t('授权失败'));
        navigate(localStorage.getItem('user') ? '/console/personal' : '/login');
        return;
      }

      if (data?.action === 'bind') {
        showSuccess(t('绑定成功！'));
        navigate('/console/personal');
      } else {
        loginSuccess(data);
        showSuccess(t('登录成功！'));
        navigate('/console/token');
      }
    } catch (error) {
      // 网络错误等可重试
      if (retry < MAX_RETRIES) {
        // 递增的退避等待
        await new Promise((resolve) => setTimeout(resolve, (retry + 1) * 2000));
        return sendCode(code, state, retry + 1);
      }

      // 重试次数耗尽，提示错误并返回设置页面
      showError(error.message || t('授权失败'));
      navigate('/console/personal');
    }
  };

  const submitInvitationCode = async () => {
    const code = invitationCode.trim();
    if (!code) {
      showError(t('请输入邀请码'));
      return;
    }
    setSubmitting(true);
    try {
      const res = await API.post('/api/oauth/complete_registration', {
        invitation_code: code,
      });
      const { success, message, data } = res.data;
      if (success) {
        // 邀请码已被消费，避免残留给同浏览器的下一位注册者
        localStorage.removeItem('invitation_code');
        loginSuccess(data);
        showSuccess(t('注册成功！'));
        navigate('/console/token');
        return;
      }
      if (data?.reason === 'invitation_code_required') {
        // 邀请码无效/已被使用：留在表单允许换码重试
        showError(message || t('邀请码无效'));
        return;
      }
      showError(message || t('注册失败，请重试'));
      navigate('/login');
    } catch (error) {
      showError(t('注册失败，请重试'));
    } finally {
      setSubmitting(false);
    }
  };

  useEffect(() => {
    // 防止 React 18 Strict Mode 下重复执行
    if (hasExecuted.current) {
      return;
    }
    hasExecuted.current = true;

    const code = searchParams.get('code');
    const state = searchParams.get('state');
    const isOpenID = searchParams.get('openid.mode');

    if (isOpenID) {
      // Steam OpenID: 整个 query string 作为 code，state 从 URL 中提取
      const rawQuery = window.location.search.substring(1);
      sendCode(rawQuery, state);
    } else if (code) {
      sendCode(code, state);
    } else {
      showError(t('未获取到授权码'));
      navigate('/console/personal');
    }
  }, []);

  if (needInvitationCode) {
    return (
      <div className='relative overflow-hidden bg-gray-100 flex items-center justify-center py-12 px-4 sm:px-6 lg:px-8'>
        <div className='w-full max-w-sm mt-[60px]'>
          <Card className='border-0 !rounded-2xl overflow-hidden'>
            <div className='flex justify-center pt-6 pb-2'>
              <Title heading={3} className='text-gray-800 dark:text-gray-200'>
                {t('完成注册')}
              </Title>
            </div>
            <div className='px-2 py-8'>
              <Text className='block text-center mb-6 text-gray-600'>
                {t('该账号尚未注册，填写邀请码即可完成注册')}
              </Text>
              <Input
                value={invitationCode}
                onChange={(value) => setInvitationCode(value)}
                placeholder={t('请输入邀请码')}
                prefix={<IconKey />}
                size='large'
                onEnterPress={submitInvitationCode}
              />
              <Button
                theme='solid'
                type='primary'
                className='w-full h-12 mt-4 !rounded-full'
                loading={submitting}
                onClick={submitInvitationCode}
              >
                {t('完成注册')}
              </Button>
              <Button
                theme='borderless'
                type='tertiary'
                className='w-full mt-2 !rounded-full'
                onClick={() => navigate('/login')}
              >
                {t('返回登录')}
              </Button>
            </div>
          </Card>
        </div>
      </div>
    );
  }

  return <Loading />;
};

export default OAuth2Callback;
