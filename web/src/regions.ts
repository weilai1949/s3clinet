// 服务商与区域预设 —— 选区域自动填 Endpoint（国内/海外主流对象存储 S3 兼容端点）。

import { locale, t } from './i18n'

export type Provider =
  | 's3'
  | 'rustfs'
  | 'oss'
  | 'cos'
  | 'obs'
  | 'tos'
  | 'bos'
  | 'jd'
  | 'qiniu'
  | 'aws'
  | 'r2'
  | 'wasabi'
  | 'b2'
  | 'do'
  | 'linode'
  | 'scaleway'
  | 'hetzner'

/** 服务商分组：兼容 / 国内 / 国外 */
export type ProviderGroup = 'compatible' | 'domestic' | 'overseas'

export interface ProviderDef {
  value: Provider
  label: string
  desc: string
  group: ProviderGroup
  /** 默认是否 path-style（MinIO/私有云常为 true） */
  pathStyle: boolean
  /** 默认是否 HTTPS */
  useSSL: boolean
  defaultRegion: string
  defaultEndpoint: string
}

export interface RegionPreset {
  label: string
  region: string
  /** 留空表示由 SDK 根据 region 自动推导（AWS） */
  endpoint: string
}

export const PROVIDER_GROUPS: { id: ProviderGroup; label: string }[] = [
  { id: 'compatible', label: 'Compatible' },
  { id: 'domestic', label: 'China' },
  { id: 'overseas', label: 'Overseas' },
]

