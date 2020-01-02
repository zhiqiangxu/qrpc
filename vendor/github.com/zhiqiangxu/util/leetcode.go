package util

import (
	"math"
)

// Max for int
func Max(a, b int) int {
	if a > b {
		return a
	}

	return b
}

// Min for int
func Min(a, b int) int {
	if a > b {
		return b
	}

	return a
}

// MaxLengthOfUniqueSubstring returns the max length of unique sub string
func MaxLengthOfUniqueSubstring(s string) (l int) {

	var indexes [math.MaxUint8] /*byte是uint8*/ int
	n := len(s)

	var i, j int
	// 基于的观察：
	// 如果s[j]跟[i,j)有重复j'，那么可以跳过[i,j']的元素，i直接变为j'+1
	for ; j < n; j++ {
		byteJ := s[j]
		// 假如indexes的元素非0，那么必定是这个byte上一次出现的位置j'+1
		// 且j'的位置必须在[i,j)之间才有效
		if indexes[byteJ] != 0 && indexes[byteJ] > i {
			i = indexes[byteJ]
		}

		l = Max(l, j-i+1)
		indexes[byteJ] = j + 1
	}
	return
}

// ManacherFallback when all byte values are used in s
func ManacherFallback(s string) (ss string) {
	n := len(s)

	for i := 0; i < n; i++ {
		// 奇数的情况
		j := 1
		for ; i-j >= 0 && i+j < n; j++ {
			if s[i-j] != s[i+j] {
				break
			}
		}
		if len(ss) < (j*2 - 1) {
			ss = s[i-j+1 : i+j]
		}
		// 偶数的情况
		if i+1 < n && s[i] == s[i+1] {
			j := 1
			for ; i-j >= 0 && i+1+j < n; j++ {
				if s[i-j] != s[i+1+j] {
					break
				}
			}
			if len(ss) < j*2 {
				ss = s[i-j+1 : i+j+1]
			}
		}
	}
	return
}

// ManacherWithFallback tries Manacher if possible
func ManacherWithFallback(s string) (ss string) {
	var indexes [math.MaxUint8] /*byte是uint8*/ bool
	n := len(s)
	for i := 0; i < n; i++ {
		indexes[s[i]] = true
	}
	canManacher := false
	var manacherByte byte
	for i, exists := range indexes {
		if !exists {
			canManacher = true
			manacherByte = byte(i)
			break
		}
	}
	if !canManacher {
		ss = ManacherFallback(s)
		return
	}

	// preprocess
	bytes := make([]byte, 2*n+1, 2*n+1)
	bytes[0] = manacherByte
	for i := 0; i < n; i++ {
		bytes[2*i+1] = s[i]
		bytes[2*i+2] = manacherByte
	}

	r := make([]int, 2*n+1)
	var maxRightPos, maxRight, maxRPos, maxR int
	r[0] = 1
	r[2*n] = 1
	for i := 1; i < 2*n; i++ {
		if i >= maxRight {
			// 半径包括自己，所以1是最小值
			r[i] = 1
		} else {
			// i在maxRight以内
			// j'坐标为2*maxRightPos-i
			r[i] = Min(maxRight-i, r[2*maxRightPos-i])
		}
		// 尝试扩大半径
		for {
			if i-r[i] >= 0 && i+r[i] <= 2*n && bytes[i-r[i]] == bytes[i+r[i]] {
				r[i]++
			} else {
				break
			}
		}
		if i+r[i]-1 > maxRight {
			maxRight = i + r[i] - 1
			maxRightPos = i
		}
		if maxR < r[i] {
			maxRPos = i
			maxR = r[i]
		}
	}

	targetBytes := make([]byte, 0, r[maxRPos]-1 /*最终结果的长度*/)
	for i := maxRPos - r[maxRPos] + 1; i < maxRPos+r[maxRPos]; i++ {
		if bytes[i] != manacherByte {
			targetBytes = append(targetBytes, bytes[i])
		}
	}

	ss = String(targetBytes)
	if len(ss) != r[maxRPos]-1 {
		panic("size != r[maxRPos]-1")
	}

	return
}

// ReverseDigits for reverse digits
func ReverseDigits(n int32) (r int32) {

	if n > 0 {
		for n != 0 {
			pop := n % 10
			// 溢出判断
			if (r > math.MaxInt32/10) || (r == math.MaxInt32/10 && pop > 7) {
				// 上溢出
				return 0
			}

			r = 10*r + pop
			n /= 10
		}
	} else {
		for n != 0 {
			pop := n % 10
			// 溢出判断
			if r < math.MinInt32/10 || (r == math.MinInt32/10 && pop < -8) {
				// 下溢出
				return 0
			}

			r = 10*r + pop
			n /= 10
		}
	}

	return
}

// IsPalindrome checks whether n is palindrome
func IsPalindrome(n int) bool {
	if n < 0 {
		return false
	}

	var reverted int
	// 反转一半即可
	for n > reverted {
		reverted = 10*reverted + n%10
		n /= 10
	}

	return reverted == n || reverted/10 == n
}

