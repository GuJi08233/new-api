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
  Button,
  Empty,
  Input,
  List,
  Modal,
  Space,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconSearch } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API } from '../../helpers';
import RiskUserState from './RiskUserState';

const { Text } = Typography;

// 按账号 ID 或用户名查找账号,选中后返回该账号。豁免名单这类按 ID 维护的配置用它挑人,
// 免得手工查 ID 填错人;每个结果都带当前状态,便于确认。
export async function searchRiskUsers(keyword) {
  const query = keyword.trim();
  if (!query) return [];
  let users = [];
  if (/^[1-9]\d*$/.test(query)) {
    users = [{ id: Number(query) }];
  } else {
    const res = await API.get(
      `/api/user/search?keyword=${encodeURIComponent(query)}&p=1&page_size=20`,
      { skipErrorHandler: true },
    );
    if (!res.data?.success) throw new Error(res.data?.message);
    users = (res.data.data?.items || []).filter((user) => user?.id);
  }
  if (users.length === 0) return [];
  const res = await API.get(
    `/api/risk/users?ids=${users.map((user) => user.id).join(',')}`,
    { skipErrorHandler: true },
  );
  if (!res.data?.success) throw new Error(res.data?.message);
  const states = res.data.data || {};
  return users
    .filter((user) => states[user.id])
    .map((user) => ({
      ...user,
      username: states[user.id].username,
      state: states[user.id],
    }));
}

const RiskUserPicker = ({
  visible,
  title,
  excludeIds = [],
  onCancel,
  onSelect,
}) => {
  const { t } = useTranslation();
  const [keyword, setKeyword] = useState('');
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState('');

  useEffect(() => {
    if (!visible) return;
    setKeyword('');
    setItems([]);
    setMessage('');
  }, [visible]);

  const search = async () => {
    setLoading(true);
    setMessage('');
    try {
      const found = (await searchRiskUsers(keyword)).filter(
        (user) => !excludeIds.includes(user.id),
      );
      setItems(found);
      if (found.length === 0) setMessage(t('没有匹配的账号'));
    } catch (e) {
      setItems([]);
      setMessage(e.response?.data?.message || e.message);
    }
    setLoading(false);
  };

  return (
    <Modal
      title={title || t('选择账号')}
      visible={visible}
      onCancel={onCancel}
      footer={null}
      width={560}
    >
      <Space className='w-full mb-3'>
        <Input
          value={keyword}
          onChange={setKeyword}
          onEnterPress={search}
          placeholder={t('用户 ID 或用户名')}
          aria-label={t('用户 ID 或用户名')}
          style={{ width: 360 }}
        />
        <Button
          icon={<IconSearch />}
          loading={loading}
          onClick={search}
          aria-label={t('查询')}
        >
          {t('查询')}
        </Button>
      </Space>
      {message && (
        <Text type='tertiary' size='small' className='block mb-2'>
          {message}
        </Text>
      )}
      <List
        dataSource={items}
        emptyContent={<Empty description={t('输入后按回车查询')} />}
        renderItem={(item) => (
          <List.Item
            main={
              <Space spacing={4} wrap>
                <Text strong>{item.username || '-'}</Text>
                <Text type='tertiary' size='small'>
                  #{item.id}
                </Text>
                {item.display_name && (
                  <Tag size='small'>{item.display_name}</Tag>
                )}
                <RiskUserState state={item.state} />
              </Space>
            }
            extra={
              <Button
                size='small'
                theme='solid'
                onClick={() => {
                  onSelect(item);
                  onCancel();
                }}
              >
                {t('选择')}
              </Button>
            }
          />
        )}
      />
    </Modal>
  );
};

export default RiskUserPicker;