export const PROVIDERS: ProviderDef[] = [
  // —— 兼容 ——
  {
    value: 's3',
    label: 'S3 Compatible',
    desc: 'MinIO / private cloud / custom endpoint',
    group: 'compatible',
    pathStyle: true,
    useSSL: false,
    defaultRegion: 'us-east-1',
    defaultEndpoint: '127.0.0.1:9000',
  },
  {
    value: 'rustfs',
    label: 'RustFS',
    desc: 'Local S3 at 127.0.0.1:9000 (rustfsadmin)',
    group: 'compatible',
    pathStyle: true,
    useSSL: false,
    defaultRegion: 'us-east-1',
    defaultEndpoint: '127.0.0.1:9000',
  },
  // —— 国内 ——
  {
    value: 'oss',
    label: 'Alibaba Cloud OSS',
    desc: 'Public / finance / cloud box, etc.',
    group: 'domestic',
    pathStyle: false,
    useSSL: true,
    defaultRegion: 'oss-cn-hangzhou',
    defaultEndpoint: 'oss-cn-hangzhou.aliyuncs.com',
  },
  {
    value: 'cos',
    label: 'Tencent Cloud COS',
    desc: 'S3-compatible: cos.<region>.myqcloud.com',
    group: 'domestic',
    pathStyle: false,
    useSSL: true,
    defaultRegion: 'ap-guangzhou',
    defaultEndpoint: 'cos.ap-guangzhou.myqcloud.com',
  },
  {
    value: 'obs',
    label: 'Huawei Cloud OBS',
    desc: 'S3-compatible: obs.<region>.myhuaweicloud.com',
    group: 'domestic',
    pathStyle: false,
    useSSL: true,
    defaultRegion: 'cn-north-4',
    defaultEndpoint: 'obs.cn-north-4.myhuaweicloud.com',
  },
  {
    value: 'tos',
    label: 'Volcengine TOS',
    desc: 'S3-compatible: tos-s3-<region>.volces.com',
    group: 'domestic',
    pathStyle: false,
    useSSL: true,
    defaultRegion: 'cn-beijing',
    defaultEndpoint: 'tos-s3-cn-beijing.volces.com',
  },
  {
    value: 'bos',
    label: 'Baidu BOS',
    desc: 'S3-compatible: s3.<region>.bcebos.com',
    group: 'domestic',
    pathStyle: false,
    useSSL: true,
    defaultRegion: 'bj',
    defaultEndpoint: 's3.bj.bcebos.com',
  },
  {
    value: 'jd',
    label: 'JD Cloud OSS',
    desc: 'S3-compatible: s3.<region>.jdcloud-oss.com',
    group: 'domestic',
    pathStyle: false,
    useSSL: true,
    defaultRegion: 'cn-north-1',
    defaultEndpoint: 's3.cn-north-1.jdcloud-oss.com',
  },
  {
    value: 'qiniu',
    label: 'Qiniu Kodo',
    desc: 'S3-compatible: s3.<region>.qiniucs.com',
    group: 'domestic',
    pathStyle: false,
    useSSL: true,
    defaultRegion: 'cn-east-1',
    defaultEndpoint: 's3.cn-east-1.qiniucs.com',
  },
  // —— 国外 ——
  {
    value: 'aws',
    label: 'AWS S3',
    desc: 'AWS regions (endpoint derived by SDK)',
    group: 'overseas',
    pathStyle: false,
    useSSL: true,
    defaultRegion: 'us-east-1',
    defaultEndpoint: '',
  },
  {
    value: 'r2',
    label: 'Cloudflare R2',
    desc: 'Replace ACCOUNT_ID with your Cloudflare account ID',
    group: 'overseas',
    pathStyle: false,
    useSSL: true,
    defaultRegion: 'auto',
    defaultEndpoint: 'ACCOUNT_ID.r2.cloudflarestorage.com',
  },
  {
    value: 'wasabi',
    label: 'Wasabi',
    desc: 'S3-compatible: s3.<region>.wasabisys.com',
    group: 'overseas',
    pathStyle: false,
    useSSL: true,
    defaultRegion: 'us-east-1',
    defaultEndpoint: 's3.us-east-1.wasabisys.com',
  },
  {
    value: 'b2',
    label: 'Backblaze B2',
    desc: 'S3-compatible: s3.<region>.backblazeb2.com',
    group: 'overseas',
    pathStyle: false,
    useSSL: true,
    defaultRegion: 'us-west-004',
    defaultEndpoint: 's3.us-west-004.backblazeb2.com',
  },
  {
    value: 'do',
    label: 'DigitalOcean Spaces',
    desc: 'S3-compatible: <region>.digitaloceanspaces.com',
    group: 'overseas',
    pathStyle: false,
    useSSL: true,
    defaultRegion: 'nyc3',
    defaultEndpoint: 'nyc3.digitaloceanspaces.com',
  },
  {
    value: 'linode',
    label: 'Linode / Akamai',
    desc: 'S3-compatible: <region>.linodeobjects.com',
    group: 'overseas',
    pathStyle: false,
    useSSL: true,
    defaultRegion: 'us-east-1',
    defaultEndpoint: 'us-east-1.linodeobjects.com',
  },
  {
    value: 'scaleway',
    label: 'Scaleway',
    desc: 'S3-compatible: s3.<region>.scw.cloud',
    group: 'overseas',
    pathStyle: false,
    useSSL: true,
    defaultRegion: 'fr-par',
    defaultEndpoint: 's3.fr-par.scw.cloud',
  },
  {
    value: 'hetzner',
    label: 'Hetzner',
    desc: 'S3-compatible: <region>.your-objectstorage.com',
    group: 'overseas',
    pathStyle: false,
    useSSL: true,
    defaultRegion: 'fsn1',
    defaultEndpoint: 'fsn1.your-objectstorage.com',
  },
]

export function providersInGroup(group: ProviderGroup): ProviderDef[] {
  return PROVIDERS.filter((p) => p.group === group)
}

