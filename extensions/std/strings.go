package std

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/nethinwei/funroute"
)

// The string functions a routing rule actually writes: normalising a channel
// code, testing a card BIN or a phone prefix, cutting a composite field apart
// and putting reason codes back together. Everything here counts in characters
// (UTF-8 code points), not bytes, because that is what someone reading a rule
// means by "the first six".
func caseSpecs() []funroute.FunctionSpec {
	specs := make([]funroute.FunctionSpec, 0, 3)
	for _, fn := range []struct {
		name, label, description, param, result string
		apply                                   func(string) string
	}{
		{"upper", "转大写", "把字符串转成大写。", "文本", "大写文本", strings.ToUpper},
		{"lower", "转小写", "把字符串转成小写。", "文本", "小写文本", strings.ToLower},
		{"trim", "去空白", "去掉字符串两端的空白字符。", "文本", "去掉空白的文本", strings.TrimSpace},
	} {
		doc := funroute.Doc{
			Label: fn.label, Description: fn.description, Category: "字符串", Cost: 3,
			Params: []string{fn.param}, Result: fn.result,
		}
		apply := fn.apply
		specs = append(specs, logic(fn.name, doc, func(text string) (string, error) {
			return apply(text), nil
		}))
	}
	return specs
}

func testSpecs() []funroute.FunctionSpec {
	specs := make([]funroute.FunctionSpec, 0, 3)
	for _, fn := range []struct {
		name, label, description string
		test                     func(string, string) bool
	}{
		{"contains", "包含子串", "文本里有没有出现这个子串。", strings.Contains},
		{"starts_with", "以此开头", "文本是不是以这个前缀开头，卡 BIN 与号段判断用它。", strings.HasPrefix},
		{"ends_with", "以此结尾", "文本是不是以这个后缀结尾。", strings.HasSuffix},
	} {
		doc := funroute.Doc{
			Label: fn.label, Description: fn.description, Category: "字符串", Cost: 3,
			Params: []string{"文本", "子串"}, Result: "是否命中",
		}
		test := fn.test
		specs = append(specs, logic(fn.name, doc, func(text, part string) (bool, error) {
			return test(text, part), nil
		}))
	}
	return specs
}

func partSpecs() []funroute.FunctionSpec {
	return []funroute.FunctionSpec{
		logic("slice", funroute.Doc{
			Label:       "取子串",
			Description: "按字符位置取一段，从 start 到 end（不含 end），下标从 0 开始；越界报错，不静默截断。",
			Category:    "字符串", Cost: 4,
			Params: []string{"文本", "起点", "终点"}, Result: "子串",
		}, sliceString),
		logic("split", funroute.Doc{
			Label:       "拆分",
			Description: "按分隔符把文本拆成数组；分隔符为空是错误。",
			Category:    "字符串", Cost: 5,
			Params: []string{"文本", "分隔符"}, Result: "各段",
		}, splitString),
		logic("join", funroute.Doc{
			Label:       "拼接",
			Description: "用分隔符把一组文本连起来，拼原因码用它。",
			Category:    "字符串", Cost: 5,
			Params: []string{"各段", "分隔符"}, Result: "文本",
		}, func(parts []string, separator string) (string, error) {
			return strings.Join(parts, separator), nil
		}),
		logic("replace", funroute.Doc{
			Label:       "替换",
			Description: "把文本里出现的每一处 old 换成 new。",
			Category:    "字符串", Cost: 5,
			Params: []string{"文本", "被替换", "替换为"}, Result: "替换后的文本",
		}, func(text, old, replacement string) (string, error) {
			return strings.ReplaceAll(text, old, replacement), nil
		}),
	}
}

// paddingSpecs are for the places a payment file or an order number has a
// fixed width: 00001234, a 20-character reconciliation column.
func paddingSpecs() []funroute.FunctionSpec {
	specs := make([]funroute.FunctionSpec, 0, 2)
	for _, side := range []struct {
		name, label, side string
		left              bool
	}{
		{"pad_left", "左侧补齐", "左侧", true},
		{"pad_right", "右侧补齐", "右侧", false},
	} {
		doc := funroute.Doc{
			Label: side.label, Category: "字符串", Cost: 4,
			Description: "把文本补到指定的字符数（至多 10000），在" + side.side + "补；填充串必须是一个字符。已经够长就原样返回 —— 截断会悄悄丢掉数据。",
			Params:      []string{"文本", "宽度", "填充"}, Result: "补齐后的文本",
		}
		left := side.left
		specs = append(specs, logic(side.name, doc, func(text string, width int64, fill string) (string, error) {
			return padTo(text, width, fill, left)
		}))
	}
	return specs
}

// maxPadWidth caps the width a padding call builds. A fixed-width field in a
// payment file is tens of characters; the cap is what keeps a width written
// as 4000000000 from allocating gigabytes — in a run, and in the compiler,
// which folds a constant call — since fuel counts calls, not bytes.
const maxPadWidth = 10_000

func padTo(text string, width int64, fill string, left bool) (string, error) {
	if utf8.RuneCountInString(fill) != 1 {
		return "", fmt.Errorf("the padding must be exactly one character, got %q", fill)
	}
	if width < 0 || width > maxPadWidth {
		return "", fmt.Errorf("a width is 0 to %d characters, got %d", maxPadWidth, width)
	}
	missing := int(width) - utf8.RuneCountInString(text)
	if missing <= 0 {
		return text, nil
	}
	padding := strings.Repeat(fill, missing)
	if left {
		return padding + text, nil
	}
	return text + padding, nil
}

func sliceString(text string, start, end int64) (string, error) {
	runes := []rune(text)
	length := int64(len(runes))
	if start < 0 || end < start || end > length {
		return "", fmt.Errorf("slice [%d, %d) is outside a string of %d characters", start, end, length)
	}
	return string(runes[start:end]), nil
}

func splitString(text, separator string) ([]string, error) {
	if separator == "" {
		return nil, errors.New("split needs a separator")
	}
	return strings.Split(text, separator), nil
}
