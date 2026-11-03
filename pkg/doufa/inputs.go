package doufa

import "encoding/base64"

// 操作记录的传输格式：相邻相同的输入合成一段，每段写成两个无符号变长整数(输入值、帧数)，整串再做 base64url(不补等号)。
// 一间房几千帧，按住方向键与攻击时输入很少变，压完通常只有几百字节。浏览器的 encodeInputs 写出同样的字节。

// EncodeInputs 把逐帧输入压成传输用的字符串。
func EncodeInputs(inputs []int) string {
	var out []byte
	put := func(v int) {
		for v >= 0x80 {
			out = append(out, byte(v&0x7f|0x80))
			v >>= 7
		}
		out = append(out, byte(v))
	}
	for i := 0; i < len(inputs); {
		j := i + 1
		for j < len(inputs) && inputs[j] == inputs[i] {
			j++
		}
		put(inputs[i])
		put(j - i)
		i = j
	}
	return base64.RawURLEncoding.EncodeToString(out)
}

// DecodeInputs 还原逐帧输入。格式不对、某段帧数为 0、输入越界或总帧数超过 MaxTicks 都返回 ErrReplay。
func DecodeInputs(s string) ([]int, error) {
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrReplay
	}
	pos := 0
	get := func() (int, bool) {
		v, shift := 0, 0
		for pos < len(data) && shift <= 21 {
			b := data[pos]
			pos++
			v |= int(b&0x7f) << shift
			if b < 0x80 {
				return v, true
			}
			shift += 7
		}
		return 0, false
	}
	var inputs []int
	for pos < len(data) {
		value, ok := get()
		if !ok || value >= 1<<18 {
			return nil, ErrReplay
		}
		run, ok := get()
		if !ok || run < 1 || len(inputs)+run > MaxTicks {
			return nil, ErrReplay
		}
		for k := 0; k < run; k++ {
			inputs = append(inputs, value)
		}
	}
	return inputs, nil
}