export const OSS_REGIONS: RegionPreset[] = [
  { label: '华东1（杭州）', region: 'oss-cn-hangzhou', endpoint: 'oss-cn-hangzhou.aliyuncs.com' },
  { label: '华东2（上海）', region: 'oss-cn-shanghai', endpoint: 'oss-cn-shanghai.aliyuncs.com' },
  { label: '华北1（青岛）', region: 'oss-cn-qingdao', endpoint: 'oss-cn-qingdao.aliyuncs.com' },
  { label: '华北2（北京）', region: 'oss-cn-beijing', endpoint: 'oss-cn-beijing.aliyuncs.com' },
  { label: '华北3（张家口）', region: 'oss-cn-zhangjiakou', endpoint: 'oss-cn-zhangjiakou.aliyuncs.com' },
  { label: '华北5（呼和浩特）', region: 'oss-cn-huhehaote', endpoint: 'oss-cn-huhehaote.aliyuncs.com' },
  { label: '华北6（乌兰察布）', region: 'oss-cn-wulanchabu', endpoint: 'oss-cn-wulanchabu.aliyuncs.com' },
  { label: '华南1（深圳）', region: 'oss-cn-shenzhen', endpoint: 'oss-cn-shenzhen.aliyuncs.com' },
  { label: '华南2（河源）', region: 'oss-cn-heyuan', endpoint: 'oss-cn-heyuan.aliyuncs.com' },
  { label: '华南3（广州）', region: 'oss-cn-guangzhou', endpoint: 'oss-cn-guangzhou.aliyuncs.com' },
  { label: '西南1（成都）', region: 'oss-cn-chengdu', endpoint: 'oss-cn-chengdu.aliyuncs.com' },
  { label: '中国（香港）', region: 'oss-cn-hongkong', endpoint: 'oss-cn-hongkong.aliyuncs.com' },
  { label: '美国西部1（硅谷）', region: 'oss-us-west-1', endpoint: 'oss-us-west-1.aliyuncs.com' },
  { label: '美国东部1（弗吉尼亚）', region: 'oss-us-east-1', endpoint: 'oss-us-east-1.aliyuncs.com' },
  { label: '亚太东南1（新加坡）', region: 'oss-ap-southeast-1', endpoint: 'oss-ap-southeast-1.aliyuncs.com' },
  { label: '亚太东南2（悉尼）', region: 'oss-ap-southeast-2', endpoint: 'oss-ap-southeast-2.aliyuncs.com' },
  { label: '亚太东南3（吉隆坡）', region: 'oss-ap-southeast-3', endpoint: 'oss-ap-southeast-3.aliyuncs.com' },
  { label: '亚太东北1（东京）', region: 'oss-ap-northeast-1', endpoint: 'oss-ap-northeast-1.aliyuncs.com' },
  { label: '欧洲中部1（法兰克福）', region: 'oss-eu-central-1', endpoint: 'oss-eu-central-1.aliyuncs.com' },
  { label: '欧洲西部1（伦敦）', region: 'oss-eu-west-1', endpoint: 'oss-eu-west-1.aliyuncs.com' },
  { label: '中东东部1（迪拜）', region: 'oss-me-east-1', endpoint: 'oss-me-east-1.aliyuncs.com' },
]

/** 腾讯云 COS：Endpoint = cos.<region>.myqcloud.com */
export const COS_REGIONS: RegionPreset[] = [
  { label: '北京', region: 'ap-beijing', endpoint: 'cos.ap-beijing.myqcloud.com' },
  { label: '南京', region: 'ap-nanjing', endpoint: 'cos.ap-nanjing.myqcloud.com' },
  { label: '上海', region: 'ap-shanghai', endpoint: 'cos.ap-shanghai.myqcloud.com' },
  { label: '广州', region: 'ap-guangzhou', endpoint: 'cos.ap-guangzhou.myqcloud.com' },
  { label: '成都', region: 'ap-chengdu', endpoint: 'cos.ap-chengdu.myqcloud.com' },
  { label: '重庆', region: 'ap-chongqing', endpoint: 'cos.ap-chongqing.myqcloud.com' },
  { label: '深圳金融', region: 'ap-shenzhen-fsi', endpoint: 'cos.ap-shenzhen-fsi.myqcloud.com' },
  { label: '上海金融', region: 'ap-shanghai-fsi', endpoint: 'cos.ap-shanghai-fsi.myqcloud.com' },
  { label: '北京金融', region: 'ap-beijing-fsi', endpoint: 'cos.ap-beijing-fsi.myqcloud.com' },
  { label: '中国香港', region: 'ap-hongkong', endpoint: 'cos.ap-hongkong.myqcloud.com' },
  { label: '新加坡', region: 'ap-singapore', endpoint: 'cos.ap-singapore.myqcloud.com' },
  { label: '雅加达', region: 'ap-jakarta', endpoint: 'cos.ap-jakarta.myqcloud.com' },
  { label: '首尔', region: 'ap-seoul', endpoint: 'cos.ap-seoul.myqcloud.com' },
  { label: '曼谷', region: 'ap-bangkok', endpoint: 'cos.ap-bangkok.myqcloud.com' },
  { label: '东京', region: 'ap-tokyo', endpoint: 'cos.ap-tokyo.myqcloud.com' },
  { label: '硅谷（美西）', region: 'na-siliconvalley', endpoint: 'cos.na-siliconvalley.myqcloud.com' },
  { label: '弗吉尼亚（美东）', region: 'na-ashburn', endpoint: 'cos.na-ashburn.myqcloud.com' },
  { label: '圣保罗', region: 'sa-saopaulo', endpoint: 'cos.sa-saopaulo.myqcloud.com' },
  { label: '法兰克福', region: 'eu-frankfurt', endpoint: 'cos.eu-frankfurt.myqcloud.com' },
]

