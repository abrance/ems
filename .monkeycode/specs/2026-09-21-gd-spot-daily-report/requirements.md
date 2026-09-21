# Requirements Document

# Introduction

本功能把广东电力交易中心公众号「现货运行日报」接入 OpenLEMS 能源管理系统。运维人员在首页「电力现货」区块阅读日报原图；管理员在管理页「信息管理」中粘贴微信文章链接完成采集。日报按运行日归档，图片保存到对象存储。

# Glossary

- **系统**: OpenLEMS 能源管理系统，含首页、管理页与采集服务。
- **运行日**: 从日报标题解析出的公历日期，格式 YYYY-MM-DD，例如标题 `(09.21)` 对应 `2026-09-21`。
- **电力现货区块**: 首页中位于「能源系统」「实时状态」「电网功率趋势」「用电发电趋势」上方的展示区域。
- **信息管理**: 管理页 Management 分区中的功能卡片，进入后管理日报档案。
- **日报原图**: 从微信文章正文中抽出的 4 页固定版式图片，不含页眉页脚。
- **昨天**: 系统当前日期（Asia/Shanghai）的前一个公历日。
- **最近一篇**: 档案中运行日最大的已采集日报。
- **覆盖采集**: 对同一运行日再次采集，用新图片与标题替换该日档案。

# Requirements

### Requirement 1

**User Story:** AS 已登录用户, I want 在首页能源看板上方看到电力现货日报, so that 查看现货行情时不必离开 EMS。

#### Acceptance Criteria

1. WHILE 用户停留在 EMS 首页, THE 系统 SHALL 在「能源系统」「实时状态」「电网功率趋势」「用电发电趋势」四个区块上方展示标题为「电力现货」的区块。
2. WHEN 电力现货区块完成加载, THE 系统 SHALL 在该区块内提供运行日切换控件，并展示该运行日的 4 页日报原图，支持左右翻页。
3. WHEN 用户切换运行日, THE 系统 SHALL 将电力现货区块中的 4 页原图替换为所选运行日档案中的图片。

### Requirement 2

**User Story:** AS 已登录用户, I want 打开首页时默认看到昨天的日报, so that 每天看到的是刚过去那一天的市场信息。

#### Acceptance Criteria

1. WHEN 用户进入 EMS 首页且档案中存在运行日等于昨天的日报, THE 系统 SHALL 将电力现货区块的当前运行日设为昨天。
2. WHEN 用户进入 EMS 首页且档案中不存在运行日等于昨天的日报、同时档案中至少有一篇日报, THE 系统 SHALL 将电力现货区块的当前运行日设为最近一篇的运行日。
3. WHEN 用户进入 EMS 首页且档案中零篇日报, THE 系统 SHALL 在电力现货区块展示「暂无日报」提示。

### Requirement 3

**User Story:** AS 管理员, I want 在管理页通过信息管理维护日报档案, so that 每日用微信链接完成采集。

#### Acceptance Criteria

1. WHILE 管理员打开管理页 Management 分区, THE 系统 SHALL 展示「信息管理」卡片。
2. WHEN 管理员打开信息管理, THE 系统 SHALL 按运行日降序列出已采集日报，每条至少包含运行日、标题、页数。
3. WHEN 管理员在信息管理中选择新增并提交微信公众号文章链接, THE 系统 SHALL 采集该文章并写入对应运行日档案。
4. WHEN 管理员提交的链接对应的运行日在档案中已存在, THE 系统 SHALL 用本次采集结果覆盖该运行日档案。
5. WHEN 管理员在信息管理列表中打开某一条日报, THE 系统 SHALL 展示该运行日的标题、运行日与 4 页日报原图。

### Requirement 4

**User Story:** AS 采集服务, I want 从固定版式的微信日报中抽出标题、运行日和正文图, so that 档案内容可按日展示。

#### Acceptance Criteria

1. WHEN 采集服务收到 `mp.weixin.qq.com` 文章链接, THE 系统 SHALL 读取文章标题。
2. WHEN 标题匹配「YYYY年M月运行日报(MM.DD)」模式, THE 系统 SHALL 将运行日解析为对应的年、月、日。
3. WHEN 文章正文含页眉、4 页日报、页脚, THE 系统 SHALL 仅将 4 页日报原图写入档案，并按顺序标记为：市场概况与日前申报、日前成交与高峰节点电价、低谷节点电价与价格曲线、实时交易电价。
4. WHEN 4 页日报原图采集成功, THE 系统 SHALL 将图片保存到已配置的七牛云空间，对象键为 `gd-spot/{运行日}/page-{页码}.png`，并在档案中保存可访问 URL。

### Requirement 5

**User Story:** AS 管理员, I want 采集失败时看到明确原因, so that 能改链接后重试。

#### Acceptance Criteria

1. WHEN 提交的链接主机名不是 `mp.weixin.qq.com`, THE 系统 SHALL 在信息管理界面展示「只支持微信公众号文章链接」。
2. WHEN 标题无法解析出运行日, THE 系统 SHALL 在信息管理界面展示「标题未包含运行日报日期」及原始标题。
3. WHEN 文章中识别不到 4 页日报原图, THE 系统 SHALL 在信息管理界面展示「文章中没有识别到日报图片」。
4. WHEN 采集成功, THE 系统 SHALL 刷新信息管理列表，并将新档案排在按运行日降序后的对应位置。
