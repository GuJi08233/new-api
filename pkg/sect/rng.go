package sect

// Rng 是可以序列化的确定性随机数发生器(SplitMix64)，状态随存档或大比一起保存，只有服务端知道。
type Rng struct {
	S uint64 `json:"s"`
}

// Next 返回下一个 64 位随机数。
func (r *Rng) Next() uint64 {
	r.S += 0x9e3779b97f4a7c15
	z := r.S
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// Intn 返回 [0, n) 里的随机数，n 不大于 1 时返回 0。
func (r *Rng) Intn(n int) int {
	if n <= 1 {
		return 0
	}
	return int(r.Next() % uint64(n))
}

// Range 返回 [lo, hi] 里的随机数。
func (r *Rng) Range(lo, hi int) int { return lo + r.Intn(hi-lo+1) }

// Chance 以 pct% 的概率返回 true。
func (r *Rng) Chance(pct int) bool { return r.Intn(100) < pct }

// pickWeighted 按权重随机选一个下标，权重全为 0 时返回 0。
func pickWeighted(r *Rng, weights []int) int {
	total := 0
	for _, w := range weights {
		total += max(w, 0)
	}
	if total == 0 {
		return 0
	}
	roll := r.Intn(total)
	for i, w := range weights {
		roll -= max(w, 0)
		if roll < 0 {
			return i
		}
	}
	return len(weights) - 1
}