/** 华为云 OBS：Endpoint = obs.<region>.myhuaweicloud.com */
export const OBS_REGIONS: RegionPreset[] = [
  { label: '华北-北京一', region: 'cn-north-1', endpoint: 'obs.cn-north-1.myhuaweicloud.com' },
  { label: '华北-北京四', region: 'cn-north-4', endpoint: 'obs.cn-north-4.myhuaweicloud.com' },
  { label: '华北-乌兰察布一', region: 'cn-north-9', endpoint: 'obs.cn-north-9.myhuaweicloud.com' },
  { label: '华东-上海一', region: 'cn-east-3', endpoint: 'obs.cn-east-3.myhuaweicloud.com' },
  { label: '华东-上海二', region: 'cn-east-2', endpoint: 'obs.cn-east-2.myhuaweicloud.com' },
  { label: '华南-广州', region: 'cn-south-1', endpoint: 'obs.cn-south-1.myhuaweicloud.com' },
  { label: '华南-深圳', region: 'cn-south-2', endpoint: 'obs.cn-south-2.myhuaweicloud.com' },
  { label: '西南-贵阳一', region: 'cn-southwest-2', endpoint: 'obs.cn-southwest-2.myhuaweicloud.com' },
  { label: '中国-香港', region: 'ap-southeast-1', endpoint: 'obs.ap-southeast-1.myhuaweicloud.com' },
  { label: '亚太-曼谷', region: 'ap-southeast-2', endpoint: 'obs.ap-southeast-2.myhuaweicloud.com' },
  { label: '亚太-新加坡', region: 'ap-southeast-3', endpoint: 'obs.ap-southeast-3.myhuaweicloud.com' },
  { label: '亚太-雅加达', region: 'ap-southeast-4', endpoint: 'obs.ap-southeast-4.myhuaweicloud.com' },
  { label: '非洲-约翰内斯堡', region: 'af-south-1', endpoint: 'obs.af-south-1.myhuaweicloud.com' },
]

/** 火山引擎 TOS：须用 S3 协议域名 tos-s3-<region>.volces.com */
export const TOS_REGIONS: RegionPreset[] = [
  { label: '华北2（北京）', region: 'cn-beijing', endpoint: 'tos-s3-cn-beijing.volces.com' },
  { label: '华东2（上海）', region: 'cn-shanghai', endpoint: 'tos-s3-cn-shanghai.volces.com' },
  { label: '华南1（广州）', region: 'cn-guangzhou', endpoint: 'tos-s3-cn-guangzhou.volces.com' },
  { label: '中国香港', region: 'cn-hongkong', endpoint: 'tos-s3-cn-hongkong.volces.com' },
  { label: '亚太东南（柔佛）', region: 'ap-southeast-1', endpoint: 'tos-s3-ap-southeast-1.volces.com' },
  { label: '亚太东南（雅加达）', region: 'ap-southeast-3', endpoint: 'tos-s3-ap-southeast-3.volces.com' },
]

/** 百度智能云 BOS：S3 兼容域名 s3.<region>.bcebos.com */
export const BOS_REGIONS: RegionPreset[] = [
  { label: '北京', region: 'bj', endpoint: 's3.bj.bcebos.com' },
  { label: '广州', region: 'gz', endpoint: 's3.gz.bcebos.com' },
  { label: '苏州', region: 'su', endpoint: 's3.su.bcebos.com' },
  { label: '保定', region: 'bd', endpoint: 's3.bd.bcebos.com' },
  { label: '金融云武汉', region: 'fwh', endpoint: 's3.fwh.bcebos.com' },
  { label: '中国香港', region: 'hkg', endpoint: 's3.hkg.bcebos.com' },
]

/** 京东云 OSS：Endpoint = s3.<region>.jdcloud-oss.com */
export const JD_REGIONS: RegionPreset[] = [
  { label: '华北-北京', region: 'cn-north-1', endpoint: 's3.cn-north-1.jdcloud-oss.com' },
  { label: '华东-宿迁', region: 'cn-east-1', endpoint: 's3.cn-east-1.jdcloud-oss.com' },
  { label: '华东-上海', region: 'cn-east-2', endpoint: 's3.cn-east-2.jdcloud-oss.com' },
  { label: '华南-广州', region: 'cn-south-1', endpoint: 's3.cn-south-1.jdcloud-oss.com' },
]

