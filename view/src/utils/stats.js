import { API } from '../utils/api.js'

/* 失败必须向上抛：此前 catch 后返回 MockStats，把后端故障伪装成"有 1200 个资源"，
   仪表盘据此显示"数据更新于 XX"，运维会基于假数据做判断。错误态由页面呈现。 */
export async function fetchOverviewStats() {
  const [data, totalData] = await Promise.all([API.stats.overview(), API.stats.resourceTotal()])
  if (!data || !data.resources) {
    throw new Error('概览接口返回数据缺失')
  }
  if (totalData && totalData.total !== undefined) {
    data.resources.total = totalData.total
  }
  return data
}