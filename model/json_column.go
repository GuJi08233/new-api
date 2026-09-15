package model

// jsonScanBytes 兼容各驱动返回JSON文本或字节；PG simple protocol写入使用string。
func jsonScanBytes(value any) []byte {
	switch v := value.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	default:
		return nil
	}
}