/** 七牛云 Kodo：Endpoint = s3.<region>.qiniucs.com */
export const QINIU_REGIONS: RegionPreset[] = [
  { label: '华东-浙江', region: 'cn-east-1', endpoint: 's3.cn-east-1.qiniucs.com' },
  { label: '华东-浙江2', region: 'cn-east-2', endpoint: 's3.cn-east-2.qiniucs.com' },
  { label: '华北-河北', region: 'cn-north-1', endpoint: 's3.cn-north-1.qiniucs.com' },
  { label: '华南-广东', region: 'cn-south-1', endpoint: 's3.cn-south-1.qiniucs.com' },
  { label: '北美-洛杉矶', region: 'us-north-1', endpoint: 's3.us-north-1.qiniucs.com' },
  { label: '亚太-新加坡', region: 'ap-southeast-1', endpoint: 's3.ap-southeast-1.qiniucs.com' },
  { label: '亚太-河内', region: 'ap-southeast-2', endpoint: 's3.ap-southeast-2.qiniucs.com' },
  { label: '亚太-胡志明', region: 'ap-southeast-3', endpoint: 's3.ap-southeast-3.qiniucs.com' },
]

export const AWS_REGIONS: RegionPreset[] = [
  { label: '美国东部（弗吉尼亚北部）', region: 'us-east-1', endpoint: '' },
  { label: '美国东部（俄亥俄）', region: 'us-east-2', endpoint: '' },
  { label: '美国西部（加利福尼亚北部）', region: 'us-west-1', endpoint: '' },
  { label: '美国西部（俄勒冈）', region: 'us-west-2', endpoint: '' },
  { label: '加拿大（中部）', region: 'ca-central-1', endpoint: '' },
  { label: '南美洲（圣保罗）', region: 'sa-east-1', endpoint: '' },
  { label: '欧洲（法兰克福）', region: 'eu-central-1', endpoint: '' },
  { label: '欧洲（爱尔兰）', region: 'eu-west-1', endpoint: '' },
  { label: '欧洲（伦敦）', region: 'eu-west-2', endpoint: '' },
  { label: '欧洲（巴黎）', region: 'eu-west-3', endpoint: '' },
  { label: '欧洲（斯德哥尔摩）', region: 'eu-north-1', endpoint: '' },
  { label: '亚太（香港）', region: 'ap-east-1', endpoint: '' },
  { label: '亚太（孟买）', region: 'ap-south-1', endpoint: '' },
  { label: '亚太（首尔）', region: 'ap-northeast-2', endpoint: '' },
  { label: '亚太（大阪）', region: 'ap-northeast-3', endpoint: '' },
  { label: '亚太（新加坡）', region: 'ap-southeast-1', endpoint: '' },
  { label: '亚太（悉尼）', region: 'ap-southeast-2', endpoint: '' },
  { label: '亚太（雅加达）', region: 'ap-southeast-3', endpoint: '' },
  { label: '亚太（东京）', region: 'ap-northeast-1', endpoint: '' },
  { label: '中东（巴林）', region: 'me-south-1', endpoint: '' },
  { label: '非洲（开普敦）', region: 'af-south-1', endpoint: '' },
]

/** Cloudflare R2：Endpoint = <ACCOUNT_ID>.r2.cloudflarestorage.com（请替换 ACCOUNT_ID；欧盟可用 ACCOUNT_ID.eu.r2.cloudflarestorage.com） */
export const R2_REGIONS: RegionPreset[] = [
  { label: '自动（推荐）', region: 'auto', endpoint: 'ACCOUNT_ID.r2.cloudflarestorage.com' },
]

