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

import React, { useMemo } from 'react';
import { Empty, Layout } from '@douyinfe/semi-ui';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import CardPro from '../../common/ui/CardPro';
import CardTable from '../../common/ui/CardTable';
import AuditLogsFilters from './AuditLogsFilters';
import AuditLogDetailModal from './modals/AuditLogDetailModal';
import { getAuditLogsColumns } from './AuditLogsColumnDefs';
import { useAuditLogsData } from '../../../hooks/audit-logs/useAuditLogsData';
import { useIsMobile } from '../../../hooks/common/useIsMobile';
import { createCardProPagination } from '../../../helpers/utils';

const AuditLogsPage = () => {
  const data = useAuditLogsData();
  const isMobile = useIsMobile();
  const { t, copyText, setDetail, isAdminUser } = data;
  const columns = useMemo(
    () => getAuditLogsColumns({ t, copyText, setDetail, isAdminUser }),
    [t, copyText, setDetail, isAdminUser],
  );

  return (
    <>
      <AuditLogDetailModal detail={data.detail} setDetail={setDetail} t={t} />
      <Layout>
        <CardPro
          type='type2'
          searchArea={<AuditLogsFilters {...data} />}
          paginationArea={createCardProPagination({
            currentPage: data.activePage,
            pageSize: data.pageSize,
            total: data.logCount,
            onPageChange: data.handlePageChange,
            onPageSizeChange: data.handlePageSizeChange,
            isMobile,
            t,
          })}
          t={t}
        >
          <CardTable
            columns={columns}
            dataSource={data.logs}
            rowKey='key'
            loading={data.loading}
            scroll={{ x: 'max-content' }}
            className='rounded-xl overflow-hidden'
            size='middle'
            empty={
              <Empty
                image={
                  <IllustrationNoResult style={{ width: 150, height: 150 }} />
                }
                darkModeImage={
                  <IllustrationNoResultDark
                    style={{ width: 150, height: 150 }}
                  />
                }
                description={t('搜索无结果')}
                style={{ padding: 30 }}
              />
            }
            pagination={{
              currentPage: data.activePage,
              pageSize: data.pageSize,
              total: data.logCount,
              pageSizeOptions: [10, 20, 50, 100],
              showSizeChanger: true,
              onPageSizeChange: data.handlePageSizeChange,
              onPageChange: data.handlePageChange,
            }}
            hidePagination={true}
          />
        </CardPro>
      </Layout>
    </>
  );
};

export default AuditLogsPage;
