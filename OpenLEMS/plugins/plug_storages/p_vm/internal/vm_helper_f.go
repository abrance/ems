package internal

import "common/c_enum"

// priceTypeCode 电价类型转数值，和 p_tsdb 的 getPriceTypeValue 保持一致。
func priceTypeCode(priceType string) int {
	return int(c_enum.ParsePriceType(priceType))
}