/** Wasabi：Endpoint = s3.<region>.wasabisys.com */
export const WASABI_REGIONS: RegionPreset[] = [
  { label: 'US East 1（弗吉尼亚）', region: 'us-east-1', endpoint: 's3.us-east-1.wasabisys.com' },
  { label: 'US East 2（弗吉尼亚）', region: 'us-east-2', endpoint: 's3.us-east-2.wasabisys.com' },
  { label: 'US Central 1（德州）', region: 'us-central-1', endpoint: 's3.us-central-1.wasabisys.com' },
  { label: 'US West 1（俄勒冈）', region: 'us-west-1', endpoint: 's3.us-west-1.wasabisys.com' },
  { label: 'US West 2（圣何塞）', region: 'us-west-2', endpoint: 's3.us-west-2.wasabisys.com' },
  { label: 'CA Central 1（多伦多）', region: 'ca-central-1', endpoint: 's3.ca-central-1.wasabisys.com' },
  { label: 'EU Central 1（阿姆斯特丹）', region: 'eu-central-1', endpoint: 's3.eu-central-1.wasabisys.com' },
  { label: 'EU Central 2（法兰克福）', region: 'eu-central-2', endpoint: 's3.eu-central-2.wasabisys.com' },
  { label: 'EU West 1（英国）', region: 'eu-west-1', endpoint: 's3.eu-west-1.wasabisys.com' },
  { label: 'EU West 2（巴黎）', region: 'eu-west-2', endpoint: 's3.eu-west-2.wasabisys.com' },
  { label: 'EU West 3（英国）', region: 'eu-west-3', endpoint: 's3.eu-west-3.wasabisys.com' },
  { label: 'EU South 1（米兰）', region: 'eu-south-1', endpoint: 's3.eu-south-1.wasabisys.com' },
  { label: 'AP Northeast 1（东京）', region: 'ap-northeast-1', endpoint: 's3.ap-northeast-1.wasabisys.com' },
  { label: 'AP Northeast 2（大阪）', region: 'ap-northeast-2', endpoint: 's3.ap-northeast-2.wasabisys.com' },
  { label: 'AP Southeast 1（新加坡）', region: 'ap-southeast-1', endpoint: 's3.ap-southeast-1.wasabisys.com' },
  { label: 'AP Southeast 2（悉尼）', region: 'ap-southeast-2', endpoint: 's3.ap-southeast-2.wasabisys.com' },
]

/** Backblaze B2：Endpoint = s3.<region>.backblazeb2.com */
export const B2_REGIONS: RegionPreset[] = [
  { label: 'US West', region: 'us-west-004', endpoint: 's3.us-west-004.backblazeb2.com' },
  { label: 'US East', region: 'us-east-005', endpoint: 's3.us-east-005.backblazeb2.com' },
  { label: 'EU Central', region: 'eu-central-003', endpoint: 's3.eu-central-003.backblazeb2.com' },
]

/** DigitalOcean Spaces：Endpoint = <region>.digitaloceanspaces.com */
export const DO_REGIONS: RegionPreset[] = [
  { label: '纽约 nyc3', region: 'nyc3', endpoint: 'nyc3.digitaloceanspaces.com' },
  { label: '旧金山 sfo2', region: 'sfo2', endpoint: 'sfo2.digitaloceanspaces.com' },
  { label: '旧金山 sfo3', region: 'sfo3', endpoint: 'sfo3.digitaloceanspaces.com' },
  { label: '阿姆斯特丹 ams3', region: 'ams3', endpoint: 'ams3.digitaloceanspaces.com' },
  { label: '新加坡 sgp1', region: 'sgp1', endpoint: 'sgp1.digitaloceanspaces.com' },
  { label: '法兰克福 fra1', region: 'fra1', endpoint: 'fra1.digitaloceanspaces.com' },
  { label: '悉尼 syd1', region: 'syd1', endpoint: 'syd1.digitaloceanspaces.com' },
]

/** Linode / Akamai Object Storage：Endpoint = <region>.linodeobjects.com */
export const LINODE_REGIONS: RegionPreset[] = [
  { label: '美国东部（纽瓦克）', region: 'us-east-1', endpoint: 'us-east-1.linodeobjects.com' },
  { label: '美国东南（亚特兰大）', region: 'us-southeast-1', endpoint: 'us-southeast-1.linodeobjects.com' },
  { label: '美国中部（芝加哥）', region: 'us-ord-1', endpoint: 'us-ord-1.linodeobjects.com' },
  { label: '美国西部（洛杉矶）', region: 'us-lax-1', endpoint: 'us-lax-1.linodeobjects.com' },
  { label: '美国西部（西雅图）', region: 'us-sea-1', endpoint: 'us-sea-1.linodeobjects.com' },
  { label: '欧洲中部（法兰克福）', region: 'eu-central-1', endpoint: 'eu-central-1.linodeobjects.com' },
  { label: '欧洲西部（伦敦）', region: 'eu-west-1', endpoint: 'eu-west-1.linodeobjects.com' },
  { label: '亚太南部（新加坡）', region: 'ap-south-1', endpoint: 'ap-south-1.linodeobjects.com' },
  { label: '亚太东北（东京）', region: 'ap-northeast-1', endpoint: 'ap-northeast-1.linodeobjects.com' },
]