// PatternMatchAllTD matches p against the whole s
// pattern supprts . and *
// top down
func PatternMatchAllTD(s, p string) bool {

	slen := len(s)
	plen := len(p)
	if plen == 0 {
		return slen == 0
	}

	memo := make([][]*bool, slen)
	for i := 0; i < slen; i++ {
		memo[i] = make([]*bool, plen)
	}

	var subProbFunc func(i, j int) bool
	// 子问题函数
	subProbFunc = func(i, j int) bool {

		// 如果都越界，匹配成功
		if i >= slen && j >= plen {
			return true
		}
		// 只有p越界，匹配失败
		if j >= plen {
			return false
		}
		// i越界，p未越界
		if i >= slen {
			// 看p能否匹配空
			if j+1 >= plen {
				return false
			}
			if p[j+1] != '*' {
				return false
			}
			return subProbFunc(i, j+2)
		}

		// 如果已计算，直接返回结果
		if memo[i][j] != nil {
			return *memo[i][j]
		}

		match := s[i] == p[j] || p[j] == '.'
		if j+1 >= plen {
			result := match && i == slen-1
			memo[i][j] = &result
			return result
		}

		// 转移方程

		if p[j+1] == '*' {
			result := subProbFunc(i, j+2) || // 匹配0次
				(match && subProbFunc(i+1, j+2)) || // 匹配1次
				(match && subProbFunc(i+1, j)) // 匹配多次

			memo[i][j] = &result
			return result
		}

		result := match && subProbFunc(i+1, j+1)
		memo[i][j] = &result

		return result
	}

	return subProbFunc(0, 0)
}

// PatternMatchAllBU matches p against the whole s
// pattern supprts . and *
// bottom up
func PatternMatchAllBU(s, p string) bool {

	slen := len(s)
	plen := len(p)
	if plen == 0 {
		return slen == 0
	}

	memo := make([][]*bool, slen)
	for i := 0; i < slen; i++ {
		memo[i] = make([]*bool, plen)
	}

	var subProbFunc func(i, j int) bool
	subProbFunc = func(i, j int) bool {
		if i < 0 && j < 0 {
			return true
		}
		if j < 0 {
			return false
		}
		if i < 0 {
			// 看p能否匹配空
			if p[j] != '*' {
				return false
			}
			if j == 0 {
				return false
			}
			return subProbFunc(i, j-2)
		}

		// 如果已计算，直接返回结果
		if memo[i][j] != nil {
			return *memo[i][j]
		}

		match := s[i] == p[j] || p[j] == '.'
		if j == 0 {
			result := match && i == 0
			memo[i][j] = &result
			return result
		}

		if p[j] == '*' {
			result := subProbFunc(i, j-2) || /*匹配0次*/
				((p[j-1] == s[i] || p[j-1] == '.') &&
					(subProbFunc(i-1, j-2) || /*匹配1次*/
						subProbFunc(i-1, j))) /*匹配多次*/
			memo[i][j] = &result
			return result
		}

		result := match && subProbFunc(i-1, j-1)
		memo[i][j] = &result
		return result
	}

	return subProbFunc(slen-1, plen-1)
}

// PatternMatchAllRec is the recursive version for PatternMatchAll
func PatternMatchAllRec(s, p string) bool {
	if len(p) == 0 {
		return len(s) == 0
	}

	if len(s) == 0 {
		// p必须是x*y*这种
		if len(p) < 2 || p[1] != '*' {
			return false
		}
		return PatternMatchAllRec(s, p[2:])
	}

	match := s[0] == p[0] || p[0] == '.'
	if len(p) < 2 {
		return match && len(s) == 1
	}

	if p[1] == '*' {
		return PatternMatchAllRec(s, p[2:]) /*匹配0次*/ ||
			(match && PatternMatchAllRec(s[1:], p[2:])) /*匹配1次*/ ||
			(match && PatternMatchAllRec(s[1:], p)) /*匹配多次*/
	}

	return match && PatternMatchAllRec(s[1:], p[1:])

}

// FindOnceNum find the number that appears only once
// caller should make sure only one num appears once
func FindOnceNum(nums []int) (r int) {
	for _, n := range nums {
		r ^= n
	}
	return
}

// MinCoveringSubstr finds min substr of s that covers t
func MinCoveringSubstr(s, t string) (ss string) {

	// 双指针滑动窗口
	var left, right, matched int

	needed := make(map[byte]int)
	for i := 0; i < len(t); i++ {
		needed[t[i]]++
	}
	windowed := make(map[byte]int)
	minLen := len(s) + 1

	for right < len(s) {

		rb := s[right]
		if _, ok := needed[rb]; ok {
			windowed[rb]++
			if windowed[rb] == needed[rb] {
				matched++
			}
		}

		if matched == len(needed) {
			for {
				lb := s[left]
				if _, ok := needed[lb]; !ok {
					left++
					continue
				}
				if windowed[lb] > needed[lb] {
					left++
					windowed[lb]--
					continue
				}
				// left不能再右了
				if right-left+1 < minLen {
					ss = s[left : right+1]
					minLen = right - left + 1
				}
				left++
				windowed[lb]--
				matched--
				break
			}
		}

		right++
	}

	return
}

// LongestConsecutive finds longest consecutive in nums
func LongestConsecutive(nums []int) (sn, length int) {
	numMap := make(map[int]struct{})
	for _, n := range nums {
		numMap[n] = struct{}{}
	}

	for _, n := range nums {
		if _, ok := numMap[n-1]; ok {
			continue
		}
		nn := n + 1
		for {
			if _, ok := numMap[nn]; ok {
				nn++
			} else {
				if nn-n > length {
					length = nn - n
					sn = n
				}
				break
			}
		}
	}

	return
}
