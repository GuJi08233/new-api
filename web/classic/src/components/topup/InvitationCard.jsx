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

import React, {
  useState,
  useEffect,
  useContext,
  useMemo,
  useCallback,
} from 'react';
import {
  Avatar,
  Typography,
  Card,
  Button,
  Space,
  Table,
  Tag,
  Modal,
  Form,
  InputNumber,
  Input,
} from '@douyinfe/semi-ui';
import { Copy, Plus, RefreshCcw, Ticket, Trash2 } from 'lucide-react';
import {
  API,
  showError,
  showInfo,
  showSuccess,
  renderQuota,
} from '../../helpers';
import { StatusContext } from '../../context/Status';

const { Text } = Typography;

const DEFAULT_PAGE_SIZE = 10;
const DEFAULT_BATCH_COUNT = 10;

const STATUS_MAP = {
  1: { text: '未使用', color: 'green' },
  2: { text: '已使用', color: 'grey' },
  3: { text: '已禁用', color: 'red' },
};

const InvitationCard = ({ t }) => {
  const [statusState] = useContext(StatusContext);
  const status = useMemo(() => {
    if (statusState?.status) return statusState.status;
    const saved = localStorage.getItem('status');
    if (!saved) return {};
    try {
      return JSON.parse(saved) || {};
    } catch {
      return {};
    }
  }, [statusState?.status]);

  const invitationCodeEnabled = !!status?.invitation_code_enabled;
  // 生成邀请码的单价与使用者奖励比例,用于生成前的消耗确认
  const invitationCodePrice = Number(status?.invitation_code_price) || 0;
  const invitationRewardRatio =
    Number(status?.invitation_code_reward_ratio) || 0;

  // 生成确认弹窗:展示本次消耗与使用者可得,避免用户在不知情下被扣费
  const confirmGenerate = ({ count, maxUses, onConfirm }) => {
    const uses = maxUses > 0 ? maxUses : 1;
    const totalCost = invitationCodePrice * count * uses;
    if (totalCost <= 0) {
      onConfirm();
      return;
    }
    const rewardPerUse =
      invitationRewardRatio > 0
        ? Math.floor((invitationCodePrice * invitationRewardRatio) / 100)
        : invitationCodePrice;
    Modal.confirm({
      title: t('确认生成邀请码'),
      content: (
        <div className='text-sm'>
          <p>
            {t('生成数量')}：<strong>{count}</strong>
          </p>
          {uses > 1 && (
            <p>
              {t('每个可用次数')}：<strong>{uses}</strong>
            </p>
          )}
          <p style={{ color: 'var(--semi-color-danger)' }}>
            {t('本次将从您的额度中扣除')}：
            <strong>{renderQuota(totalCost)}</strong>
          </p>
          <p style={{ color: 'var(--semi-color-text-2)' }}>
            {t('每次被使用后,使用者可获得')} {renderQuota(rewardPerUse)}
          </p>
        </div>
      ),
      okText: t('确认生成'),
      cancelText: t('取消'),
      onOk: onConfirm,
    });
  };

  const [myCodes, setMyCodes] = useState([]);
  const [myCodesLoading, setMyCodesLoading] = useState(false);
  const [generateLoading, setGenerateLoading] = useState(false);
  const [batchGenerateLoading, setBatchGenerateLoading] = useState(false);
  const [batchActionLoading, setBatchActionLoading] = useState('');
  const [activePage, setActivePage] = useState(1);
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE);
  const [total, setTotal] = useState(0);
  const [selectedRowKeys, setSelectedRowKeys] = useState([]);
  const [showBatchModal, setShowBatchModal] = useState(false);
  const [batchCount, setBatchCount] = useState(DEFAULT_BATCH_COUNT);
  const [batchMaxUses, setBatchMaxUses] = useState(1);
  const [batchRemark, setBatchRemark] = useState('');

  const loadMyCodes = useCallback(
    async (page = activePage, size = pageSize) => {
      setMyCodesLoading(true);
      try {
        const res = await API.get(
          `/api/invitation_code/mine?p=${page}&page_size=${size}`,
        );
        const { success, message, data } = res.data;
        if (!success) {
          showError(message);
          return;
        }
        const items = data.items || [];
        const nextTotal = data.total || 0;
        setMyCodes(items);
        setTotal(nextTotal);
        setSelectedRowKeys((prev) =>
          prev.filter((id) => items.some((item) => item.id === id)),
        );
        if (items.length === 0 && nextTotal > 0 && page > 1) {
          setActivePage(page - 1);
        }
      } catch (e) {
        showError(t('加载失败'));
      } finally {
        setMyCodesLoading(false);
      }
    },
    [activePage, pageSize, t],
  );

  useEffect(() => {
    if (invitationCodeEnabled) {
      loadMyCodes(activePage, pageSize);
    }
  }, [activePage, invitationCodeEnabled, loadMyCodes, pageSize]);

  const selectedCodes = useMemo(
    () => myCodes.filter((item) => selectedRowKeys.includes(item.id)),
    [myCodes, selectedRowKeys],
  );

  const buildInvitationLink = (code) =>
    `${window.location.origin}/register?invitation_code=${code}`;

  const copyInvitationCodes = async (codes) => {
    const usableCodes = codes.filter((item) => item.status === 1);
    if (usableCodes.length === 0) {
      showInfo(t('暂无可复制的未使用邀请码'));
      return;
    }

    const text = usableCodes.map((item) => item.code).join('\n');
    try {
      await navigator.clipboard.writeText(text);
      showSuccess(t('已复制到剪贴板'));
    } catch (e) {
      showError(t('复制失败'));
    }
  };

  const copyInvitationLinks = async (codes) => {
    const usableCodes = codes.filter((item) => item.status === 1);
    if (usableCodes.length === 0) {
      showInfo(t('暂无可复制的邀请码'));
      return;
    }

    const text = usableCodes
      .map((item) => buildInvitationLink(item.code))
      .join('\n');
    try {
      await navigator.clipboard.writeText(text);
      showSuccess(usableCodes.length > 1 ? t('复制成功') : t('已复制到剪贴板'));
    } catch (e) {
      showError(t('复制失败'));
    }
  };

  const fetchAllMyInvitationCodes = async () => {
    const size = 100;
    let page = 1;
    let totalCount = 0;
    const items = [];

    while (page === 1 || items.length < totalCount) {
      const res = await API.get(
        `/api/invitation_code/mine?p=${page}&page_size=${size}`,
        { disableDuplicate: true },
      );
      const { success, message, data } = res.data;
      if (!success) {
        throw new Error(message || t('加载失败'));
      }
      const pageItems = data.items || [];
      totalCount = data.total || 0;
      items.push(...pageItems);
      if (pageItems.length === 0) {
        break;
      }
      page += 1;
    }

    return items;
  };

  const refreshCurrentPage = useCallback(() => {
    loadMyCodes(activePage, pageSize);
  }, [activePage, loadMyCodes, pageSize]);

  const handleGenerate = () => {
    confirmGenerate({ count: 1, maxUses: 1, onConfirm: doGenerate });
  };

  const doGenerate = async () => {
    setGenerateLoading(true);
    try {
      const res = await API.post('/api/invitation_code/generate', {
        count: 1,
      });
      const { success, message } = res.data;
      if (!success) {
        showError(message);
        return;
      }
      showSuccess(t('邀请码生成成功'));
      if (activePage === 1) {
        refreshCurrentPage();
      } else {
        setActivePage(1);
      }
    } catch (e) {
      showError(t('生成失败'));
    } finally {
      setGenerateLoading(false);
    }
  };

  const handleBatchGenerate = () => {
    if (batchCount <= 0 || batchCount > 100) {
      showError(t('数量必须在 1-100 之间'));
      return;
    }
    if (batchMaxUses <= 0 || batchMaxUses > 1000) {
      showError(t('可用次数需在 1-1000 之间'));
      return;
    }
    confirmGenerate({
      count: batchCount,
      maxUses: batchMaxUses,
      onConfirm: doBatchGenerate,
    });
  };

  const doBatchGenerate = async () => {
    setBatchGenerateLoading(true);
    try {
      const res = await API.post('/api/invitation_code/generate', {
        count: batchCount,
        max_uses: batchMaxUses,
        remark: batchRemark,
      });
      const { success, message } = res.data;
      if (!success) {
        showError(message);
        return;
      }
      showSuccess(t('创建成功'));
      setShowBatchModal(false);
      setBatchCount(DEFAULT_BATCH_COUNT);
      setBatchMaxUses(1);
      setBatchRemark('');
      setSelectedRowKeys([]);
      if (activePage === 1) {
        refreshCurrentPage();
      } else {
        setActivePage(1);
      }
    } catch (e) {
      showError(t('创建失败'));
    } finally {
      setBatchGenerateLoading(false);
    }
  };

  const deleteCodes = async (ids) => {
    setBatchActionLoading('delete');
    try {
      const res = await API.post('/api/invitation_code/mine/batch_delete', {
        ids,
      });
      const { success, message } = res.data;
      if (!success) {
        showError(message);
        return;
      }
      showSuccess(t('删除成功'));
      setSelectedRowKeys((prev) => prev.filter((id) => !ids.includes(id)));
      refreshCurrentPage();
    } catch (e) {
      showError(t('删除失败'));
    } finally {
      setBatchActionLoading('');
    }
  };

  const confirmDeleteCodes = (ids, title) => {
    Modal.confirm({
      title,
      centered: true,
      onOk: () => deleteCodes(ids),
    });
  };

  const handleClearUsed = () => {
    Modal.confirm({
      title: t('确定要清理所有已使用的邀请码吗？'),
      centered: true,
      onOk: async () => {
        setBatchActionLoading('clear');
        try {
          const res = await API.post('/api/invitation_code/mine/delete_used');
          const { success, message } = res.data;
          if (!success) {
            showError(message);
            return;
          }
          showSuccess(t('清理成功'));
          setSelectedRowKeys([]);
          refreshCurrentPage();
        } catch (e) {
          showError(t('删除失败'));
        } finally {
          setBatchActionLoading('');
        }
      },
    });
  };

  const handleCopyAllUnusedCodes = async () => {
    setBatchActionLoading('copy_raw_all');
    try {
      const allCodes = await fetchAllMyInvitationCodes();
      await copyInvitationCodes(allCodes);
    } catch (e) {
      showError(e?.message || t('复制失败'));
    } finally {
      setBatchActionLoading('');
    }
  };

  const handleCopySelectedCodes = async () => {
    await copyInvitationCodes(selectedCodes);
  };

  const rowSelection = {
    selectedRowKeys,
    onChange: (keys) => setSelectedRowKeys(keys),
  };

  const columns = [
    {
      title: t('邀请码'),
      dataIndex: 'code',
      width: 300,
      render: (text, record) => (
        <div className='flex flex-col gap-1'>
          <Text copyable={{ content: text }} className='break-all'>
            {text}
          </Text>
          {record.remark ? (
            <Text type='tertiary' size='small'>
              {record.remark}
            </Text>
          ) : null}
        </div>
      ),
    },
    {
      title: t('状态'),
      dataIndex: 'status',
      width: 88,
      render: (statusValue) => {
        const info = STATUS_MAP[statusValue] || {
          text: t('未知'),
          color: 'grey',
        };
        return <Tag color={info.color}>{t(info.text)}</Tag>;
      },
    },
    {
      title: t('使用次数'),
      dataIndex: 'used_count',
      width: 90,
      render: (value, record) => {
        const maxUses = record.max_uses > 0 ? record.max_uses : 1;
        return (
          <Tag color={maxUses > 1 ? 'blue' : 'grey'}>
            {`${value || 0} / ${maxUses}`}
          </Tag>
        );
      },
    },
    {
      title: t('去向'),
      dataIndex: 'used_user_id',
      width: 170,
      render: (value, record) => {
        if (record.status !== 2) return '-';
        const typeTag =
          record.used_type === 1 ? (
            <Tag color='green'>{t('注册')}</Tag>
          ) : record.used_type === 2 ? (
            <Tag color='blue'>{t('兑换')}</Tag>
          ) : (
            <Tag color='grey'>{t('已使用')}</Tag>
          );
        const who = record.used_user_name
          ? `${record.used_user_name}${value ? ` (${value})` : ''}`
          : value || '';
        return (
          <Space size='small'>
            {typeTag}
            {who ? <Text size='small'>{who}</Text> : null}
          </Space>
        );
      },
    },
    {
      title: t('创建时间'),
      dataIndex: 'created_time',
      width: 180,
      render: (value) =>
        value ? new Date(value * 1000).toLocaleString() : '-',
    },
    {
      title: t('操作'),
      width: 190,
      render: (_, record) => (
        <Space wrap size='small'>
          <Button
            size='small'
            icon={<Copy size={12} />}
            disabled={record.status !== 1}
            onClick={() => copyInvitationLinks([record])}
          >
            {t('复制链接')}
          </Button>
          <Button
            size='small'
            type='danger'
            icon={<Trash2 size={12} />}
            onClick={() =>
              confirmDeleteCodes([record.id], t('确定要删除此邀请码吗？'))
            }
          />
        </Space>
      ),
    },
  ];

  if (!invitationCodeEnabled) {
    return null;
  }

  return (
    <Card className='!rounded-2xl shadow-sm border-0'>
      <div className='flex items-center mb-4'>
        <Avatar size='small' color='green' className='mr-3 shadow-md'>
          <Ticket size={16} />
        </Avatar>
        <div>
          <Typography.Text className='text-lg font-medium'>
            {t('我的邀请码')}
          </Typography.Text>
          <div className='text-xs'>
            {t('生成邀请码，可用于邀请好友注册或作为兑换码兑换额度')}
          </div>
        </div>
      </div>

      <Space vertical style={{ width: '100%' }} size='large'>
        <Card className='!rounded-xl w-full'>
          <div className='flex flex-col gap-4'>
            <div className='flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between'>
              <div className='flex flex-wrap gap-2'>
                <Button
                  theme='solid'
                  type='primary'
                  size='small'
                  onClick={handleGenerate}
                  loading={generateLoading}
                  className='!rounded-lg'
                >
                  {t('生成邀请码')}
                </Button>
                <Button
                  size='small'
                  icon={<Plus size={14} />}
                  onClick={() => setShowBatchModal(true)}
                >
                  {t('批量创建')}
                </Button>
                <Button
                  size='small'
                  type='warning'
                  icon={<Trash2 size={14} />}
                  loading={batchActionLoading === 'clear'}
                  onClick={handleClearUsed}
                >
                  {t('清理已使用')}
                </Button>
                <Button
                  size='small'
                  icon={<Copy size={14} />}
                  loading={batchActionLoading === 'copy_raw_all'}
                  onClick={handleCopyAllUnusedCodes}
                >
                  {t('复制全部未使用原始码')}
                </Button>
                <Button
                  size='small'
                  icon={<Copy size={14} />}
                  disabled={selectedCodes.length === 0}
                  onClick={handleCopySelectedCodes}
                >
                  {t('复制选中原始码')}
                  {selectedCodes.length > 0 ? ` (${selectedCodes.length})` : ''}
                </Button>
                <Button
                  size='small'
                  type='danger'
                  icon={<Trash2 size={14} />}
                  loading={batchActionLoading === 'delete'}
                  disabled={selectedRowKeys.length === 0}
                  onClick={() =>
                    confirmDeleteCodes(
                      selectedRowKeys,
                      t('确定要删除选中的邀请码吗？'),
                    )
                  }
                >
                  {t('批量删除')}
                  {selectedRowKeys.length > 0
                    ? ` (${selectedRowKeys.length})`
                    : ''}
                </Button>
              </div>

              <div className='flex flex-wrap items-center gap-2 text-xs text-[var(--semi-color-text-2)]'>
                <Text type='tertiary'>{`共 ${total} 个邀请码`}</Text>
                <Button
                  size='small'
                  theme='borderless'
                  icon={<RefreshCcw size={14} />}
                  onClick={refreshCurrentPage}
                >
                  {t('刷新')}
                </Button>
              </div>
            </div>

            <Table
              dataSource={myCodes}
              loading={myCodesLoading}
              rowKey='id'
              size='small'
              rowSelection={rowSelection}
              columns={columns}
              scroll={{ x: 1000, y: 520 }}
              pagination={{
                currentPage: activePage,
                pageSize,
                total,
                showSizeChanger: true,
                pageSizeOpts: [10, 20, 50],
                onPageChange: setActivePage,
                onPageSizeChange: (size) => {
                  setPageSize(size);
                  setActivePage(1);
                },
              }}
              empty={
                <Text type='tertiary'>{t('暂无邀请码，点击上方按钮生成')}</Text>
              }
            />
          </div>
        </Card>
      </Space>

      <Modal
        title={t('批量生成邀请码')}
        visible={showBatchModal}
        onOk={handleBatchGenerate}
        onCancel={() => setShowBatchModal(false)}
        okText={t('生成')}
        confirmLoading={batchGenerateLoading}
        centered
      >
        <Form layout='vertical'>
          <Form.Slot label={t('生成数量')}>
            <InputNumber
              value={batchCount}
              onChange={setBatchCount}
              min={1}
              max={100}
              style={{ width: '100%' }}
            />
          </Form.Slot>
          <Form.Slot label={t('每个可用次数')}>
            <InputNumber
              value={batchMaxUses}
              onChange={(v) => setBatchMaxUses(v > 0 ? v : 1)}
              min={1}
              max={1000}
              style={{ width: '100%' }}
            />
            <div
              className='text-xs mt-1'
              style={{ color: 'var(--semi-color-text-2)' }}
            >
              {t(
                '大于 1 即一码多用:同一个码可被多个不同用户各使用一次,按次数计价。',
              )}
            </div>
          </Form.Slot>
          <Form.Slot label={t('备注')}>
            <Input
              value={batchRemark}
              onChange={setBatchRemark}
              placeholder={t('可选备注信息')}
            />
          </Form.Slot>
        </Form>
      </Modal>
    </Card>
  );
};

export default InvitationCard;
