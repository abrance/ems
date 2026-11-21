-- gd_price_forecast 增加 algo 维度：TimesFM / GBM 对同一目标时刻各留一条。
--
-- 执行人：运维 / DBA（应用启动不再自动改表）。
-- 执行时机：应用停机（或维护窗口）后先执行本文件，再启动带 algo 逻辑的新二进制。
--
-- 为什么不能边跑旧版边执行：
--   旧二进制的 UpsertBatch 用 ON CONFLICT (generated_on, market, grain, target_time)，
--   依赖旧唯一键。旧实例还活着时删掉旧键，旧实例每次写入都会直接报错。
--
-- 回滚注意：
--   一旦同一目标时刻已经同时存在 timesfm 与 gbm 两行，回退到旧唯一键会失败，
--   必须先删除 algo = 'gbm' 的行再回滚；迁移在产生 GBM 行之后是前向不可逆的。
--
-- 执行后用下面的语句核对（应只剩一条唯一约束，且定义里含 algo）：
--   SELECT conname, pg_get_constraintdef(oid)
--     FROM pg_constraint
--    WHERE conrelid = 'gd_price_forecast'::regclass AND contype = 'u';

BEGIN;

-- 1) 补列。历史行视为 timesfm，与旧代码写入的数据一致。
ALTER TABLE gd_price_forecast
  ADD COLUMN IF NOT EXISTS algo VARCHAR(16) NOT NULL DEFAULT 'timesfm';

-- 2) 删旧唯一键（幂等）。
ALTER TABLE gd_price_forecast
  DROP CONSTRAINT IF EXISTS gd_price_forecast_generated_on_market_grain_target_time_key;

-- 3) 加含 algo 的唯一键（幂等：已存在就不重复加）。
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
      FROM pg_constraint
     WHERE conrelid = 'gd_price_forecast'::regclass
       AND contype = 'u'
       AND pg_get_constraintdef(oid) LIKE '%algo%'
  ) THEN
    ALTER TABLE gd_price_forecast
      ADD CONSTRAINT gd_price_forecast_algo_target_key
      UNIQUE (generated_on, market, grain, algo, target_time);
  END IF;
END $$;

COMMIT;