/** Scaleway：Endpoint = s3.<region>.scw.cloud */
export const SCALEWAY_REGIONS: RegionPreset[] = [
  { label: '巴黎 fr-par', region: 'fr-par', endpoint: 's3.fr-par.scw.cloud' },
  { label: '阿姆斯特丹 nl-ams', region: 'nl-ams', endpoint: 's3.nl-ams.scw.cloud' },
  { label: '华沙 pl-waw', region: 'pl-waw', endpoint: 's3.pl-waw.scw.cloud' },
]

/** Hetzner Object Storage：Endpoint = <region>.your-objectstorage.com */
export const HETZNER_REGIONS: RegionPreset[] = [
  { label: 'Falkenstein fsn1', region: 'fsn1', endpoint: 'fsn1.your-objectstorage.com' },
  { label: 'Nuremberg nbg1', region: 'nbg1', endpoint: 'nbg1.your-objectstorage.com' },
  { label: 'Helsinki hel1', region: 'hel1', endpoint: 'hel1.your-objectstorage.com' },
]

const REGION_MAP: Record<Provider, RegionPreset[]> = {
  s3: [],
  rustfs: [],
  oss: OSS_REGIONS,
  cos: COS_REGIONS,
  obs: OBS_REGIONS,
  tos: TOS_REGIONS,
  bos: BOS_REGIONS,
  jd: JD_REGIONS,
  qiniu: QINIU_REGIONS,
  aws: AWS_REGIONS,
  r2: R2_REGIONS,
  wasabi: WASABI_REGIONS,
  b2: B2_REGIONS,
  do: DO_REGIONS,
  linode: LINODE_REGIONS,
  scaleway: SCALEWAY_REGIONS,
  hetzner: HETZNER_REGIONS,
}

/** 依据 endpoint 反推服务商，用于编辑时预选。 */
export function inferProvider(endpoint: string): Provider {
  const e = (endpoint || '').toLowerCase()
  if (e.includes('aliyuncs.com')) return 'oss'
  if (e.includes('myqcloud.com') || e.includes('tencentcos.cn')) return 'cos'
  if (e.includes('myhuaweicloud.com')) return 'obs'
  if (e.includes('volces.com') || e.includes('ivolces.com')) return 'tos'
  if (e.includes('bcebos.com')) return 'bos'
  if (e.includes('jdcloud-oss.com')) return 'jd'
  if (e.includes('qiniucs.com')) return 'qiniu'
  if (e.includes('r2.cloudflarestorage.com')) return 'r2'
  if (e.includes('wasabisys.com')) return 'wasabi'
  if (e.includes('backblazeb2.com')) return 'b2'
  if (e.includes('digitaloceanspaces.com')) return 'do'
  if (e.includes('linodeobjects.com')) return 'linode'
  if (e.includes('scw.cloud')) return 'scaleway'
  if (e.includes('your-objectstorage.com')) return 'hetzner'
  if (e.includes('amazonaws.com')) return 'aws'
  return 's3'
}

/** 依据服务商返回区域候选列表。 */
export function regionsFor(p: Provider): RegionPreset[] {
  return REGION_MAP[p] ?? []
}

/** 公共云预设是否应同步公网 Endpoint（有固定域名的服务商）。 */
export function syncsPublicEndpoint(p: Provider): boolean {
  return p !== 'aws' && p !== 's3'
}

/** 服务商分组标签（value/id 保持英文）。 */
export function providerGroupLabel(id: ProviderGroup): string {
  return t(`provider.group.${id}`)
}

/** 服务商标签（value 保持英文）。 */
export function providerLabel(p: Provider): string {
  return t(`provider.${p}.label`)
}

/** 服务商描述。 */
export function providerDesc(p: Provider): string {
  return t(`provider.${p}.desc`)
}

/**
 * 区域展示名：zh-CN 用预设中文标签，其它语言显示 region code。
 * 避免为 ~140 个区域各建 i18n key。
 */
export function regionLabel(code: string, zhLabel: string): string {
  return locale() === 'zh-CN' ? zhLabel : code
}
